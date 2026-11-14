// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package chat

import (
	"context"
	"errors"
	"testing"

	"github.com/phorepoc/cloud-companion/internal/store"
)

// stub is a connection that records what it was asked to send.
type stub struct {
	id   string
	sent []string
}

func (s *stub) ID() string    { return s.id }
func (s *stub) Label() string { return s.id }
func (s *stub) Send(_ context.Context, from, peer, body, opt string) error {
	s.sent = append(s.sent, from+">"+peer+":"+body+"/"+opt)
	return nil
}

func TestDefaultsWithoutRules(t *testing.T) {
	st := store.OpenTest(t)
	h := NewHub(st)
	st.AddSIPUser("e7")
	if err := h.Send(context.Background(), "n9", "+491715550100", "hi"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("a number with no SMS phone: %v", err)
	}
	st.SMSUpload("pixel", []store.SMS{{DevMsgID: "1", Number: "+49 171 5550100", Dir: "in", Body: "x", TS: 1}})
	if err := h.Send(context.Background(), "n9", "+491715550100", "hi"); err != nil {
		t.Fatal(err)
	}
	if q := st.SMSPending("pixel"); len(q) != 1 || q[0].Body != "hi" {
		t.Fatalf("not queued as SMS: %+v", q)
	}
	if err := h.Send(context.Background(), "n9", "E7", "to my other phone"); err != nil {
		t.Fatal(err)
	}
	if m := st.ChatFor("e7"); len(m) != 1 || m[0].Peer != "n9" || m[0].Body != "to my other phone" {
		t.Fatalf("device to device: %+v", m)
	}
	if err := h.Send(context.Background(), "n9", "nobody", "x"); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("unknown address: %v", err)
	}
}

func TestRulesPickTheConnectionAndRepliesComeBackTheSameWay(t *testing.T) {
	st := store.OpenTest(t)
	h := NewHub(st)
	sig, irc := &stub{id: "signal"}, &stub{id: "irc:libera"}
	h.Add(sig)
	h.Add(irc)
	if err := st.SetChatRoutes([]store.ChatRoute{
		{Kind: "prefix", Value: "signal.", Conn: "signal"},
		{Kind: "prefix", Value: "libera.", Conn: "irc:libera", Opt: "#etesian"},
		{Kind: "number", Conn: "signal"}, // numbers go to Signal instead of SMS
	}); err != nil {
		t.Fatal(err)
	}
	h.Send(context.Background(), "n9", "signal.+491715550100", "a")
	h.Send(context.Background(), "n9", "Libera.alice", "b")
	h.Send(context.Background(), "n9", "+491715550100", "c")
	if len(sig.sent) != 2 || sig.sent[0] != "n9>+491715550100:a/" || sig.sent[1] != "n9>+491715550100:c/" {
		t.Fatalf("signal got %v", sig.sent)
	}
	if len(irc.sent) != 1 || irc.sent[0] != "n9>alice:b/#etesian" {
		t.Fatalf("irc got %v", irc.sent)
	}
	// the address a device sees for a message from each connection answers back through it
	if a := h.Address("irc:libera", "alice"); a != "libera.alice" {
		t.Fatalf("irc reply address %q", a)
	}
	if a := h.Address("signal", "+491715550100"); a != "+491715550100" { // the number rule comes after the prefix? no: first rule for signal is the prefix
		if a != "signal.+491715550100" {
			t.Fatalf("signal reply address %q", a)
		}
	}
	for _, a := range []string{h.Address("irc:libera", "alice"), h.Address("signal", "+491715550100")} {
		if c, _, _, err := h.Route(a); err != nil || (c.ID() != "irc:libera" && c.ID() != "signal") {
			t.Fatalf("%q does not route back: %v", a, err)
		}
	}
	if a := h.Address("matrix:home", "@bob:example.org"); a != "matrix:home.@bob:example.org" {
		t.Fatalf("no rule yet: %q", a)
	}
	// a rule naming a connection that is not set up
	st.SetChatRoutes([]store.ChatRoute{{Kind: "prefix", Value: "tg.", Conn: "telegram"}})
	if err := h.Send(context.Background(), "n9", "tg.alice", "x"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing connection: %v", err)
	}
	if err := st.SetChatRoutes([]store.ChatRoute{{Kind: "prefix", Conn: "signal"}}); err == nil {
		t.Fatal("a prefix rule without a prefix was accepted")
	}
}

func TestWhoGetsWhichConnection(t *testing.T) {
	st := store.OpenTest(t)
	h := NewHub(st)
	st.AddSIPUser("n9")
	st.AddSIPUser("e7")
	if err := st.SetChatDeliver("irc:libera", []string{"e7"}); err != nil {
		t.Fatal(err)
	}
	h.In(store.ChatMessage{Conn: "irc:libera", Peer: "alice", Body: "only for the E7"})
	h.In(store.ChatMessage{Conn: "sms", Peer: "+491715550100", Body: "for everyone"})
	if m := st.ChatFor("n9"); len(m) != 1 || m[0].Body != "for everyone" {
		t.Fatalf("n9 got %+v", m)
	}
	m := st.ChatFor("e7")
	if len(m) != 2 {
		t.Fatalf("e7 got %+v", m)
	}
	st.ChatTaken("e7", m[0].ID)
	if m := st.ChatFor("e7"); len(m) != 1 || m[0].Body != "for everyone" {
		t.Fatalf("after taking the first: %+v", m)
	}
	if err := st.SetChatDeliver("sms", []string{"nobody"}); err == nil {
		t.Fatal("an unknown login was accepted")
	}
}

func TestSMSComeIntoTheInboxOnce(t *testing.T) {
	st := store.OpenTest(t)
	h := NewHub(st)
	s := &SMS{St: st}
	st.SMSUpload("pixel", []store.SMS{{DevMsgID: "1", Number: "+49 171 5550100", Dir: "in", Body: "old", TS: 1}})
	s.Poll(h) // a fresh server starts after the newest SMS
	st.AddSIPUser("n9")
	st.SMSUpload("pixel", []store.SMS{{DevMsgID: "2", Number: "+49 171 5550100", Dir: "in", Body: "new", TS: 2},
		{DevMsgID: "3", Number: "+49 171 5550100", Dir: "out", Body: "sent by the phone", TS: 3}})
	s.Poll(h)
	s.Poll(h)
	m := st.ChatFor("n9")
	if len(m) != 1 || m[0].Body != "new" || m[0].Conn != "sms" || m[0].Peer != "+491715550100" || m[0].PeerName != "+49 171 5550100" {
		t.Fatalf("inbox: %+v", m)
	}
}
