// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

//go:build xmpp

// Against a real server (testdata/xmpp/test.sh runs Prosody next to it): Cloud Companion signs in as alice; bob is another client.
// Messages go both ways, roster names are used, and a message kept by the server while alice was away arrives when she is back.

package xmpp

import (
	"context"
	"encoding/xml"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
	"mellium.im/xmlstream"
	"mellium.im/xmpp"
	"mellium.im/xmpp/jid"
	"mellium.im/xmpp/roster"
	"mellium.im/xmpp/stanza"
)

var quiet = func() *slog.Logger {
	if os.Getenv("XMPP_LOG") != "" {
		return slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}()

func cfg(user string) Cfg {
	return Cfg{JID: user + "@localhost", Server: os.Getenv("XMPP"), Insecure: true}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; !ok(); i++ {
		if i > 300 {
			t.Fatal("timed out: " + what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// client is a plain XMPP client that keeps the bodies it gets.
type client struct {
	s   *xmpp.Session
	mu  sync.Mutex
	got []string
}

func dialClient(t *testing.T, user string) *client {
	s, err := connect(context.Background(), cfg(user), "pw-"+user)
	if err != nil {
		t.Fatal(err)
	}
	c := &client{s: s}
	go s.Serve(xmpp.HandlerFunc(func(tr xmlstream.TokenReadEncoder, start *xml.StartElement) error {
		var m message
		if start.Name.Local == "message" && decode(tr, start, &m) == nil && m.Body != "" {
			c.mu.Lock()
			c.got = append(c.got, m.From.Bare().String()+": "+m.Body)
			c.mu.Unlock()
		}
		return nil
	}))
	s.Send(context.Background(), stanza.Presence{}.Wrap(nil))
	t.Cleanup(func() { s.Close(); s.Conn().Close() })
	return c
}

func (c *client) say(t *testing.T, to, body string) {
	msg := stanza.Message{To: jid.MustParse(to), Type: stanza.ChatMessage}
	if err := c.s.Send(context.Background(), msg.Wrap(xmlstream.Wrap(xmlstream.Token(xml.CharData(body)), xml.StartElement{Name: xml.Name{Local: "body"}}))); err != nil {
		t.Fatal(err)
	}
}

func (c *client) has(s string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, g := range c.got {
		if g == s {
			return true
		}
	}
	return false
}

func inbox(st *store.Store, peer, body string) bool {
	for _, m := range st.ChatFor("me") {
		if m.Peer == peer && m.Body == body {
			return true
		}
	}
	return false
}

func TestProsody(t *testing.T) {
	if _, err := Test(context.Background(), cfg("alice"), "wrong"); err == nil || !strings.Contains(err.Error(), "login refused") {
		t.Fatalf("a wrong password: %v", err)
	}
	if d, err := Test(context.Background(), cfg("alice"), "pw-alice"); err != nil || d != "login works as alice@localhost" {
		t.Fatalf("the right password: %q %v", d, err)
	}

	// a message to alice while she is away, kept by the server (offline storage)
	bob := dialClient(t, "bob")
	bob.say(t, "alice@localhost", "while you were away")
	time.Sleep(500 * time.Millisecond)

	// alice names bob in her roster, as another client of hers would
	a0, err := connect(context.Background(), cfg("alice"), "pw-alice")
	if err != nil {
		t.Fatal(err)
	}
	go a0.Serve(nil)
	if err := roster.Set(context.Background(), a0, roster.Item{JID: jid.MustParse("bob@localhost"), Name: "Bob Builder"}); err != nil {
		t.Fatal(err)
	}
	a0.Close()
	a0.Conn().Close()

	st := store.OpenTest(t)
	st.AddSIPUser("me")
	hub := chat.NewHub(st)
	var mu sync.Mutex
	signedIn := 0
	alice := &Conn{Acc: "a", Name: "alice", Cfg: cfg("alice"), Pass: "pw-alice", Log: quiet, Status: func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if err == nil {
			signedIn++
		}
	}}
	hub.Add(alice)
	if err := alice.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(alice.Stop)
	waitFor(t, "the message kept while away", func() bool { return inbox(st, "bob", "while you were away") })
	t.Log("a message kept by the server while away arrives")

	bob.say(t, "alice@localhost", "hello alice")
	waitFor(t, "bob's message in the inbox", func() bool { return inbox(st, "bob", "hello alice") })
	for _, m := range st.ChatFor("me") {
		if m.Peer == "bob" && m.PeerName != "Bob Builder" {
			t.Fatalf("the roster name is not used: %q", m.PeerName)
		}
	}
	t.Log("bob's message arrives with his roster name")

	if err := alice.Send(context.Background(), "me", "bob", "hi bob", ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the answer at bob", func() bool { return bob.has("alice@localhost: hi bob") })
	if err := alice.Send(context.Background(), "me", "bob=localhost", "the long form", ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the long form at bob", func() bool { return bob.has("alice@localhost: the long form") })
	t.Log("answers reach bob, short and long address")

	// the connection drops: alice signs in again by herself
	alice.mu.Lock()
	alice.s.Conn().Close()
	alice.mu.Unlock()
	waitFor(t, "signed in again", func() bool { mu.Lock(); defer mu.Unlock(); return signedIn == 2 })
	bob.say(t, "alice@localhost", "after the drop")
	waitFor(t, "a message after the drop", func() bool { return inbox(st, "bob", "after the drop") })
	t.Log("after a dropped connection alice signs in again")
}
