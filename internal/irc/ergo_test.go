// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

//go:build irc

// Against a real IRC server (testdata/irc/test.sh runs Ergo next to it): Cloud Companion is "cc" with a registered account (SASL);
// bob is another client. Private messages go both ways, channel lines reach the inbox only when they name cc, long answers are split.

package irc

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"
	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; !ok(); i++ {
		if i > 200 {
			t.Fatal("timed out: " + what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// client is a plain IRC client that keeps what is said to it or in its channels.
type client struct {
	ic  *ircevent.Connection
	mu  sync.Mutex
	got []string
}

func dial(t *testing.T, nick string) *client {
	c := &client{}
	c.ic = (Cfg{Server: os.Getenv("IRC"), Insecure: true, Nick: nick}).conn("")
	c.ic.AddCallback("PRIVMSG", func(m ircmsg.Message) {
		c.mu.Lock()
		c.got = append(c.got, m.Nick()+">"+m.Params[0]+": "+m.Params[1])
		c.mu.Unlock()
	})
	c.ic.AddCallback("NOTICE", func(m ircmsg.Message) {
		c.mu.Lock()
		c.got = append(c.got, "notice "+m.Nick()+": "+m.Params[len(m.Params)-1])
		c.mu.Unlock()
	})
	if err := c.ic.Connect(); err != nil {
		t.Fatal(err)
	}
	go c.ic.Loop()
	t.Cleanup(c.ic.Quit)
	return c
}

func (c *client) find(sub string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, g := range c.got {
		if strings.Contains(g, sub) {
			return g
		}
	}
	return ""
}

func inbox(st *store.Store, peer, body string) *store.ChatMessage {
	for _, m := range st.ChatFor("me") {
		if m.Peer == peer && m.Body == body {
			return &m
		}
	}
	return nil
}

func TestErgo(t *testing.T) {
	// cc registers its account, as you would once with /msg NickServ REGISTER
	reg := dial(t, "cc")
	reg.ic.Privmsg("NickServ", "REGISTER pw-cc-123")
	waitFor(t, "the account made", func() bool { return reg.find("Account created") != "" })
	reg.ic.Quit()
	time.Sleep(time.Second)

	cfg := Cfg{Server: os.Getenv("IRC"), Insecure: true, Nick: "cc", Channels: []string{"team"}}
	if _, err := Test(context.Background(), cfg, "wrong"); err == nil || !strings.Contains(err.Error(), "login refused") {
		t.Fatalf("a wrong password: %v", err)
	}
	if d, err := Test(context.Background(), cfg, "pw-cc-123"); err != nil || d != "connected as cc" {
		t.Fatalf("the right password: %q %v", d, err)
	}
	t.Log("a wrong password is refused, the right one connects")

	bob := dial(t, "bob")
	bob.ic.Join("#team")
	st := store.OpenTest(t)
	st.AddSIPUser("me")
	hub := chat.NewHub(st)
	cc := &Conn{Acc: "a", Name: "net", Cfg: cfg, Pass: "pw-cc-123", Log: quiet}
	hub.Add(cc)
	if err := cc.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cc.Stop)
	time.Sleep(2 * time.Second) // cc in #team

	bob.ic.Privmsg("cc", "hello cc")
	waitFor(t, "bob's private message", func() bool { return inbox(st, "bob", "hello cc") != nil })
	bob.ic.Privmsg("#team", "just chatter")
	bob.ic.Privmsg("#team", "cc: are you there?")
	waitFor(t, "the line naming cc", func() bool { return inbox(st, "#team", "cc: are you there?") != nil })
	if m := inbox(st, "#team", "cc: are you there?"); m.PeerName != "bob (#team)" {
		t.Fatalf("named %q", m.PeerName)
	}
	if inbox(st, "#team", "just chatter") != nil {
		t.Fatal("a channel line that does not name cc reached the inbox")
	}
	t.Log("a private message arrives; in a channel only the line naming cc")

	if err := cc.Send(context.Background(), "me", "bob", "hi bob", ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the answer at bob", func() bool { return bob.find("cc>bob: hi bob") != "" })
	if err := cc.Send(context.Background(), "me", "#team", "yes, here", ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the answer in #team", func() bool { return bob.find("cc>#team: yes, here") != "" })
	long := strings.TrimSpace(strings.Repeat("lorem ipsum ", 120))
	if err := cc.Send(context.Background(), "me", "bob", long, ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the long message, all of it", func() bool {
		bob.mu.Lock()
		defer bob.mu.Unlock()
		var parts []string
		for _, g := range bob.got {
			if p, ok := strings.CutPrefix(g, "cc>bob: lorem"); ok {
				parts = append(parts, "lorem"+p)
			} else if p, ok := strings.CutPrefix(g, "cc>bob: ipsum"); ok {
				parts = append(parts, "ipsum"+p)
			}
		}
		return len(parts) > 1 && strings.Join(parts, " ") == long
	})
	t.Log("answers reach bob and #team; a long one is split into lines")
}
