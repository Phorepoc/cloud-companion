// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package server is the phone-protocol endpoint: auth by paired device key, then a cloud-browser session. The same CBOR
// frames travel over a length-prefixed TCP/TLS connection (phones) or binary WebSocket messages (the web app).
package server

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/phorepoc/phecloud/internal/browser"
	"github.com/phorepoc/phecloud/internal/davhub"
	"github.com/phorepoc/phecloud/internal/proto"
	"github.com/phorepoc/phecloud/internal/store"
)

// Features are advertised in the welcome frame.
var Features = []string{"zoom", "tabs"}

type Server struct {
	Store   *store.Store
	Browser browser.Factory
	Log     *slog.Logger
	Hub     *davhub.Client // the personal hub (contacts, calendar); nil when not configured
	reg     registry
}

func MAC(key, nonce []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(nonce)
	m.Write([]byte("phecloud-v1"))
	return m.Sum(nil)
}

// FrameIO is one connection's frame transport.
type FrameIO interface {
	Read() (typ string, raw []byte, err error)
	Write(v any) error
	Close() error
}

type tcpIO struct {
	nc net.Conn
	r  *bufio.Reader
}

func (t *tcpIO) Read() (string, []byte, error) { return proto.Read(t.r) }
func (t *tcpIO) Write(v any) error             { _, err := proto.Write(t.nc, v); return err }
func (t *tcpIO) Close() error                  { return t.nc.Close() }

func (s *Server) Serve(ln net.Listener) error {
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.ServeConn(&tcpIO{nc: c, r: bufio.NewReader(c)}, c.RemoteAddr().String())
	}
}

type conn struct {
	io FrameIO
	mu sync.Mutex
}

func (c *conn) send(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.io.Write(v)
}

// ServeConn runs one connection to its end: challenge, pairing/auth, then requests.
func (s *Server) ServeConn(fio FrameIO, remote string) {
	defer fio.Close()
	log := s.Log.With("peer", remote)
	c := &conn{io: fio}

	nonce := make([]byte, 16)
	rand.Read(nonce)
	if c.send(proto.Challenge{T: proto.TChallenge, Proto: proto.Version, Nonce: nonce}) != nil {
		return
	}
	authTimer := time.AfterFunc(20*time.Second, func() { fio.Close() })
	dev, admin, err := s.auth(c, fio, nonce)
	authTimer.Stop()
	if err != nil {
		log.Warn("auth failed", "err", err)
		c.send(proto.Error{T: proto.TError, Msg: err.Error()})
		return
	}
	log = log.With("dev", dev)
	log.Info("device connected", "admin", admin)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ses := &session{srv: s, c: c, log: log, dev: dev, admin: admin, done: make(chan struct{}), dialogs: map[int]chan dialogAnswer{}, ops: make(chan func(ctx context.Context) error, 4)}
	ses.devName = s.Store.DeviceName(dev)
	s.register(ses)
	defer s.unregister(ses)
	defer ses.close()

	type frameMsg struct {
		t   string
		raw []byte
	}
	reqs := make(chan frameMsg, 16)
	go func() { // a dialog reply must be read while a request is blocked on the dialog, so reading is separate
		defer close(reqs)
		for {
			t, raw, err := fio.Read()
			if err != nil {
				log.Info("disconnected", "err", err)
				cancel()
				return
			}
			if t == proto.TDialogReply {
				ses.dialogReply(raw)
				continue
			}
			select {
			case reqs <- frameMsg{t, raw}:
			case <-ctx.Done():
				return
			}
		}
	}()
	for {
		select {
		case m, ok := <-reqs:
			if !ok {
				return
			}
			if err := ses.dispatch(ctx, m.t, m.raw); err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Warn("request failed", "type", m.t, "err", err)
				c.send(proto.Error{T: proto.TError, Msg: err.Error()})
			}
		case op := <-ses.ops: // another device asked this one to close a tab or open a page
			if err := op(ctx); err != nil {
				log.Warn("remote action failed", "err", err)
			}
		}
	}
}

func (s *Server) auth(c *conn, fio FrameIO, nonce []byte) (string, bool, error) {
	t, raw, err := fio.Read()
	if err != nil {
		return "", false, err
	}
	switch t {
	case proto.TPair:
		var p proto.Pair
		if err := proto.Decode(raw, &p); err != nil {
			return "", false, err
		}
		id, key, admin, err := s.Store.Pair(p.Code, p.Name)
		if err != nil {
			time.Sleep(time.Second) // slow down guessing
			return "", false, err
		}
		return id, admin, c.send(proto.Welcome{T: proto.TWelcome, Dev: id, Key: key, Server: "phecloud", Admin: admin, Features: Features})
	case proto.THello:
		var h proto.Hello
		if err := proto.Decode(raw, &h); err != nil {
			return "", false, err
		}
		key, admin, err := s.Store.Key(h.Dev)
		if err != nil || !hmac.Equal(h.Mac, MAC(key, nonce)) {
			time.Sleep(time.Second)
			return "", false, store.ErrUnknown
		}
		return h.Dev, admin, c.send(proto.Welcome{T: proto.TWelcome, Dev: h.Dev, Server: "phecloud", Admin: admin, Features: Features})
	}
	return "", false, store.ErrUnknown
}
