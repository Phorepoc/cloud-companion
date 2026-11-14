// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package xmppgw is the XMPP front (docs/messaging.md): phones whose chat app speaks XMPP (the Nokia N9's, Symbian's, any Jabber
// client) sign in with a login from the Messaging section, the same as the SIP front's, and chat through your connections.
//
// It is a small XMPP server for those logins only: TLS required (Cloud Companion's own certificate), the password over SASL PLAIN, no
// federation. Every address is a contact at the front's domain: "+306912345678@cloud-companion" goes out as SMS, "xm.bob@..." to
// the XMPP connection a rule names, "n9@..." to your other device; incoming messages arrive from those same contacts.
package xmppgw

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
	"mellium.im/sasl"
	"mellium.im/xmlstream"
	"mellium.im/xmpp"
	"mellium.im/xmpp/jid"
	"mellium.im/xmpp/stanza"
)

// Front is the XMPP front. Listen and Run start it.
type Front struct {
	St     *store.Store
	Hub    *chat.Hub
	Log    *slog.Logger
	TLS    *tls.Config
	Domain string        // the domain of the logins and contacts (default store.SIPRealm, as the SIP front)
	Tick   time.Duration // how often waiting messages are looked for (default 3 s)

	mu    sync.Mutex
	conns map[string]map[*client]bool // signed-in devices by login
	kick  chan struct{}
}

// client is one signed-in device.
type client struct {
	user   string
	s      *xmpp.Session
	online bool // sent its first presence: ready for messages
}

// New prepares the front; Listen and Run start it.
func New(st *store.Store, hub *chat.Hub, tc *tls.Config, log *slog.Logger) *Front {
	f := &Front{St: st, Hub: hub, Log: log, TLS: tc, Domain: store.SIPRealm, Tick: 3 * time.Second, conns: map[string]map[*client]bool{}, kick: make(chan struct{}, 1)}
	hub.OnIn(f.Kick)
	return f
}

// Kick asks for a delivery round now.
func (f *Front) Kick() {
	select {
	case f.kick <- struct{}{}:
	default:
	}
}

// Listen serves XMPP clients on addr (TCP, STARTTLS) until ctx ends.
func (f *Front) Listen(ctx context.Context, addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return f.Serve(ctx, l)
}

// Serve takes clients from l until ctx ends.
func (f *Front) Serve(ctx context.Context, l net.Listener) error {
	go func() { <-ctx.Done(); l.Close() }()
	for {
		c, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go f.serveConn(ctx, c)
	}
}

func (f *Front) domain() jid.JID { return jid.MustParse(f.Domain) }

// login checks a password against the login's digest (the logins keep only that, for SIP).
func (f *Front) login(user, pass string) bool {
	ha1 := f.St.SIPUserHA1(user)
	return ha1 != "" && subtle.ConstantTimeCompare([]byte(ha1), []byte(store.SIPHA1(user, pass))) == 1
}

func (f *Front) serveConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	var user string
	nctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	s, err := xmpp.ReceiveClientSession(nctx, f.domain(), conn,
		xmpp.StartTLS(f.TLS),
		xmpp.SASLServer(func(n *sasl.Negotiator) bool {
			u, p, id := n.Credentials()
			name := strings.ToLower(string(u))
			if (len(id) != 0 && string(id) != name && string(id) != name+"@"+f.Domain) || !f.login(name, string(p)) {
				return false
			}
			user = name
			return true
		}, sasl.Plain),
		xmpp.BindCustom(func(_ jid.JID, res string) (jid.JID, error) {
			if res == "" {
				res = rand.Text()[:12]
			}
			return jid.New(user, f.Domain, res)
		}),
	)
	cancel()
	if err != nil {
		f.Log.Debug("xmpp front: no session", "remote", conn.RemoteAddr().String(), "err", err)
		return
	}
	c := &client{user: user, s: s}
	f.mu.Lock()
	if f.conns[user] == nil {
		f.conns[user] = map[*client]bool{}
	}
	f.conns[user][c] = true
	f.mu.Unlock()
	f.Log.Info("xmpp front: signed in", "login", user, "remote", conn.RemoteAddr().String())
	defer func() {
		f.mu.Lock()
		delete(f.conns[user], c)
		f.mu.Unlock()
		s.Close()
	}()
	if err := s.Serve(xmpp.HandlerFunc(func(t xmlstream.TokenReadEncoder, start *xml.StartElement) error {
		return f.handle(c, t, start)
	})); err != nil && !errors.Is(err, io.EOF) {
		f.Log.Debug("xmpp front: session ended", "login", user, "err", err)
	}
}

func decode(t xml.TokenReader, start *xml.StartElement, v any) error {
	return xml.NewTokenDecoder(xmlstream.MultiReader(xmlstream.Token(*start), t)).Decode(v)
}

type iqIn struct {
	stanza.IQ
	Payload struct {
		XMLName xml.Name
	} `xml:",any"`
}

type msgIn struct {
	stanza.Message
	Body string `xml:"body"`
}

func (f *Front) handle(c *client, t xmlstream.TokenReadEncoder, start *xml.StartElement) error {
	switch start.Name.Local {
	case "iq":
		var iq iqIn
		if decode(t, start, &iq) != nil {
			return nil
		}
		return f.onIQ(c, t, iq)
	case "presence":
		var p stanza.Presence
		if decode(t, start, &p) != nil || p.To.String() != "" {
			return nil // presence to a contact: subscriptions are not kept, everyone is reachable
		}
		if p.Type == stanza.UnavailablePresence {
			c.online = false
			return nil
		}
		// the server echoes the device's own presence back (RFC 6121 §4.2.2)
		p.From, p.To = c.s.RemoteAddr(), c.s.RemoteAddr()
		t.Encode(p)
		c.online = true
		f.Kick()
	case "message":
		var m msgIn
		if decode(t, start, &m) != nil || strings.TrimSpace(m.Body) == "" || m.Type == stanza.ErrorMessage || m.Type == stanza.GroupChatMessage {
			return nil
		}
		f.onMessage(c, t, m)
	}
	return nil
}

// onIQ answers what a phone's chat app asks for at sign-in: the session (old clients), the contact list (empty: every address is
// reachable without one), pings and what the server supports. Anything else gets service-unavailable.
func (f *Front) onIQ(c *client, t xmlstream.TokenReadEncoder, iq iqIn) error {
	if iq.Type == stanza.ResultIQ || iq.Type == stanza.ErrorIQ {
		return nil
	}
	p := iq.Payload.XMLName
	var body xml.TokenReader
	switch {
	case p.Space == "urn:ietf:params:xml:ns:xmpp-session", p.Space == "urn:xmpp:ping":
	case p.Space == "jabber:iq:roster" && iq.Type == stanza.GetIQ:
		body = xmlstream.Wrap(nil, xml.StartElement{Name: xml.Name{Space: "jabber:iq:roster", Local: "query"}})
	case p.Space == "http://jabber.org/protocol/disco#info" && iq.Type == stanza.GetIQ:
		body = xmlstream.Wrap(xmlstream.MultiReader(
			xmlstream.Wrap(nil, xml.StartElement{Name: xml.Name{Local: "identity"}, Attr: []xml.Attr{{Name: xml.Name{Local: "category"}, Value: "server"}, {Name: xml.Name{Local: "type"}, Value: "im"}, {Name: xml.Name{Local: "name"}, Value: "Cloud Companion"}}}),
			feature("http://jabber.org/protocol/disco#info"), feature("urn:xmpp:ping"),
		), xml.StartElement{Name: xml.Name{Space: "http://jabber.org/protocol/disco#info", Local: "query"}})
	default:
		_, err := xmlstream.Copy(t, iq.IQ.Error(stanza.Error{Type: stanza.Cancel, Condition: stanza.ServiceUnavailable}))
		return err
	}
	_, err := xmlstream.Copy(t, iq.IQ.Result(body))
	return err
}

func feature(v string) xml.TokenReader {
	return xmlstream.Wrap(nil, xml.StartElement{Name: xml.Name{Local: "feature"}, Attr: []xml.Attr{{Name: xml.Name{Local: "var"}, Value: v}}})
}

// onMessage passes a device's message on through the hub; a refusal comes back as an error message, as clients show it.
func (f *Front) onMessage(c *client, t xmlstream.TokenReadEncoder, m msgIn) {
	to := Unescape(m.To.Localpart())
	if m.To.Domainpart() != f.Domain || to == "" {
		f.refuse(c, t, m, stanza.ItemNotFound, "Only addresses @"+f.Domain+" go through here")
		return
	}
	switch err := f.Hub.Send(context.Background(), c.user, to, m.Body); {
	case err == nil:
	case errors.Is(err, chat.ErrNoRoute):
		f.refuse(c, t, m, stanza.ItemNotFound, "No connection for that address")
	case errors.Is(err, chat.ErrUnavailable):
		f.refuse(c, t, m, stanza.RecipientUnavailable, "That connection cannot send now")
	default:
		f.Log.Warn("xmpp front: message not passed on", "login", c.user, "err", err)
		f.refuse(c, t, m, stanza.UndefinedCondition, "Could not pass the message on")
	}
}

func (f *Front) refuse(c *client, t xmlstream.TokenReadEncoder, m msgIn, cond stanza.Condition, text string) {
	m.Message.From = c.s.RemoteAddr() // Error swaps from and to: the reply comes from the address, to the device
	xmlstream.Copy(t, m.Message.Error(stanza.Error{Type: stanza.Cancel, Condition: cond, Text: map[string]string{"": text}}))
}

// ---- delivery to devices

// Run delivers what waits in the inbox to the signed-in devices until ctx ends.
func (f *Front) Run(ctx context.Context) {
	tick := time.NewTicker(f.Tick)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-f.kick:
		}
		f.deliverAll(ctx)
	}
}

func (f *Front) online() map[string][]*client {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string][]*client{}
	for u, cs := range f.conns {
		for c := range cs {
			if c.online {
				out[u] = append(out[u], c)
			}
		}
	}
	return out
}

// deliverAll sends each waiting message to every online device of its login (they all show the chat); it is taken from the inbox
// once one device got it.
func (f *Front) deliverAll(ctx context.Context) {
	for user, cs := range f.online() {
		for _, m := range f.St.ChatFor(user) {
			from, err := jid.New(Escape(f.Hub.Address(m.Conn, m.Peer)), f.Domain, "")
			if err != nil {
				f.Log.Warn("xmpp front: an address that cannot be written as XMPP", "conn", m.Conn, "err", err)
				f.St.ChatTaken(user, m.ID)
				continue
			}
			sent := false
			for _, c := range cs {
				if f.send(ctx, c, from, m) == nil {
					sent = true
				}
			}
			if !sent {
				break // keep the order: the rest waits for the next round
			}
			f.St.ChatTaken(user, m.ID)
		}
	}
}

func (f *Front) send(ctx context.Context, c *client, from jid.JID, m store.ChatMessage) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	msg := stanza.Message{From: from, To: c.s.RemoteAddr(), Type: stanza.ChatMessage, ID: rand.Text()}
	parts := []xml.TokenReader{xmlstream.Wrap(xmlstream.Token(xml.CharData(m.Body)), xml.StartElement{Name: xml.Name{Local: "body"}})}
	if m.PeerName != "" { // the name to show (XEP-0172), as the device has no contact list entry for it
		parts = append(parts, xmlstream.Wrap(xmlstream.Token(xml.CharData(m.PeerName)), xml.StartElement{Name: xml.Name{Space: "http://jabber.org/protocol/nick", Local: "nick"}}))
	}
	if m.TS > 0 && time.Since(time.Unix(m.TS, 0)) > time.Minute { // it waited: say when it was sent (XEP-0203)
		parts = append(parts, xmlstream.Wrap(nil, xml.StartElement{Name: xml.Name{Space: "urn:xmpp:delay", Local: "delay"}, Attr: []xml.Attr{
			{Name: xml.Name{Local: "from"}, Value: f.Domain}, {Name: xml.Name{Local: "stamp"}, Value: time.Unix(m.TS, 0).UTC().Format(time.RFC3339)}}}))
	}
	return c.s.Send(ctx, msg.Wrap(xmlstream.MultiReader(parts...)))
}

// ---- addresses as XMPP names (XEP-0106: the characters a localpart cannot hold)

var escapes = strings.NewReplacer(`\`, `\5c`, " ", `\20`, `"`, `\22`, "&", `\26`, "'", `\27`, "/", `\2f`, ":", `\3a`, "<", `\3c`, ">", `\3e`, "@", `\40`)
var unescapes = strings.NewReplacer(`\20`, " ", `\22`, `"`, `\26`, "&", `\27`, "'", `\2f`, "/", `\3a`, ":", `\3c`, "<", `\3e`, ">", `\40`, "@", `\5c`, `\`)

// Escape writes an address as a JID localpart.
func Escape(addr string) string { return escapes.Replace(addr) }

// Unescape reads an address back from a JID localpart.
func Unescape(local string) string { return unescapes.Replace(local) }
