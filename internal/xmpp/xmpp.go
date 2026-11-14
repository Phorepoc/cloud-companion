// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package xmpp connects an XMPP (Jabber) account as a chat connection (docs/messaging.md): Cloud Companion signs in as one more
// resource of your account (TLS required), takes the chat messages sent to you into the chat inbox and sends your answers back.
//
// Addresses are written the way the Matrix connection writes them, so a SIP phone can type them: someone on your own server is the
// name alone ("bob"), anyone else is "bob=example.org" ("@" ends a SIP user part). The web app may also use the plain bob@example.org.
package xmpp

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
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
	"mellium.im/xmpp/dial"
	"mellium.im/xmpp/jid"
	"mellium.im/xmpp/roster"
	"mellium.im/xmpp/stanza"
)

// Cfg is the account's configuration (store.Account.Config); the password is its secret.
type Cfg struct {
	JID    string `json:"jid"`              // you@example.org
	Server string `json:"server,omitempty"` // host:port, when the server is not found from the domain (DNS SRV records)
	// Insecure accepts a server certificate that cannot be checked (a server of your own with a self-made certificate).
	Insecure bool `json:"insecure,omitempty"`
	// Rooms are the group chats (XEP-0045) to join, as room@conference.example.org; Nick is the name shown there (default: the
	// address's local part).
	Rooms []string `json:"rooms,omitempty"`
	Nick  string   `json:"nick,omitempty"`
}

func (c *Cfg) norm() (jid.JID, error) {
	c.JID, c.Server = strings.TrimSpace(c.JID), strings.TrimSpace(c.Server)
	j, err := jid.Parse(c.JID)
	if err != nil || j.Localpart() == "" {
		return jid.JID{}, errors.New("the address is needed, as you@example.org")
	}
	if c.Server != "" {
		if _, _, err := net.SplitHostPort(c.Server); err != nil {
			c.Server = net.JoinHostPort(c.Server, "5222")
		}
	}
	c.Nick = strings.TrimSpace(c.Nick)
	if c.Nick == "" {
		c.Nick = j.Localpart()
	}
	var rooms []string
	for _, r := range c.Rooms {
		if r = strings.TrimSpace(r); r == "" {
			continue
		}
		rj, err := jid.Parse(r)
		if err != nil || rj.Localpart() == "" || rj.Resourcepart() != "" {
			return jid.JID{}, fmt.Errorf("%q is not a group chat address (room@conference.example.org)", r)
		}
		rooms = append(rooms, rj.String())
	}
	c.Rooms = rooms
	return j.Bare(), nil
}

// connect opens a session: TLS first, then the password, then a resource of our own.
func connect(ctx context.Context, c Cfg, pass string) (*xmpp.Session, error) {
	j, err := c.norm()
	if err != nil {
		return nil, err
	}
	var conn net.Conn
	if c.Server != "" {
		conn, err = (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", c.Server)
	} else {
		conn, err = dial.Client(ctx, "tcp", j)
	}
	if err != nil {
		return nil, fmt.Errorf("no answer from %s: %w", j.Domainpart(), err)
	}
	tc := &tls.Config{ServerName: j.Domainpart(), MinVersion: tls.VersionTLS12, InsecureSkipVerify: c.Insecure}
	s, err := xmpp.NewClientSession(ctx, j, conn,
		xmpp.StartTLS(tc),
		xmpp.SASL("", pass, sasl.ScramSha256Plus, sasl.ScramSha256, sasl.ScramSha1Plus, sasl.ScramSha1, sasl.Plain),
		xmpp.BindResource(),
	)
	if err != nil {
		conn.Close()
		if cond, ok := saslFailure(err); ok {
			if cond == "not-authorized" || cond == "credentials-expired" || cond == "account-disabled" {
				return nil, errors.New("login refused: check the address and password")
			}
			return nil, fmt.Errorf("login refused by %s (%s)", j.Domainpart(), cond)
		}
		return nil, fmt.Errorf("could not sign in to %s: %w", j.Domainpart(), err)
	}
	return s, nil
}

// saslFailure is the condition of the server's SASL failure (RFC 6120 §6.5), read from mellium's error by writing it back as XML.
func saslFailure(err error) (string, bool) {
	var f interface {
		error
		xml.Marshaler
	}
	if !errors.As(err, &f) {
		return "", false
	}
	b, merr := xml.Marshal(f)
	if merr != nil || !strings.Contains(string(b), "failure") {
		return "", false
	}
	var v struct {
		Any []xml.Name `xml:",any"`
	}
	xml.Unmarshal(b, &v)
	for _, n := range v.Any {
		if n.Local != "text" {
			return n.Local, true
		}
	}
	return "", true
}

// Test checks the login (the Connections page's Test button).
func Test(ctx context.Context, c Cfg, pass string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	s, err := connect(ctx, c, pass)
	if err != nil {
		return "", err
	}
	me := s.LocalAddr().Bare().String()
	s.Close()
	s.Conn().Close()
	return "login works as " + me, nil
}

// Conn is one XMPP account, a chat connection.
type Conn struct {
	Acc  string
	Name string
	Cfg  Cfg
	Pass string
	Log  *slog.Logger
	// Status reports the sign-in for the account's row.
	Status func(error)

	hub    *chat.Hub
	me     jid.JID
	cancel context.CancelFunc
	done   chan struct{}

	mu    sync.Mutex
	s     *xmpp.Session
	names map[string]string // roster names by bare address
	rooms map[string]*room  // the group chats, by bare address
}

// room is a group chat the account is in.
type room struct {
	jid  jid.JID
	nick string               // ours there (a taken one gets "_" added)
	last time.Time            // when the last line came: after a drop the room sends what came since, with a margin
	seen map[string]time.Time // the lines of the last minutes, so the ones the room sends again are dropped
}

// the margin asked of a room after a drop, for its clock and ours not agreeing; lines are remembered a while longer
const (
	historyMargin = 2 * time.Minute
	seenFor       = 10 * time.Minute
)

func ConnID(acc string) string { return "xmpp:" + acc }

func (x *Conn) ID() string         { return ConnID(x.Acc) }
func (x *Conn) Label() string      { return x.Name + " (XMPP)" }
func (x *Conn) SetHub(h *chat.Hub) { x.hub = h }

// Start is the chat.Starter for "xmpp" accounts.
func Start(log *slog.Logger) chat.Starter {
	return func(a store.Account, pass string, status func(error)) (chat.Running, error) {
		var c Cfg
		if err := json.Unmarshal([]byte(a.Config), &c); err != nil {
			return nil, errors.New("bad configuration")
		}
		x := &Conn{Acc: a.ID, Name: a.Name, Cfg: c, Pass: pass, Log: log, Status: status}
		return x, x.Start()
	}
}

// Start signs in once (a wrong password is reported right away) and then stays connected until Stop, signing in again after a drop.
func (x *Conn) Start() error {
	j, err := x.Cfg.norm()
	if err != nil {
		return err
	}
	x.me = j
	x.rooms = map[string]*room{}
	for _, r := range x.Cfg.Rooms {
		rj := jid.MustParse(r)
		x.rooms[rj.String()] = &room{jid: rj, nick: x.Cfg.Nick, seen: map[string]time.Time{}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	s, err := connect(ctx, x.Cfg, x.Pass)
	cancel()
	if err != nil {
		return err
	}
	rctx, rcancel := context.WithCancel(context.Background())
	x.cancel, x.done = rcancel, make(chan struct{})
	go x.run(rctx, s)
	return nil
}

func (x *Conn) run(ctx context.Context, s *xmpp.Session) {
	defer close(x.done)
	for {
		err := x.serve(ctx, s)
		if ctx.Err() != nil {
			return
		}
		x.Log.Warn("xmpp: connection lost, signing in again", "account", x.Name, "err", err)
		if x.Status != nil {
			x.Status(fmt.Errorf("connection lost: %v", err))
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(15 * time.Second):
			}
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			s, err = connect(cctx, x.Cfg, x.Pass)
			cancel()
			if err == nil {
				break
			}
			if x.Status != nil {
				x.Status(err)
			}
		}
	}
}

// serve handles one session until it ends: presence so the server sends us messages, the roster for names, a ping now and then.
func (x *Conn) serve(ctx context.Context, s *xmpp.Session) error {
	x.mu.Lock()
	x.s = s
	x.mu.Unlock()
	defer func() {
		x.mu.Lock()
		x.s = nil
		x.mu.Unlock()
		s.Close()
		s.Conn().Close()
	}()
	served := make(chan error, 1)
	go func() { served <- s.Serve(xmpp.HandlerFunc(x.handle)) }()
	x.loadRoster(ctx, s) // first: the messages kept for us come right after our presence
	// available at the default priority: a negative one would leave messages to your bare address (most of them) on the server
	if err := s.Send(ctx, stanza.Presence{}.Wrap(nil)); err != nil {
		return err
	}
	x.mu.Lock()
	rooms := make([]*room, 0, len(x.rooms))
	for _, r := range x.rooms {
		rooms = append(rooms, r)
	}
	x.mu.Unlock()
	for _, r := range rooms {
		if err := x.join(ctx, s, r); err != nil {
			return err
		}
	}
	if x.Status != nil {
		x.Status(nil)
	}
	tick := time.NewTicker(2 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-served:
			if err == nil {
				err = io.EOF
			}
			return err
		case <-tick.C:
			// a whitespace keepalive (RFC 6120 §4.6.1) finds a dead connection
			if _, err := s.Conn().Write([]byte(" ")); err != nil {
				return err
			}
		}
	}
}

// join enters a group chat: the first time without its history (nothing is replayed), after a drop with what came since.
func (x *Conn) join(ctx context.Context, s *xmpp.Session, r *room) error {
	x.mu.Lock()
	nick, last := r.nick, r.last
	x.mu.Unlock()
	h := xml.StartElement{Name: xml.Name{Local: "history"}, Attr: []xml.Attr{{Name: xml.Name{Local: "maxstanzas"}, Value: "0"}}}
	if !last.IsZero() {
		h.Attr = []xml.Attr{{Name: xml.Name{Local: "since"}, Value: last.Add(-historyMargin).UTC().Format(time.RFC3339)}}
	}
	to, err := r.jid.WithResource(nick)
	if err != nil {
		return err
	}
	return s.Send(ctx, stanza.Presence{To: to}.Wrap(xmlstream.Wrap(xmlstream.Wrap(nil, h), xml.StartElement{Name: xml.Name{Space: nsMUC, Local: "x"}})))
}

const nsMUC = "http://jabber.org/protocol/muc"

func (x *Conn) loadRoster(ctx context.Context, s *xmpp.Session) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	names := map[string]string{}
	it := roster.Fetch(ctx, s)
	for it.Next() {
		if i := it.Item(); i.Name != "" {
			names[i.JID.Bare().String()] = i.Name
		}
	}
	it.Close()
	x.mu.Lock()
	x.names = names
	x.mu.Unlock()
}

// Stop signs out.
func (x *Conn) Stop() {
	if x.cancel != nil {
		x.cancel()
		<-x.done
	}
}

type message struct {
	stanza.Message
	Body  string `xml:"body"`
	Delay *struct {
		Stamp string `xml:"stamp,attr"`
	} `xml:"urn:xmpp:delay delay"`
	Error *struct {
		Text string     `xml:"urn:ietf:params:xml:ns:xmpp-stanzas text"`
		Any  []xml.Name `xml:",any"`
	} `xml:"error"`
}

// decode reads the element the handler was given: the decoder must see its start too, or it refuses the end.
func decode(t xml.TokenReader, start *xml.StartElement, v any) error {
	return xml.NewTokenDecoder(xmlstream.MultiReader(xmlstream.Token(*start), t)).Decode(v)
}

// handle takes the chat messages; IQs we don't know are answered by mellium with an error, as they must be.
func (x *Conn) handle(t xmlstream.TokenReadEncoder, start *xml.StartElement) error {
	if start.Name.Local == "presence" {
		return x.presence(t, start)
	}
	if start.Name.Local != "message" {
		return nil
	}
	var m message
	if err := decode(t, start, &m); err != nil && err != io.EOF {
		return nil
	}
	if m.Type == stanza.ErrorMessage {
		why := "not delivered"
		if m.Error != nil {
			for _, n := range m.Error.Any {
				if n.Local != "text" {
					why = n.Local
				}
			}
		}
		x.Log.Warn("xmpp: a message was refused", "account", x.Name, "to", m.From.Bare().String(), "why", why)
		return nil
	}
	ts := time.Now()
	if m.Delay != nil { // kept by the server while we were away
		if d, err := time.Parse(time.RFC3339, m.Delay.Stamp); err == nil {
			ts = d
		}
	}
	x.mu.Lock()
	r := x.rooms[m.From.Bare().String()]
	x.mu.Unlock()
	if r != nil {
		x.groupMessage(r, m, ts)
		return nil
	}
	if m.Body == "" || (m.Type != stanza.ChatMessage && m.Type != stanza.NormalMessage) || m.From.Bare().Equal(x.me) {
		return nil
	}
	from := m.From.Bare()
	x.mu.Lock()
	name := x.names[from.String()]
	x.mu.Unlock()
	if name == "" {
		name = from.Localpart()
	}
	if err := x.hub.In(store.ChatMessage{Conn: x.ID(), Peer: x.addr(from), PeerName: name, Body: m.Body, TS: ts.Unix()}); err != nil {
		x.Log.Warn("xmpp: message not kept", "account", x.Name, "err", err)
	}
	return nil
}

// groupMessage passes on a line said in a group chat, named "Bob (room)". Our own lines (the room repeats them), the topic and
// private messages through the room are not passed on.
func (x *Conn) groupMessage(r *room, m message, ts time.Time) {
	who := m.From.Resourcepart()
	if m.Type != stanza.GroupChatMessage || m.Body == "" || who == "" {
		return
	}
	now, key := time.Now(), who+"\x00"+m.Body
	x.mu.Lock()
	nick := r.nick
	_, old := r.seen[key]
	old = old && m.Delay != nil // only history repeats; someone saying the same thing again is passed on
	for k, at := range r.seen {
		if now.Sub(at) > seenFor {
			delete(r.seen, k)
		}
	}
	r.seen[key], r.last = now, now
	x.mu.Unlock()
	if old || who == nick {
		return
	}
	if err := x.hub.In(store.ChatMessage{Conn: x.ID(), Peer: x.roomAddr(r.jid), PeerName: who + " (" + r.jid.Localpart() + ")", Body: m.Body, TS: ts.Unix()}); err != nil {
		x.Log.Warn("xmpp: message not kept", "account", x.Name, "err", err)
	}
}

// presence reads the group chats' answers to joining: a taken nickname is tried again with "_" added; other refusals are logged.
func (x *Conn) presence(t xmlstream.TokenReadEncoder, start *xml.StartElement) error {
	var p struct {
		stanza.Presence
		Error *struct {
			Any []xml.Name `xml:",any"`
		} `xml:"error"`
	}
	if err := decode(t, start, &p); err != nil && err != io.EOF {
		return nil
	}
	if p.Type != stanza.ErrorPresence {
		return nil
	}
	x.mu.Lock()
	r := x.rooms[p.From.Bare().String()]
	x.mu.Unlock()
	if r == nil || p.Error == nil {
		return nil
	}
	why := "refused"
	for _, n := range p.Error.Any {
		if n.Local != "text" {
			why = n.Local
		}
	}
	x.Log.Warn("xmpp: could not join a group chat", "account", x.Name, "room", r.jid.String(), "why", why)
	x.mu.Lock()
	retry := why == "conflict" && len(r.nick) < 40
	if retry {
		r.nick += "_"
	}
	s := x.s
	x.mu.Unlock()
	if retry && s != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			x.join(ctx, s, r)
		}()
	}
	return nil
}

// ---- addresses

// roomAddr is how a group chat reaches your devices: "#room", or "#room=conference.example.org" when two of the account's group
// chats share the name.
func (x *Conn) roomAddr(j jid.JID) string {
	x.mu.Lock()
	defer x.mu.Unlock()
	for k, r := range x.rooms {
		if k != j.String() && r.jid.Localpart() == j.Localpart() {
			return "#" + j.Localpart() + "=" + j.Domainpart()
		}
	}
	return "#" + j.Localpart()
}

// room finds a group chat of the account from "#room" or "#room=conference.example.org".
func (x *Conn) room(peer string) *room {
	name, service, _ := strings.Cut(strings.TrimPrefix(peer, "#"), "=")
	x.mu.Lock()
	defer x.mu.Unlock()
	var found *room
	for _, r := range x.rooms {
		if r.jid.Localpart() == name && (service == "" || r.jid.Domainpart() == service) {
			if found != nil {
				return nil // two share the name: the address must say which
			}
			found = r
		}
	}
	return found
}

func (x *Conn) addr(j jid.JID) string {
	if j.Domainpart() == x.me.Domainpart() && j.Localpart() != "" {
		return j.Localpart()
	}
	if j.Localpart() == "" {
		return "=" + j.Domainpart()
	}
	return j.Localpart() + "=" + j.Domainpart()
}

// target reads an address: "bob", "bob=example.org" or bob@example.org.
func (x *Conn) target(peer string) (jid.JID, error) {
	p := strings.TrimSpace(peer)
	switch {
	case strings.Contains(p, "@"):
	case strings.Contains(p, "="):
		p = strings.Replace(p, "=", "@", 1)
		p = strings.TrimPrefix(p, "@")
	default:
		p += "@" + x.me.Domainpart()
	}
	j, err := jid.Parse(p)
	if err != nil || j.Domainpart() == "" {
		return jid.JID{}, chat.ErrNoRoute
	}
	return j.Bare(), nil
}

// Send passes a message on as a chat message, or to a group chat ("#room"); the server says later when nobody is there (logged).
func (x *Conn) Send(ctx context.Context, _, peer, body, _ string) error {
	typ := stanza.ChatMessage
	var to jid.JID
	if strings.HasPrefix(strings.TrimSpace(peer), "#") {
		r := x.room(strings.TrimSpace(peer))
		if r == nil {
			return chat.ErrNoRoute // only the account's own group chats
		}
		to, typ = r.jid, stanza.GroupChatMessage
	} else {
		var err error
		if to, err = x.target(peer); err != nil {
			return err
		}
	}
	x.mu.Lock()
	s := x.s
	x.mu.Unlock()
	if s == nil {
		return fmt.Errorf("%w: not connected", chat.ErrUnavailable)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	msg := stanza.Message{To: to, Type: typ, ID: rand.Text()}
	if err := s.Send(ctx, msg.Wrap(xmlstream.Wrap(xmlstream.Token(xml.CharData(body)), xml.StartElement{Name: xml.Name{Local: "body"}}))); err != nil {
		return fmt.Errorf("%w: %v", chat.ErrUnavailable, err)
	}
	return nil
}
