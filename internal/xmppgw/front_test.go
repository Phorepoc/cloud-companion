// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package xmppgw

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
	"github.com/phorepoc/cloud-companion/internal/tlsutil"
	"mellium.im/sasl"
	"mellium.im/xmlstream"
	"mellium.im/xmpp"
	"mellium.im/xmpp/jid"
	"mellium.im/xmpp/stanza"
)

// far is a connection that keeps what it was asked to send (another network, as Matrix or XMPP would be).
type far struct {
	mu   sync.Mutex
	sent []string
}

func (*far) ID() string    { return "far" }
func (*far) Label() string { return "Far away" }
func (f *far) Send(_ context.Context, from, peer, body, _ string) error {
	if peer == "nobody" {
		return chat.ErrNoRoute
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, from+">"+peer+": "+body)
	return nil
}

func (f *far) has(s string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, x := range f.sent {
		if x == s {
			return true
		}
	}
	return false
}

func start(t *testing.T) (*store.Store, *chat.Hub, *far, string) {
	st := store.OpenTest(t)
	hub := chat.NewHub(st)
	fc := &far{}
	hub.Add(fc)
	if err := st.SetChatRoutes([]store.ChatRoute{{Kind: "prefix", Value: "far.", Conn: "far"}}); err != nil {
		t.Fatal(err)
	}
	tc, _, err := tlsutil.LoadOrCreateLegacy(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := New(st, hub, tc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.Tick = 200 * time.Millisecond
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go f.Serve(ctx, l)
	go f.Run(ctx)
	return st, hub, fc, l.Addr().String()
}

type got struct{ from, nick, body, err string }

// phone is a Jabber client, as on the N9: it signs in, says it is online and keeps what arrives.
type phone struct {
	s   *xmpp.Session
	mu  sync.Mutex
	got []got
}

func dial(addr, user, pass string) (*phone, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	s, err := xmpp.NewClientSession(context.Background(), jid.MustParse(user+"@cloud-companion"), conn,
		xmpp.StartTLS(&tls.Config{InsecureSkipVerify: true}), // Cloud Companion's own certificate (the N9: "ignore certificate errors")
		xmpp.SASL("", pass, sasl.Plain),
		xmpp.BindResource(),
	)
	if err != nil {
		conn.Close()
		return nil, err
	}
	p := &phone{s: s}
	go s.Serve(xmpp.HandlerFunc(func(t xmlstream.TokenReadEncoder, start *xml.StartElement) error {
		if start.Name.Local != "message" {
			return nil
		}
		var m struct {
			stanza.Message
			Body  string `xml:"body"`
			Nick  string `xml:"http://jabber.org/protocol/nick nick"`
			Error *struct {
				Text string `xml:"urn:ietf:params:xml:ns:xmpp-stanzas text"`
			} `xml:"error"`
		}
		if decode(t, start, &m) == nil {
			g := got{from: m.From.String(), nick: m.Nick, body: m.Body}
			if m.Error != nil {
				g.err = m.Error.Text
			}
			p.mu.Lock()
			p.got = append(p.got, g)
			p.mu.Unlock()
		}
		return nil
	}))
	return p, s.Send(context.Background(), stanza.Presence{}.Wrap(nil))
}

func (p *phone) close() { p.s.Close(); p.s.Conn().Close() }

func (p *phone) say(t *testing.T, to, body string) {
	m := stanza.Message{To: jid.MustParse(to), Type: stanza.ChatMessage}
	if err := p.s.Send(context.Background(), m.Wrap(xmlstream.Wrap(xmlstream.Token(xml.CharData(body)), xml.StartElement{Name: xml.Name{Local: "body"}}))); err != nil {
		t.Fatal(err)
	}
}

func (p *phone) wait(t *testing.T, what string, ok func(got) bool) got {
	t.Helper()
	for i := 0; i < 100; i++ {
		p.mu.Lock()
		for _, g := range p.got {
			if ok(g) {
				p.mu.Unlock()
				return g
			}
		}
		p.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out: %s (got %+v)", what, p.got)
	return got{}
}

func TestSignInNeedsTheRightPassword(t *testing.T) {
	st, _, _, addr := start(t)
	pw, _ := st.AddSIPUser("n9")
	if p, err := dial(addr, "n9", "wrong"); err == nil {
		p.close()
		t.Fatal("signed in with a wrong password")
	}
	if p, err := dial(addr, "nobody", pw); err == nil {
		p.close()
		t.Fatal("signed in as a login that does not exist")
	}
	p, err := dial(addr, "n9", pw)
	if err != nil {
		t.Fatal(err)
	}
	p.close()
}

func TestMessagesBothWaysThroughAConnection(t *testing.T) {
	st, hub, fc, addr := start(t)
	pw, _ := st.AddSIPUser("n9")
	p, err := dial(addr, "n9", pw)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()

	p.say(t, "far.bob@cloud-companion", "hello bob")
	for i := 0; !fc.has("n9>bob: hello bob"); i++ {
		if i > 100 {
			t.Fatalf("not passed on: %v", fc.sent)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// an answer, from an address that needs escaping, with the sender's name
	hub.In(store.ChatMessage{Conn: "far", Peer: "bob@other.org", PeerName: "Bob", Body: "hi n9"})
	g := p.wait(t, "the answer", func(g got) bool { return g.body == "hi n9" })
	if g.from != `far.bob\40other.org@cloud-companion` || g.nick != "Bob" {
		t.Fatalf("from %q, nick %q", g.from, g.nick)
	}
	p.say(t, g.from, "and back")
	for i := 0; !fc.has("n9>bob@other.org: and back"); i++ {
		if i > 100 {
			t.Fatalf("the reply did not go back the way it came: %v", fc.sent)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestRefusedMessagesComeBackAsErrors(t *testing.T) {
	st, _, _, addr := start(t)
	pw, _ := st.AddSIPUser("n9")
	p, err := dial(addr, "n9", pw)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	p.say(t, "far.nobody@cloud-companion", "anyone?")
	g := p.wait(t, "an error", func(g got) bool { return g.err != "" })
	if g.from != "far.nobody@cloud-companion" || !strings.Contains(g.err, "No connection") {
		t.Fatalf("%+v", g)
	}
	p.say(t, "bob@example.org", "elsewhere")
	p.wait(t, "an error for another domain", func(g got) bool { return strings.Contains(g.err, "Only addresses @cloud-companion") })
}

// Device to device: a message to a login that is not signed in waits, and arrives with its time once it is.
func TestDeviceToDeviceWaitsUntilSignedIn(t *testing.T) {
	st, _, _, addr := start(t)
	pw, _ := st.AddSIPUser("n9")
	pw2, _ := st.AddSIPUser("e7")
	n9, err := dial(addr, "n9", pw)
	if err != nil {
		t.Fatal(err)
	}
	defer n9.close()
	n9.say(t, "e7@cloud-companion", "are you there?")
	time.Sleep(500 * time.Millisecond)
	e7, err := dial(addr, "e7", pw2)
	if err != nil {
		t.Fatal(err)
	}
	defer e7.close()
	g := e7.wait(t, "the waiting message", func(g got) bool { return g.body == "are you there?" })
	if g.from != "n9@cloud-companion" {
		t.Fatalf("from %q", g.from)
	}
	if len(st.ChatFor("e7")) != 0 {
		t.Fatal("still in the inbox after delivery")
	}
}

func TestEscaping(t *testing.T) {
	for _, a := range []string{"+306912345678", "mx.bob=other.org", "x.bob@other.org", `a b:c/d\e`} {
		e := Escape(a)
		if strings.ContainsAny(e, ` "&'/:<>@`) {
			t.Fatalf("%q escaped as %q", a, e)
		}
		if _, err := jid.New(e, "cloud-companion", ""); err != nil {
			t.Fatalf("%q: %v", e, err)
		}
		if Unescape(e) != a {
			t.Fatalf("%q came back as %q", a, Unescape(e))
		}
	}
}
