// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The fake against the app's own IRC client (internal/irc): a password for no account is refused, the nick alone connects, and a
// second client with the same nick gets another one.
package irc

import (
	"context"
	"crypto/tls"
	"strings"
	"testing"

	app "github.com/phorepoc/cloud-companion/internal/irc"
)

func TestLogin(t *testing.T) {
	tc, err := TLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tc)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go New().Serve(ctx, ln)
	cfg := app.Cfg{Server: ln.Addr().String(), Nick: "cc", Insecure: true, Channels: []string{"#team"}}
	if _, err := app.Test(ctx, cfg, "definitely-wrong"); err == nil || !strings.Contains(err.Error(), "login refused") {
		t.Fatalf("wrong password: %v", err)
	}
	if msg, err := app.Test(ctx, cfg, ""); err != nil || msg != "connected as cc" {
		t.Fatalf("nick alone: %q %v", msg, err)
	}

	// a nick in use: the client is told and takes another
	held, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	held.Write([]byte("NICK cc\r\nUSER cc s e :x\r\n"))
	buf := make([]byte, 512)
	if n, _ := held.Read(buf); !strings.Contains(string(buf[:n]), " 001 cc ") {
		t.Fatalf("no welcome: %q", buf[:n])
	}
	if msg, err := app.Test(ctx, cfg, ""); err != nil || !strings.Contains(msg, "cc is taken") {
		t.Fatalf("taken nick: %q %v", msg, err)
	}
}
