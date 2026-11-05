// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package matrix

import (
	"testing"

	"maunium.net/go/mautrix/id"
)

// The addresses a SIP phone can type (no ":"), and that every form is read back as the same user or room.
func TestAddresses(t *testing.T) {
	m := &Conn{me: "@alice:example.org"}
	if a := m.userAddr("@bob:example.org"); a != "bob" {
		t.Fatalf("same homeserver: %q", a)
	}
	if a := m.userAddr("@bob:other.net"); a != "bob=other.net" {
		t.Fatalf("other homeserver: %q", a)
	}
	if a := roomAddr("!abc:example.org"); a != "!abc=example.org" {
		t.Fatalf("room: %q", a)
	}
	for in, want := range map[string]id.UserID{"bob": "@bob:example.org", "Bob": "@bob:example.org", "bob=other.net": "@bob:other.net",
		"@bob:other.net": "@bob:other.net"} {
		if _, _, u := m.target(in); u != want {
			t.Fatalf("%q read as %q", in, u)
		}
	}
	if r, _, _ := m.target("!abc=example.org"); r != "!abc:example.org" {
		t.Fatalf("room read as %q", r)
	}
	if r, _, _ := m.target("!abc:example.org"); r != "!abc:example.org" {
		t.Fatalf("plain room id read as %q", r)
	}
	if _, a, _ := m.target("#team=example.org"); a != "#team:example.org" {
		t.Fatalf("alias read as %q", a)
	}
}
