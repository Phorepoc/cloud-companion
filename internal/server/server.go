// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package server is the phone-protocol endpoint: auth by paired device key, then a cloud-browser session.
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
	"github.com/phorepoc/phecloud/internal/proto"
	"github.com/phorepoc/phecloud/internal/store"
	"github.com/phorepoc/phecloud/internal/tiles"
)

type Server struct {
	Store   *store.Store
	Browser browser.Factory
	Log     *slog.Logger
}

func MAC(key, nonce []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(nonce)
	m.Write([]byte("phecloud-v1"))
	return m.Sum(nil)
}

func (s *Server) Serve(ln net.Listener) error {
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handle(c)
	}
}

type conn struct {
	c  net.Conn
	mu sync.Mutex
}

func (c *conn) send(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := proto.Write(c.c, v)
	return err
}

func (s *Server) handle(nc net.Conn) {
	defer nc.Close()
	log := s.Log.With("peer", nc.RemoteAddr().String())
	c := &conn{c: nc}
	r := bufio.NewReader(nc)

	nonce := make([]byte, 16)
	rand.Read(nonce)
	if c.send(proto.Challenge{T: proto.TChallenge, Proto: proto.Version, Nonce: nonce}) != nil {
		return
	}
	nc.SetReadDeadline(time.Now().Add(20 * time.Second))
	dev, err := s.auth(c, r, nonce)
	if err != nil {
		log.Warn("auth failed", "err", err)
		c.send(proto.Error{T: proto.TError, Msg: err.Error()})
		return
	}
	nc.SetReadDeadline(time.Time{})
	log = log.With("dev", dev)
	log.Info("device connected")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ses := &session{srv: s, c: c, log: log, dev: dev}
	defer ses.close()

	for {
		t, raw, err := proto.Read(r)
		if err != nil {
			log.Info("disconnected", "err", err)
			return
		}
		if err := ses.dispatch(ctx, t, raw); err != nil {
			log.Warn("request failed", "type", t, "err", err)
			c.send(proto.Error{T: proto.TError, Msg: err.Error()})
		}
	}
}

func (s *Server) auth(c *conn, r *bufio.Reader, nonce []byte) (string, error) {
	t, raw, err := proto.Read(r)
	if err != nil {
		return "", err
	}
	switch t {
	case proto.TPair:
		var p proto.Pair
		if err := proto.Decode(raw, &p); err != nil {
			return "", err
		}
		id, key, err := s.Store.Pair(p.Code, p.Name)
		if err != nil {
			time.Sleep(time.Second) // slow down guessing
			return "", err
		}
		return id, c.send(proto.Welcome{T: proto.TWelcome, Dev: id, Key: key, Server: "phecloud"})
	case proto.THello:
		var h proto.Hello
		if err := proto.Decode(raw, &h); err != nil {
			return "", err
		}
		key, err := s.Store.Key(h.Dev)
		if err != nil || !hmac.Equal(h.Mac, MAC(key, nonce)) {
			time.Sleep(time.Second)
			return "", store.ErrUnknown
		}
		return h.Dev, c.send(proto.Welcome{T: proto.TWelcome, Dev: h.Dev, Server: "phecloud"})
	}
	return "", store.ErrUnknown
}

var _ = tiles.TileW
