// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package chat joins the services a message can go through (docs/messaging.md). Each is a Conn: SMS through your phones, your own
// SIP devices, and later Matrix, XMPP, IRC, Nextcloud Talk, Signal, Telegram. The Hub picks the connection for an address from your
// routing rules (store.ChatRoutes) and turns a message that arrived on a connection back into an address your devices can answer, so a
// reply goes out the way the message came in. What arrives lands in the store's chat inbox; the fronts (the SIP front, the web app)
// take it from there.
package chat

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/phorepoc/cloud-companion/internal/store"
)

var (
	// ErrNoRoute: no rule and no built-in default matches the address.
	ErrNoRoute = errors.New("no connection for that address")
	// ErrUnavailable: the connection exists but cannot send now (no phone syncs SMS, the service is down).
	ErrUnavailable = errors.New("that connection cannot send now")
)

// Conn is one service messages go through.
type Conn interface {
	ID() string    // stable id, used in rules and in the inbox ("sms", "sip", "matrix:<account>"...)
	Label() string // shown in the web app
	// Send delivers body from one of your logins to peer, the address as this connection names it; opt is the rule's own choice.
	Send(ctx context.Context, from, peer, body, opt string) error
}

// ConnInfo describes a connection for the routing page.
type ConnInfo struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Options []Option `json:"options,omitempty"` // what a rule's opt can be (for SMS: the phones)
}

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// optioner is a Conn with choices for its rules.
type optioner interface{ Options() []Option }

// Hub holds the connections and routes between them.
type Hub struct {
	St *store.Store
	// Notify is called after a message landed in the inbox (the SIP front delivers at once instead of at its next tick).
	Notify func()

	mu    sync.RWMutex
	conns map[string]Conn
}

func NewHub(st *store.Store) *Hub {
	h := &Hub{St: st, conns: map[string]Conn{}}
	h.Add(&SMS{St: st})
	h.Add(&Devices{St: st})
	return h
}

// Add registers a connection (replacing one with the same id).
func (h *Hub) Add(c Conn) {
	h.mu.Lock()
	h.conns[c.ID()] = c
	h.mu.Unlock()
}

// Remove drops a connection (its account was removed). Rules that name it stop matching anything until it is back.
func (h *Hub) Remove(id string) {
	h.mu.Lock()
	delete(h.conns, id)
	h.mu.Unlock()
}

func (h *Hub) conn(id string) Conn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.conns[id]
}

// Conns lists the connections, SMS and your devices first.
func (h *Hub) Conns() []ConnInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := []ConnInfo{}
	for _, c := range h.conns {
		i := ConnInfo{ID: c.ID(), Label: c.Label()}
		if o, ok := c.(optioner); ok {
			i.Options = o.Options()
		}
		out = append(out, i)
	}
	rank := map[string]int{"sms": 0, "sip": 1}
	sort.Slice(out, func(a, b int) bool {
		ra, oka := rank[out[a].ID]
		rb, okb := rank[out[b].ID]
		if oka != okb {
			return oka
		}
		if oka {
			return ra < rb
		}
		return out[a].ID < out[b].ID
	})
	return out
}

// IsNumber tells a phone number (+49 171 5550100) from any other address.
func IsNumber(a string) bool {
	a = strings.TrimPrefix(a, "tel:")
	digits := 0
	for _, r := range a {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '+' || r == '-' || r == '(' || r == ')' || r == '.' || r == ' ':
		default:
			return false
		}
	}
	return digits > 0
}

// Route picks the connection for an address: the first of your rules that matches, else the built-in defaults (a number goes out
// as SMS, a login to that device). It returns the connection, the address as that connection names it and the rule's option.
func (h *Hub) Route(addr string) (Conn, string, string, error) {
	for _, r := range h.St.ChatRoutes() {
		peer := ""
		switch {
		case r.Kind == "number" && IsNumber(addr):
			peer = addr
		case r.Kind == "prefix" && strings.HasPrefix(strings.ToLower(addr), strings.ToLower(r.Value)) && len(addr) > len(r.Value):
			peer = addr[len(r.Value):]
		default:
			continue
		}
		c := h.conn(r.Conn)
		if c == nil {
			return nil, "", "", ErrUnavailable // the rule names a connection that is not set up (any more)
		}
		return c, peer, r.Opt, nil
	}
	if IsNumber(addr) {
		return h.conn("sms"), addr, "", nil
	}
	if h.St.SIPUserHA1(strings.ToLower(addr)) != "" {
		return h.conn("sip"), strings.ToLower(addr), "", nil
	}
	return nil, "", "", ErrNoRoute
}

// Send routes a message from one of your logins to an address.
func (h *Hub) Send(ctx context.Context, from, addr, body string) error {
	c, peer, opt, err := h.Route(addr)
	if err != nil {
		return err
	}
	if err := c.Send(ctx, from, peer, body, opt); err != nil {
		return err
	}
	if c.ID() == "sip" && h.Notify != nil { // between your devices: the message is in the inbox already
		h.Notify()
	}
	return nil
}

// Address is how your devices write a peer of a connection, so that answering it routes back to the same connection: a phone
// number stays a number when numbers go there, a prefix rule puts its prefix in front.
func (h *Hub) Address(conn, peer string) string {
	if conn == "sip" {
		return peer
	}
	for _, r := range h.St.ChatRoutes() {
		if r.Conn != conn {
			continue
		}
		if r.Kind == "number" && IsNumber(peer) {
			return peer
		}
		if r.Kind == "prefix" {
			return r.Value + peer
		}
	}
	if conn == "sms" && IsNumber(peer) {
		return peer
	}
	return conn + "." + peer // no rule leads there yet: still unique, and a prefix rule with it makes it answerable
}

// In records a message that arrived on a connection and tells the fronts.
func (h *Hub) In(m store.ChatMessage) error {
	if err := h.St.ChatIn(m); err != nil {
		return err
	}
	if h.Notify != nil {
		h.Notify()
	}
	return nil
}

// Run keeps the connections that poll (SMS) feeding the inbox until ctx ends.
func (h *Hub) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		h.mu.RLock()
		var ps []poller
		for _, c := range h.conns {
			if p, ok := c.(poller); ok {
				ps = append(ps, p)
			}
		}
		h.mu.RUnlock()
		for _, p := range ps {
			p.Poll(h)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

type poller interface{ Poll(h *Hub) }

// ---- SMS through your phones

// SMS sends through one of your phones (docs/sms.md) and brings what they receive into the inbox.
type SMS struct{ St *store.Store }

func (*SMS) ID() string    { return "sms" }
func (*SMS) Label() string { return "SMS through your phones" }

func (s *SMS) Options() []Option {
	out := []Option{{Value: "", Label: "The phone that has the conversation"}}
	for _, d := range s.St.SMSDevices() {
		l := d.Name
		if l == "" {
			l = d.Device
		}
		out = append(out, Option{Value: d.Device, Label: l})
	}
	return out
}

func (s *SMS) Send(_ context.Context, _, peer, body, opt string) error {
	dev := opt
	if dev == "" {
		dev = s.St.SMSRoute(peer)
	}
	if dev == "" {
		return ErrUnavailable
	}
	_, err := s.St.SMSQueue(dev, peer, body)
	return err
}

const smsCursor = "chat.sms_cursor"

// Poll copies newly received SMS into the inbox. On a fresh server it starts after the newest SMS: old ones are not replayed.
func (s *SMS) Poll(h *Hub) {
	cur := s.St.Setting(smsCursor)
	if cur == "" {
		s.St.SetSetting(smsCursor, itoa(s.St.SMSMaxID()))
		return
	}
	for _, m := range s.St.SMSReceivedAfter(atoi(cur)) {
		if h.In(store.ChatMessage{Conn: "sms", Peer: strings.ReplaceAll(m.Number, " ", ""), PeerName: m.Number, Body: m.Body, TS: m.TS}) != nil {
			return
		}
		s.St.SetSetting(smsCursor, itoa(m.ID))
	}
}

// ---- your own devices

// Devices carries messages between your SIP logins.
type Devices struct{ St *store.Store }

func (*Devices) ID() string    { return "sip" }
func (*Devices) Label() string { return "Your SIP devices" }

func (d *Devices) Send(_ context.Context, from, peer, body, _ string) error {
	peer = strings.ToLower(peer)
	if d.St.SIPUserHA1(peer) == "" {
		return ErrNoRoute
	}
	return d.St.ChatIn(store.ChatMessage{Conn: "sip", Peer: from, PeerName: from, ToLogin: peer, Body: body})
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func atoi(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }
