// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package irc

import (
	"strings"
	"testing"
)

func TestMentions(t *testing.T) {
	for line, want := range map[string]bool{"cc: hi": true, "hi CC!": true, "ping cc": true, "ccbot here": false, "accent": false, "cc_ is not": false, "x": false} {
		if mentions(line, "cc") != want {
			t.Fatalf("%q: want %v", line, want)
		}
	}
}

func TestSplit(t *testing.T) {
	long := strings.Repeat("word ", 200)
	for _, p := range split(long, 100) {
		if len(p) > 100 || strings.HasPrefix(p, " ") || strings.HasSuffix(p, " ") {
			t.Fatalf("part %q", p)
		}
	}
	greek := strings.Repeat("αβγδ", 100) // no spaces: cut between characters
	parts := split(greek, 101)
	if strings.Join(parts, "") != greek {
		t.Fatal("characters lost")
	}
	for _, p := range parts {
		if !strings.HasPrefix(p, "α") && !strings.HasPrefix(p, "β") && !strings.HasPrefix(p, "γ") && !strings.HasPrefix(p, "δ") {
			t.Fatalf("cut inside a character: %q", p[:4])
		}
	}
}

func TestCfg(t *testing.T) {
	c := Cfg{Server: "irc.example.org", Nick: "cc", Channels: []string{"team", " #ops ", ""}}
	if err := c.norm(); err != nil || c.Server != "irc.example.org:6697" || strings.Join(c.Channels, ",") != "#team,#ops" {
		t.Fatalf("%v %+v", err, c)
	}
	p := Cfg{Server: "irc.example.org", Nick: "cc", Plain: true}
	if p.norm(); p.Server != "irc.example.org:6667" {
		t.Fatal(p.Server)
	}
	if err := (&Cfg{Server: "x", Nick: "a b"}).norm(); err == nil {
		t.Fatal("a nick with a space")
	}
}
