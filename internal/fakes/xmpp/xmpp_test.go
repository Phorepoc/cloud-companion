// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The fake against the app's own XMPP client (internal/xmpp): a wrong password is refused, the right one signs in and stays
// connected with its group chats.
package xmpp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/phorepoc/cloud-companion/internal/store"
	app "github.com/phorepoc/cloud-companion/internal/xmpp"
)

func start(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tc, err := TLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go Serve(ctx, ln, tc)
	return ln.Addr().String()
}

func TestLogin(t *testing.T) {
	addr := start(t)
	cfg := app.Cfg{JID: "cc@localhost", Server: addr, Insecure: true}
	if _, err := app.Test(context.Background(), cfg, "definitely-wrong"); err == nil || !strings.Contains(err.Error(), "login refused") {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := app.Test(context.Background(), app.Cfg{JID: "cc@localhost", Server: addr}, "pw-cc"); err == nil {
		t.Fatal("an unchecked certificate was accepted without insecure")
	}
	msg, err := app.Test(context.Background(), cfg, "pw-cc")
	if err != nil || msg != "login works as cc@localhost" {
		t.Fatalf("right password: %q %v", msg, err)
	}
}

func TestStaysConnected(t *testing.T) {
	addr := start(t)
	b, _ := json.Marshal(app.Cfg{JID: "cc@localhost", Server: addr, Insecure: true, Rooms: []string{"team@conference.localhost"}, Nick: "Cloudy"})
	status := make(chan error, 4)
	run, err := app.Start(slog.New(slog.NewTextHandler(io.Discard, nil)))(store.Account{ID: "a", Name: "My XMPP", Config: string(b)}, "pw-cc", func(e error) { status <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer run.Stop()
	select {
	case err := <-status:
		if err != nil {
			t.Fatalf("status: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("never connected")
	}
}
