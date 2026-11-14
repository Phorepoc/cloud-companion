// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package sipsvc connects Cloud Companion to a SIP service as one of its accounts (docs/messaging.md): your own Asterisk (the compose
// profile "asterisk") or a provider. Cloud Companion registers there like a phone, sends your messages through it and takes the
// messages the service delivers to it into the chat inbox, so your routing rules can send any address to that service.
package sipsvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
)

// Cfg is the account's configuration (store.Account.Config); the password is its secret.
type Cfg struct {
	Server    string `json:"server"`    // host or host:port of the registrar and proxy (5060 when no port is given)
	Domain    string `json:"domain"`    // the SIP domain of addresses; the server's host when empty
	User      string `json:"user"`      // the account's user name there
	Transport string `json:"transport"` // udp (default) or tcp
	Listen    string `json:"listen"`    // where Cloud Companion takes this service's messages, e.g. ":5070"
	Contact   string `json:"contact"`   // host:port the service reaches that at; the listen address when empty
}

func (c *Cfg) norm() error {
	c.Server, c.User, c.Domain = strings.TrimSpace(c.Server), strings.TrimSpace(c.User), strings.TrimSpace(c.Domain)
	if c.Server == "" || c.User == "" {
		return errors.New("the server and the user name are needed")
	}
	if _, _, err := net.SplitHostPort(c.Server); err != nil {
		c.Server = net.JoinHostPort(c.Server, "5060")
	}
	if c.Domain == "" {
		c.Domain, _, _ = net.SplitHostPort(c.Server)
	}
	if c.Transport != "tcp" {
		c.Transport = "udp"
	}
	if c.Listen == "" {
		c.Listen = ":5070"
	}
	return nil
}

func (c Cfg) aor() sip.Uri { return sip.Uri{Scheme: "sip", User: c.User, Host: c.Domain} }

const expires = 600

// register sends one REGISTER (with the digest when challenged). contact nil asks for the bindings only, which checks the login
// without changing anything.
func register(ctx context.Context, cli *sipgo.Client, c Cfg, pass string, contact *sip.Uri, exp int) error {
	host, port, _ := net.SplitHostPort(c.Server)
	req := sip.NewRequest(sip.REGISTER, sip.Uri{Scheme: "sip", Host: c.Domain})
	aor := c.aor()
	req.AppendHeader(&sip.FromHeader{Address: aor, Params: params("tag", sip.GenerateTagN(8))})
	req.AppendHeader(&sip.ToHeader{Address: aor})
	if contact != nil {
		req.AppendHeader(&sip.ContactHeader{Address: *contact})
		req.AppendHeader(sip.NewHeader("Expires", fmt.Sprint(exp)))
	}
	req.SetDestination(net.JoinHostPort(host, port))
	req.SetTransport(strings.ToUpper(c.Transport))
	res, err := cli.Do(ctx, req, sipgo.ClientRequestRegisterBuild)
	if err != nil {
		return fmt.Errorf("no answer from %s: %w", c.Server, err)
	}
	if res.StatusCode == 401 || res.StatusCode == 407 {
		res, err = cli.DoDigestAuth(ctx, req, res, sipgo.DigestAuth{Username: c.User, Password: pass})
		if err != nil {
			return fmt.Errorf("no answer from %s: %w", c.Server, err)
		}
	}
	switch {
	case res.StatusCode == 401 || res.StatusCode == 403 || res.StatusCode == 407:
		return errors.New("login refused: check the user name and password")
	case res.StatusCode < 200 || res.StatusCode >= 300:
		return fmt.Errorf("the service answered %d %s", res.StatusCode, res.Reason)
	}
	return nil
}

func params(k, v string) sip.HeaderParams {
	p := sip.NewParams()
	p.Add(k, v)
	return p
}

// Test checks the login on the service (the Connections page's Test button) without registering anything.
func Test(ctx context.Context, c Cfg, pass string) (string, error) {
	if err := c.norm(); err != nil {
		return "", err
	}
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("Cloud Companion"))
	if err != nil {
		return "", err
	}
	defer ua.Close()
	cli, err := sipgo.NewClient(ua)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := register(ctx, cli, c, pass, nil, 0); err != nil {
		return "", err
	}
	return "login works as " + c.User + "@" + c.Domain, nil
}

// Service is one connected SIP service, a chat connection.
type Service struct {
	Acc  string // the account id
	Name string
	Cfg  Cfg
	Pass string
	Hub  *chat.Hub // where its messages go; the hub that adds it sets this (chat.Hub.Add)
	Log  *slog.Logger
	// Status reports the registration (ok, or what went wrong) for the account's row.
	Status func(err error)

	ua     *sipgo.UserAgent
	cli    *sipgo.Client
	srv    *sipgo.Server
	from   map[string]bool // the service's addresses: only they may deliver
	cancel context.CancelFunc
	mu     sync.Mutex
}

func ConnID(acc string) string { return "sipsvc:" + acc }

// Start is the chat.Starter for "sip" accounts.
func Start(log *slog.Logger) chat.Starter {
	return func(a store.Account, pass string, status func(error)) (chat.Running, error) {
		var c Cfg
		if err := json.Unmarshal([]byte(a.Config), &c); err != nil {
			return nil, errors.New("bad configuration")
		}
		s := &Service{Acc: a.ID, Name: a.Name, Cfg: c, Pass: pass, Log: log, Status: status}
		return s, s.Start()
	}
}

func (s *Service) ID() string    { return ConnID(s.Acc) }
func (s *Service) Label() string { return s.Name + " (SIP)" }

// Start listens for the service's messages and keeps the registration until Stop.
func (s *Service) SetHub(h *chat.Hub) { s.Hub = h }

func (s *Service) Start() error {
	if err := s.Cfg.norm(); err != nil {
		return err
	}
	var err error
	if s.ua, err = sipgo.NewUA(sipgo.WithUserAgent("Cloud Companion")); err != nil {
		return err
	}
	if s.srv, err = sipgo.NewServer(s.ua); err != nil {
		return err
	}
	if s.cli, err = sipgo.NewClient(s.ua); err != nil {
		return err
	}
	s.srv.OnMessage(s.onMessage)
	s.srv.OnOptions(func(req *sip.Request, tx sip.ServerTransaction) {
		tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
	})
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	ready, failed := make(chan struct{}), make(chan error, 1)
	lctx := context.WithValue(ctx, sipgo.ListenReadyCtxKey, sipgo.ListenReadyCtxValue(ready))
	go func() {
		if err := s.srv.ListenAndServe(lctx, s.Cfg.Transport, s.Cfg.Listen); err != nil && ctx.Err() == nil {
			failed <- err
		}
	}()
	select {
	case <-ready:
	case err := <-failed:
		cancel()
		return fmt.Errorf("cannot take messages on %s: %w", s.Cfg.Listen, err)
	case <-time.After(5 * time.Second):
		cancel()
		return errors.New("the listener for " + s.Cfg.Listen + " did not start")
	}
	go s.keep(ctx)
	return nil
}

// Stop unregisters and closes the listener.
func (s *Service) Stop() {
	if s.cancel == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	register(ctx, s.cli, s.Cfg, s.Pass, s.contact(), 0)
	cancel()
	s.cancel()
	s.srv.Close()
	s.ua.Close()
}

func (s *Service) contact() *sip.Uri {
	hp := s.Cfg.Contact
	if hp == "" {
		hp = s.Cfg.Listen
	}
	host, port, _ := net.SplitHostPort(hp)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = outboundIP(s.Cfg.Server)
	}
	u := sip.Uri{Scheme: "sip", User: s.Cfg.User, Host: host}
	fmt.Sscan(port, &u.Port)
	if s.Cfg.Transport == "tcp" {
		u.UriParams = params("transport", "tcp")
	}
	return &u
}

// outboundIP is this machine's address towards the server (what the service can reach when no contact is configured).
func outboundIP(server string) string {
	c, err := net.Dial("udp", server)
	if err != nil {
		return "127.0.0.1"
	}
	defer c.Close()
	h, _, _ := net.SplitHostPort(c.LocalAddr().String())
	return h
}

// keep registers now and again before each registration runs out; a failure is retried after 30 s.
func (s *Service) keep(ctx context.Context) {
	for {
		s.refreshFrom()
		rc, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := register(rc, s.cli, s.Cfg, s.Pass, s.contact(), expires)
		cancel()
		if s.Status != nil && ctx.Err() == nil {
			s.Status(err)
		}
		wait := expires / 2 * time.Second
		if err != nil {
			s.Log.Warn("sip service: not registered", "account", s.Name, "err", err)
			wait = 30 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// refreshFrom resolves the server's addresses: a MESSAGE from anywhere else is refused.
func (s *Service) refreshFrom() {
	host, _, _ := net.SplitHostPort(s.Cfg.Server)
	ips, _ := net.LookupHost(host)
	m := map[string]bool{host: true}
	for _, ip := range ips {
		m[ip] = true
	}
	s.mu.Lock()
	s.from = m
	s.mu.Unlock()
}

func (s *Service) fromService(source string) bool {
	h, _, err := net.SplitHostPort(source)
	if err != nil {
		h = source
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.from[h]
}

func (s *Service) onMessage(req *sip.Request, tx sip.ServerTransaction) {
	if !s.fromService(req.Source()) {
		tx.Respond(sip.NewResponseFromRequest(req, 403, "Only the service delivers here", nil))
		return
	}
	if ct := req.ContentType(); ct != nil && !strings.HasPrefix(strings.ToLower(ct.Value()), "text/plain") {
		tx.Respond(sip.NewResponseFromRequest(req, 415, "Only text/plain", nil))
		return
	}
	f := req.From()
	if f == nil || f.Address.User == "" || len(req.Body()) == 0 {
		tx.Respond(sip.NewResponseFromRequest(req, 400, "No sender or no text", nil))
		return
	}
	peer := f.Address.User
	if !strings.EqualFold(f.Address.Host, s.Cfg.Domain) && net.ParseIP(f.Address.Host) == nil {
		peer += "@" + f.Address.Host // someone on another domain: answer them there
	}
	name := f.DisplayName
	if name == "" {
		name = peer
	}
	if err := s.Hub.In(store.ChatMessage{Conn: s.ID(), Peer: peer, PeerName: name, Body: string(req.Body()), TS: time.Now().Unix()}); err != nil {
		tx.Respond(sip.NewResponseFromRequest(req, 500, "Could not keep the message", nil))
		return
	}
	tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
}

// Send passes a message to peer (an extension or user on the service's domain, or user@domain) through the service.
func (s *Service) Send(ctx context.Context, from, peer, body, _ string) error {
	to := sip.Uri{Scheme: "sip", User: peer, Host: s.Cfg.Domain}
	if u, h, ok := strings.Cut(peer, "@"); ok {
		to.User, to.Host = u, h
	}
	req := sip.NewRequest(sip.MESSAGE, to)
	req.AppendHeader(&sip.FromHeader{DisplayName: from, Address: s.Cfg.aor(), Params: params("tag", sip.GenerateTagN(8))})
	req.AppendHeader(&sip.ToHeader{Address: to})
	req.AppendHeader(sip.NewHeader("Content-Type", "text/plain;charset=UTF-8"))
	req.SetBody([]byte(body))
	req.SetDestination(s.Cfg.Server)
	req.SetTransport(strings.ToUpper(s.Cfg.Transport))
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := s.cli.Do(ctx, req)
	if err == nil && (res.StatusCode == 401 || res.StatusCode == 407) {
		res, err = s.cli.DoDigestAuth(ctx, req, res, sipgo.DigestAuth{Username: s.Cfg.User, Password: s.Pass})
	}
	if err != nil {
		return fmt.Errorf("%w: %v", chat.ErrUnavailable, err)
	}
	switch {
	case res.StatusCode == 404 || res.StatusCode == 484:
		return chat.ErrNoRoute
	case res.StatusCode < 200 || res.StatusCode >= 300:
		return fmt.Errorf("%w: the service answered %d %s", chat.ErrUnavailable, res.StatusCode, res.Reason)
	}
	return nil
}
