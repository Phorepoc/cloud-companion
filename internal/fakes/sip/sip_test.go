// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package sip

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/phorepoc/cloud-companion/internal/sipsvc"
)

// The app's own client against the fake: a wrong password is refused, the right one logs in, and the test leaves no registration.
func TestLogin(t *testing.T) {
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := c.LocalAddr().String()
	c.Close()
	s := New(User, Pass)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Serve(ctx, addr)
	time.Sleep(200 * time.Millisecond)

	cfg := sipsvc.Cfg{Server: addr, Domain: "asterisk", User: User}
	if _, err := sipsvc.Test(ctx, cfg, "definitely-wrong"); err == nil || !strings.Contains(err.Error(), "login refused") {
		t.Fatalf("wrong password: %v", err)
	}
	for _, tr := range []string{"udp", "tcp"} {
		cfg.Transport = tr
		got, err := sipsvc.Test(ctx, cfg, Pass)
		if err != nil || got != "login works as cloud-companion@asterisk" {
			t.Fatalf("%s: %q %v", tr, got, err)
		}
	}
	if r := s.Registered(User); r != "" {
		t.Fatalf("a test registered %q", r)
	}
}
