// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package sipgw is the SIP front of Cloud Companion (docs/messaging.md): phones register with it as with any SIP service and chat
// through it. It is not a SIP service of its own but a front for the chat connections (package chat): a message goes to the
// connection your routing rules pick for its address (a number to SMS through your phones by default, a login to that device), and
// what arrives on the connections comes to the devices that get it, from an address that answers back the same way. Calls are not
// handled yet.
package sipgw

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
)

const (
	maxExpires  = 3600
	nonceMaxAge = 5 * time.Minute
)

type binding struct {
	contact   sip.Uri
	source    string // where the REGISTER came from (behind NAT the Contact address is not reachable)
	transport string
	expires   time.Time
}

// Gateway is the SIP front. Run starts it; Kick asks it to deliver what is waiting now rather than at the next tick.
type Gateway struct {
	St   *store.Store
	Hub  *chat.Hub
	Log  *slog.Logger
	Tick time.Duration // how often waiting messages and new SMS are looked for (default 3 s)

	ua    *sipgo.UserAgent
	srv   *sipgo.Server
	cli   *sipgo.Client
	key   []byte // signs nonces, so they need no state
	mu    sync.Mutex
	binds map[string]binding
	kick  chan struct{}
}

// New prepares a gateway; Listen and Run start it.
func New(st *store.Store, hub *chat.Hub, log *slog.Logger) (*Gateway, error) {
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("Cloud Companion"))
	if err != nil {
		return nil, err
	}
	srv, err := sipgo.NewServer(ua)
	if err != nil {
		return nil, err
	}
	cli, err := sipgo.NewClient(ua)
	if err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	rand.Read(key)
	g := &Gateway{St: st, Hub: hub, Log: log, Tick: 3 * time.Second, ua: ua, srv: srv, cli: cli, key: key, binds: map[string]binding{}, kick: make(chan struct{}, 1)}
	hub.Notify = g.Kick
	srv.OnRegister(g.onRegister)
	srv.OnMessage(g.onMessage)
	srv.OnOptions(func(req *sip.Request, tx sip.ServerTransaction) {
		tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
	})
	return g, nil
}

// Listen serves SIP on addr over UDP and TCP until ctx ends.
func (g *Gateway) Listen(ctx context.Context, addr string) error {
	errs := make(chan error, 2)
	for _, n := range []string{"udp", "tcp"} {
		go func(n string) { errs <- g.srv.ListenAndServe(ctx, n, addr) }(n)
	}
	return <-errs
}

// Run delivers waiting messages and new SMS to registered devices until ctx ends.
func (g *Gateway) Run(ctx context.Context) {
	t := time.NewTicker(g.Tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-g.kick:
		}
		g.deliverAll(ctx)
	}
}

// Kick asks for a delivery round now.
func (g *Gateway) Kick() {
	select {
	case g.kick <- struct{}{}:
	default:
	}
}

// Close stops the SIP front.
func (g *Gateway) Close() { g.srv.Close(); g.cli.Close(); g.ua.Close() }

// ---- digest authentication (RFC 2617 / 3261 22): stateless nonces signed with the gateway's key

func (g *Gateway) nonce() string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	m := hmac.New(sha256.New, g.key)
	m.Write([]byte(ts))
	return ts + "." + hex.EncodeToString(m.Sum(nil))[:32]
}

func (g *Gateway) nonceOK(n string) bool {
	ts, sig, ok := strings.Cut(n, ".")
	if !ok {
		return false
	}
	m := hmac.New(sha256.New, g.key)
	m.Write([]byte(ts))
	if !hmac.Equal([]byte(sig), []byte(hex.EncodeToString(m.Sum(nil))[:32])) {
		return false
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	return err == nil && time.Since(time.Unix(t, 0)) < nonceMaxAge
}

// params makes header parameters with one key and value.
func params(k, v string) sip.HeaderParams {
	p := sip.NewParams()
	p.Add(k, v)
	return p
}

func md5hex(s string) string { h := md5.Sum([]byte(s)); return hex.EncodeToString(h[:]) }

// parseAuth splits a Digest Authorization header into its parameters.
func parseAuth(v string) map[string]string {
	out := map[string]string{}
	v = strings.TrimSpace(v)
	if len(v) < 7 || !strings.EqualFold(v[:7], "digest ") {
		return out
	}
	for _, part := range splitParams(v[7:]) {
		k, val, ok := strings.Cut(part, "=")
		if ok {
			out[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(val), `"`)
		}
	}
	return out
}

// splitParams splits on commas that are not inside quotes.
func splitParams(s string) []string {
	var out []string
	q, start := false, 0
	for i, r := range s {
		switch {
		case r == '"':
			q = !q
		case r == ',' && !q:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// authenticate answers 401 with a challenge unless the request carries valid credentials; it returns the login on success.
func (g *Gateway) authenticate(req *sip.Request, tx sip.ServerTransaction) (string, bool) {
	h := req.GetHeader("Authorization")
	if h != nil {
		a := parseAuth(h.Value())
		user := strings.ToLower(a["username"])
		ha1 := g.St.SIPUserHA1(user)
		if ha1 != "" && a["realm"] == store.SIPRealm && g.nonceOK(a["nonce"]) {
			ha2 := md5hex(string(req.Method) + ":" + a["uri"])
			want := md5hex(ha1 + ":" + a["nonce"] + ":" + ha2)
			if a["qop"] != "" {
				want = md5hex(ha1 + ":" + a["nonce"] + ":" + a["nc"] + ":" + a["cnonce"] + ":" + a["qop"] + ":" + ha2)
			}
			if hmac.Equal([]byte(want), []byte(strings.ToLower(a["response"]))) {
				return user, true
			}
		}
	}
	res := sip.NewResponseFromRequest(req, 401, "Unauthorized", nil)
	res.AppendHeader(sip.NewHeader("WWW-Authenticate", fmt.Sprintf(`Digest realm="%s", nonce="%s", algorithm=MD5, qop="auth"`, store.SIPRealm, g.nonce())))
	tx.Respond(res)
	return "", false
}

// ---- registration

func (g *Gateway) onRegister(req *sip.Request, tx sip.ServerTransaction) {
	user, ok := g.authenticate(req, tx)
	if !ok {
		return
	}
	if to := req.To(); to == nil || !strings.EqualFold(to.Address.User, user) {
		tx.Respond(sip.NewResponseFromRequest(req, 403, "Register your own login only", nil))
		return
	}
	exp := maxExpires
	if h := req.GetHeader("Expires"); h != nil {
		if n, err := strconv.Atoi(strings.TrimSpace(h.Value())); err == nil {
			exp = n
		}
	}
	c := req.Contact()
	if c != nil {
		if v, ok := c.Params.Get("expires"); ok {
			if n, err := strconv.Atoi(v); err == nil {
				exp = n
			}
		}
	}
	exp = min(max(exp, 0), maxExpires)
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	g.mu.Lock()
	if c == nil || c.Address.Wildcard || exp == 0 {
		delete(g.binds, user)
	} else {
		g.binds[user] = binding{contact: c.Address, source: req.Source(), transport: req.Transport(), expires: time.Now().Add(time.Duration(exp) * time.Second)}
		cc := c.Clone()
		cc.Params = params("expires", strconv.Itoa(exp))
		res.AppendHeader(cc)
	}
	g.mu.Unlock()
	tx.Respond(res)
	if exp > 0 && c != nil {
		g.Log.Info("sip: registered", "login", user, "from", req.Source())
		g.Kick()
	}
}

// ---- messages from a device

func (g *Gateway) onMessage(req *sip.Request, tx sip.ServerTransaction) {
	user, ok := g.authenticate(req, tx)
	if !ok {
		return
	}
	if ct := req.ContentType(); ct != nil && !strings.HasPrefix(strings.ToLower(ct.Value()), "text/plain") {
		tx.Respond(sip.NewResponseFromRequest(req, 415, "Only text/plain", nil))
		return
	}
	body := string(req.Body())
	if strings.TrimSpace(body) == "" {
		tx.Respond(sip.NewResponseFromRequest(req, 400, "Empty message", nil))
		return
	}
	to := req.Recipient.User
	if to == "" && req.To() != nil {
		to = req.To().Address.User
	}
	switch err := g.Hub.Send(context.Background(), user, to, body); {
	case errors.Is(err, chat.ErrNoRoute):
		tx.Respond(sip.NewResponseFromRequest(req, 404, "No connection for that address", nil))
		return
	case errors.Is(err, chat.ErrUnavailable):
		tx.Respond(sip.NewResponseFromRequest(req, 480, "That connection cannot send now", nil))
		return
	case err != nil:
		g.Log.Warn("sip: message not passed on", "login", user, "err", err)
		tx.Respond(sip.NewResponseFromRequest(req, 500, "Could not pass the message on", nil))
		return
	}
	tx.Respond(sip.NewResponseFromRequest(req, 202, "Accepted", nil))
}

// ---- delivery to devices

func (g *Gateway) bound() map[string]binding {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := map[string]binding{}
	for u, b := range g.binds {
		if time.Now().After(b.expires) {
			delete(g.binds, u)
			continue
		}
		out[u] = b
	}
	return out
}

func (g *Gateway) deliverAll(ctx context.Context) {
	for user, b := range g.bound() {
		for _, m := range g.St.ChatFor(user) {
			from := "sip:" + g.Hub.Address(m.Conn, m.Peer) + "@" + store.SIPRealm
			if g.send(ctx, b, from, m.PeerName, m.Body) != nil {
				break // keep the order: the rest waits for the next round
			}
			g.St.ChatTaken(user, m.ID)
		}
	}
}

// send delivers one MESSAGE to a registered device; nil once the device has accepted it (2xx).
func (g *Gateway) send(ctx context.Context, b binding, fromURI, fromName, body string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req := sip.NewRequest(sip.MESSAGE, b.contact)
	var fu sip.Uri
	if err := sip.ParseUri(fromURI, &fu); err != nil {
		return err
	}
	tag := make([]byte, 8)
	rand.Read(tag)
	req.AppendHeader(&sip.FromHeader{DisplayName: fromName, Address: fu, Params: params("tag", hex.EncodeToString(tag))})
	req.AppendHeader(&sip.ToHeader{Address: b.contact})
	req.AppendHeader(sip.NewHeader("Content-Type", "text/plain;charset=UTF-8"))
	req.SetBody([]byte(body))
	req.SetDestination(b.source)
	if b.transport != "" {
		req.SetTransport(b.transport)
	}
	res, err := g.cli.Do(ctx, req)
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("the device answered %d %s", res.StatusCode, res.Reason)
	}
	return nil
}
