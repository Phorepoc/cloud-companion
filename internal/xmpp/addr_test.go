// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package xmpp

import (
	"errors"
	"testing"

	"github.com/phorepoc/cloud-companion/internal/chat"
	"mellium.im/xmpp/jid"
)

// The addresses a SIP phone can type (no "@"), and that every form is read back as the same bare address.
func TestAddresses(t *testing.T) {
	x := &Conn{me: jid.MustParse("alice@example.org")}
	for in, want := range map[string]string{"bob@example.org/phone": "bob", "bob@other.net": "bob=other.net", "other.net": "=other.net"} {
		if a := x.addr(jid.MustParse(in)); a != want {
			t.Fatalf("%s written as %q, want %q", in, a, want)
		}
	}
	for in, want := range map[string]string{"bob": "bob@example.org", "Bob": "bob@example.org", "bob=other.net": "bob@other.net",
		"bob@other.net": "bob@other.net", "=other.net": "other.net"} {
		if j, err := x.target(in); err != nil || j.String() != want {
			t.Fatalf("%q read as %q (%v), want %q", in, j, err, want)
		}
	}
	if _, err := x.target("bad name="); !errors.Is(err, chat.ErrNoRoute) {
		t.Fatalf("a bad address: %v", err)
	}
}

// The settings: the address is needed, a server without a port gets 5222.
func TestCfg(t *testing.T) {
	if _, err := (&Cfg{JID: "example.org"}).norm(); err == nil {
		t.Fatal("an address without a name is taken")
	}
	c := Cfg{JID: " me@example.org ", Server: "xmpp.example.org"}
	if j, err := c.norm(); err != nil || j.String() != "me@example.org" || c.Server != "xmpp.example.org:5222" {
		t.Fatalf("%v %v %q", j, err, c.Server)
	}
}

// Two group chats with the same name on different services need the long address; one alone is "#name".
func TestRoomAddresses(t *testing.T) {
	x := &Conn{rooms: map[string]*room{}}
	for _, r := range []string{"team@conference.a.org", "team@muc.b.org", "ops@conference.a.org"} {
		j := jid.MustParse(r)
		x.rooms[r] = &room{jid: j}
	}
	if a := x.roomAddr(jid.MustParse("ops@conference.a.org")); a != "#ops" {
		t.Fatal(a)
	}
	if a := x.roomAddr(jid.MustParse("team@muc.b.org")); a != "#team=muc.b.org" {
		t.Fatal(a)
	}
	if x.room("#team") != nil {
		t.Fatal("#team chose one of two")
	}
	if r := x.room("#team=muc.b.org"); r == nil || r.jid.String() != "team@muc.b.org" {
		t.Fatal("the long address")
	}
	if r := x.room("#ops"); r == nil || x.room("#nope") != nil {
		t.Fatal("#ops / #nope")
	}
}
