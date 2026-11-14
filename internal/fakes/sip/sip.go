// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package sip fakes the asterisk profile (deploy/asterisk) for web/test/sipservice.sh: a registrar and proxy on port 5060 (UDP and
// TCP) that knows one account, cloud-companion with the password ui-test-secret, asks for an MD5 digest like Asterisk does, refuses a
// wrong one, keeps the registrations (an Expires of 0 removes one) and takes MESSAGEs. The real Asterisk runs in deploy/asterisk/test.sh.
package sip

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/phorepoc/cloud-companion/internal/fakes"
)

// The seeded account (what PHECLOUD_ASTERISK_SECRET set up for the real one) and the digest realm Asterisk uses.
const (
	User  = "cloud-companion"
	Pass  = "ui-test-secret"
	Realm = "asterisk"
)

func init() {
	fakes.Register(fakes.Fake{Name: "sip", Run: func(ctx context.Context) error { return New(User, Pass).Serve(ctx, ":5060") }})
}

// Service is the fake: the accounts and what they did.
type Service struct {
	mu       sync.Mutex
	pass     map[string]string // user -> password
	contacts map[string]string // user -> registered contact
	Messages []string          // "to-user: body", in order
}

func New(user, pass string) *Service {
	return &Service{pass: map[string]string{user: pass}, contacts: map[string]string{}}
}

// Serve answers on addr over UDP and TCP until ctx ends.
func (s *Service) Serve(ctx context.Context, addr string) error {
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("Asterisk PBX (fake)"))
	if err != nil {
		return err
	}
	defer ua.Close()
	srv, err := sipgo.NewServer(ua)
	if err != nil {
		return err
	}
	srv.OnRegister(s.onRegister)
	srv.OnMessage(s.onMessage)
	srv.OnOptions(func(req *sip.Request, tx sip.ServerTransaction) {
		tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
	})
	errs := make(chan error, 2)
	for _, n := range []string{"udp", "tcp"} {
		go func() { errs <- srv.ListenAndServe(ctx, n, addr) }()
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-errs:
		return err
	}
}

// Registered is the contact user is registered at ("" when none).
func (s *Service) Registered(user string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.contacts[user]
}

func (s *Service) onRegister(req *sip.Request, tx sip.ServerTransaction) {
	user, ok := s.auth(req, tx, 401)
	if !ok {
		return
	}
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	if c := req.Contact(); c != nil {
		s.mu.Lock()
		if h := req.GetHeader("Expires"); h != nil && h.Value() == "0" {
			delete(s.contacts, user)
		} else {
			s.contacts[user] = c.Address.String()
		}
		s.mu.Unlock()
		res.AppendHeader(c.Clone())
	}
	tx.Respond(res)
}

func (s *Service) onMessage(req *sip.Request, tx sip.ServerTransaction) {
	if _, ok := s.auth(req, tx, 407); !ok {
		return
	}
	s.mu.Lock()
	s.Messages = append(s.Messages, req.Recipient.User+": "+string(req.Body()))
	s.mu.Unlock()
	tx.Respond(sip.NewResponseFromRequest(req, 202, "Accepted", nil))
}

func md5hex(s string) string { h := md5.Sum([]byte(s)); return hex.EncodeToString(h[:]) }

// auth checks the digest (Authorization for 401, Proxy-Authorization for 407); without one it sends the challenge, with a wrong one
// 403 Forbidden. It answers the request itself unless the login is good.
func (s *Service) auth(req *sip.Request, tx sip.ServerTransaction, code int) (string, bool) {
	hn, chn := "Authorization", "WWW-Authenticate"
	if code == 407 {
		hn, chn = "Proxy-Authorization", "Proxy-Authenticate"
	}
	h := req.GetHeader(hn)
	if h == nil {
		b := make([]byte, 8)
		rand.Read(b)
		res := sip.NewResponseFromRequest(req, code, "Unauthorized", nil)
		res.AppendHeader(sip.NewHeader(chn, fmt.Sprintf(`Digest realm="%s", nonce="%x", algorithm=MD5`, Realm, b)))
		tx.Respond(res)
		return "", false
	}
	a := map[string]string{}
	for _, part := range strings.Split(strings.TrimPrefix(h.Value(), "Digest "), ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		a[k] = strings.Trim(v, `"`)
	}
	s.mu.Lock()
	pass, known := s.pass[a["username"]]
	s.mu.Unlock()
	ha1 := md5hex(a["username"] + ":" + Realm + ":" + pass)
	ha2 := md5hex(string(req.Method) + ":" + a["uri"])
	want := md5hex(ha1 + ":" + a["nonce"] + ":" + ha2)
	if a["qop"] != "" {
		want = md5hex(ha1 + ":" + a["nonce"] + ":" + a["nc"] + ":" + a["cnonce"] + ":" + a["qop"] + ":" + ha2)
	}
	if !known || a["response"] != want {
		log.Printf("sip: refused %s for %q", req.Method, a["username"])
		tx.Respond(sip.NewResponseFromRequest(req, 403, "Forbidden", nil))
		return "", false
	}
	return a["username"], true
}
