// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

//go:build matrix

// Against a real homeserver (testdata/matrix/test.sh runs Conduit next to it): Cloud Companion signs in as alice; bob is another
// encrypted client (the same connection on a store of its own). Direct chats are end-to-end encrypted both ways; a group is
// answered through its room address.

package matrix

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
)

var quiet = func() *slog.Logger {
	if os.Getenv("MATRIX_LOG") != "" {
		return slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}()

func register(t *testing.T, hs, user string) {
	cli, _ := mautrix.NewClient(hs, "", "")
	if _, err := cli.RegisterDummy(context.Background(), &mautrix.ReqRegister[any]{Username: user, Password: "pw-" + user + "-123"}); err != nil {
		t.Fatalf("register %s: %v", user, err)
	}
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

// side is one Matrix account with its own store and a login "me" that gets everything.
func side(t *testing.T, hs, user string) (*Conn, *store.Store, *chat.Hub) {
	st := store.OpenTest(t)
	st.AddSIPUser("me")
	hub := chat.NewHub(st)
	c := &Conn{Acc: user, Name: user, Cfg: Cfg{Homeserver: hs, User: user}, Pass: "pw-" + user + "-123", Dir: t.TempDir(), Log: quiet}
	if os.Getenv("MATRIX_LOG") != "" {
		c.Trace = os.Stderr
	}
	hub.Add(c)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Stop)
	return c, st, hub
}

func TestConduit(t *testing.T) {
	hs := os.Getenv("MATRIX")
	for _, u := range []string{"alice", "bob", "carol"} {
		register(t, hs, u)
	}
	if _, err := Test(context.Background(), Cfg{Homeserver: hs, User: "alice"}, "wrong"); err == nil || !strings.Contains(err.Error(), "login refused") {
		t.Fatalf("a wrong password: %v", err)
	}
	if d, err := Test(context.Background(), Cfg{Homeserver: hs, User: "alice"}, "pw-alice-123"); err != nil || !strings.Contains(d, "@alice:test.local") {
		t.Fatalf("the right password: %q %v", d, err)
	}

	alice, ast, ahub := side(t, hs, "alice") // Cloud Companion
	ast.SetChatRoutes([]store.ChatRoute{{Kind: "prefix", Value: "mx.", Conn: alice.ID()}})
	bob, bst, _ := side(t, hs, "bob")
	time.Sleep(time.Second) // both devices have uploaded their keys

	// Cloud Companion starts an encrypted direct chat
	if err := ahub.Send(context.Background(), "n9", "mx.bob", "hello bob, from the N9"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "bob decrypts it", func() bool { return len(bst.ChatFor("me")) == 1 })
	if m := bst.ChatFor("me")[0]; m.Body != "hello bob, from the N9" || m.Peer != "alice" {
		t.Fatalf("bob got %+v", m)
	}
	rooms := alice.directs(context.Background())[id.UserID("@bob:test.local")]
	if len(rooms) != 1 {
		t.Fatalf("direct chats with bob: %v", rooms)
	}
	if enc, _ := alice.Client().StateStore.IsEncrypted(context.Background(), rooms[0]); !enc {
		t.Fatal("the direct chat is not encrypted")
	}

	// bob answers in the same chat; it reaches your devices from an address that answers back
	if err := bob.Send(context.Background(), "", "alice", "hi alice", ""); err != nil {
		t.Fatal(err)
	}
	if r := bob.directs(context.Background())[id.UserID("@alice:test.local")]; len(r) != 1 || r[0] != rooms[0] {
		t.Fatalf("bob answered in another room: %v vs %v", r, rooms)
	}
	waitFor(t, "alice decrypts bob's answer", func() bool { return len(ast.ChatFor("me")) == 1 })
	m := ast.ChatFor("me")[0]
	if m.Body != "hi alice" || m.Peer != "bob" || !strings.HasPrefix(m.PeerName, "bob") || ahub.Address(m.Conn, m.Peer) != "mx.bob" {
		t.Fatalf("alice got %+v (address %q)", m, ahub.Address(m.Conn, m.Peer))
	}
	ast.ChatTaken("me", m.ID)

	// Conduit gives new users a display name with ⚡️ after it, so names are checked by how they start.

	// a group: bob, alice (joins by herself) and carol
	g, err := bob.Client().CreateRoom(context.Background(), &mautrix.ReqCreateRoom{Name: "Team", Preset: "private_chat",
		Invite: []id.UserID{"@alice:test.local", "@carol:test.local"}})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "alice joins the group", func() bool { return alice.Client().StateStore.IsInRoom(context.Background(), g.RoomID, alice.Me()) })
	time.Sleep(500 * time.Millisecond)
	if _, err := bob.Client().SendText(context.Background(), g.RoomID, "group hello"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the group message", func() bool { return len(ast.ChatFor("me")) == 1 })
	m = ast.ChatFor("me")[0]
	if m.Body != "group hello" || m.Peer != strings.Replace(string(g.RoomID), ":", "=", 1) || !strings.HasPrefix(m.PeerName, "bob") || !strings.HasSuffix(m.PeerName, "(Team)") {
		t.Fatalf("group message %+v", m)
	}
	if err := ahub.Send(context.Background(), "n9", ahub.Address(m.Conn, m.Peer), "hi team"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "bob sees the answer in the group", func() bool {
		for _, x := range bst.ChatFor("me") {
			if x.Body == "hi team" && strings.HasPrefix(x.PeerName, "alice") && strings.HasSuffix(x.PeerName, "(Team)") {
				return true
			}
		}
		return false
	})

	// someone who does not exist
	if err := ahub.Send(context.Background(), "n9", "mx.nobody", "x"); err == nil {
		t.Fatal("a message to nobody went out")
	}
}
