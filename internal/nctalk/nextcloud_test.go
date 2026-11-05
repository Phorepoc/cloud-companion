// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

//go:build nctalk

// Against a real Nextcloud with Talk (testdata/nctalk/test.sh): Cloud Companion signs in as alice; bob and carol use Talk's API
// as their apps would. One-to-one chats both ways (one made by Cloud Companion), a group answered through its #token, mentions
// written out, a user nobody has refused.

package nctalk

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func cfg(u string) Cfg   { return Cfg{URL: os.Getenv("NEXTCLOUD"), User: u} }
func pw(u string) string { return "pw-" + u + "-12345" }

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; !ok(); i++ {
		if i > 300 {
			t.Fatal("timed out: " + what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func inbox(st *store.Store, peer, body string) *store.ChatMessage {
	for _, m := range st.ChatFor("me") {
		if m.Peer == peer && m.Body == body {
			return &m
		}
	}
	return nil
}

// said is whether user sees text from alice in the conversation.
func said(t *testing.T, user, token, text string) bool {
	var msgs []message
	if _, err := newAPI(cfg(user), pw(user)).call(context.Background(), "GET", "/apps/spreed/api/v1/chat/"+token+"?lookIntoFuture=0&limit=50", nil, &msgs); err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if m.ActorID == "alice" && m.Message == text {
			return true
		}
	}
	return false
}

func TestNextcloudTalk(t *testing.T) {
	ctx := context.Background()
	if _, err := Test(ctx, cfg("alice"), "wrong"); !errors.Is(err, errLogin) {
		t.Fatalf("a wrong password: %v", err)
	}
	if d, err := Test(ctx, cfg("alice"), pw("alice")); err != nil || d != "login works as alice, with Talk" {
		t.Fatalf("the right password: %q %v", d, err)
	}
	t.Log("a wrong password is refused, the right one works")

	bob, carol := newAPI(cfg("bob"), pw("bob")), newAPI(cfg("carol"), pw("carol"))
	var dm room
	if _, err := bob.call(ctx, "POST", "/apps/spreed/api/v4/room", url.Values{"roomType": {"1"}, "invite": {"alice"}}, &dm); err != nil {
		t.Fatal(err)
	}
	bob.call(ctx, "POST", "/apps/spreed/api/v1/chat/"+dm.Token, url.Values{"message": {"from before"}}, nil)

	st := store.OpenTest(t)
	st.AddSIPUser("me")
	hub := chat.NewHub(st)
	alice := &Conn{Acc: "a", Name: "cloud", Cfg: cfg("alice"), Pass: pw("alice"), Log: quiet, Every: 500 * time.Millisecond}
	hub.Add(alice)
	if err := alice.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(alice.Stop)

	bob.call(ctx, "POST", "/apps/spreed/api/v1/chat/"+dm.Token, url.Values{"message": {"hello alice"}}, nil)
	waitFor(t, "bob's message", func() bool { return inbox(st, "bob", "hello alice") != nil })
	if inbox(st, "bob", "from before") != nil {
		t.Fatal("a message from before the account was connected was replayed")
	}
	if err := alice.Send(ctx, "me", "bob", "hi bob", ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the answer at bob", func() bool { return said(t, "bob", dm.Token, "hi bob") })
	t.Log("a one-to-one chat both ways; what came before is not replayed")

	if err := alice.Send(ctx, "me", "carol", "hi carol, new chat", ""); err != nil {
		t.Fatal(err)
	}
	var crooms []room
	carol.call(ctx, "GET", "/apps/spreed/api/v4/room", nil, &crooms)
	var cdm string
	for _, r := range crooms {
		if r.Type == oneToOne && r.Name == "alice" {
			cdm = r.Token
		}
	}
	if cdm == "" || !said(t, "carol", cdm, "hi carol, new chat") {
		t.Fatal("no new one-to-one chat with carol")
	}
	carol.call(ctx, "POST", "/apps/spreed/api/v1/chat/"+cdm, url.Values{"message": {"got it"}}, nil)
	waitFor(t, "carol's answer in the new chat", func() bool { return inbox(st, "carol", "got it") != nil })
	if err := alice.Send(ctx, "me", "nobody", "anyone?", ""); !errors.Is(err, chat.ErrNoRoute) {
		t.Fatalf("a user nobody has: %v", err)
	}
	t.Log("a new chat made for carol; a user nobody has is refused")

	var grp room
	if _, err := bob.call(ctx, "POST", "/apps/spreed/api/v4/room", url.Values{"roomType": {"2"}, "roomName": {"Team"}}, &grp); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"alice", "carol"} {
		if _, err := bob.call(ctx, "POST", "/apps/spreed/api/v4/room/"+grp.Token+"/participants", url.Values{"newParticipant": {u}, "source": {"users"}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	bob.call(ctx, "POST", "/apps/spreed/api/v1/chat/"+grp.Token, url.Values{"message": {"@alice are you in?"}}, nil)
	var m *store.ChatMessage
	waitFor(t, "the group message", func() bool {
		for _, x := range st.ChatFor("me") {
			if x.Peer == "#"+grp.Token && strings.HasSuffix(x.Body, "are you in?") {
				m = &x
				return true
			}
		}
		return false
	})
	if m.Body != "@alice are you in?" || m.PeerName != "bob (Team)" {
		t.Fatalf("%q from %q", m.Body, m.PeerName)
	}
	if err := alice.Send(ctx, "me", "#"+grp.Token, "yes", ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the answer in the group", func() bool { return said(t, "carol", grp.Token, "yes") })
	if err := alice.Send(ctx, "me", "#nosuchroom", "x", ""); !errors.Is(err, chat.ErrNoRoute) {
		t.Fatalf("a conversation that is not there: %v", err)
	}
	t.Log("a group: the mention written out, named bob (Team), answered through its #token")
}
