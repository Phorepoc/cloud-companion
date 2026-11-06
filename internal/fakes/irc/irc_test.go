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
	"time"

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
	// (the server frees the nick of the client before when it sees that one go: try again for a moment, as a person would)
	var held *tls.Conn
	for i := 0; ; i++ {
		c, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{InsecureSkipVerify: true})
		if err != nil {
			t.Fatal(err)
		}
		c.Write([]byte("NICK cc\r\nUSER cc s e :x\r\n"))
		buf := make([]byte, 512)
		n, _ := c.Read(buf)
		if strings.Contains(string(buf[:n]), " 001 cc ") {
			held = c
			break
		}
		c.Close()
		if i == 50 {
			t.Fatalf("no welcome: %q", buf[:n])
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer held.Close()
	if msg, err := app.Test(ctx, cfg, ""); err != nil || !strings.Contains(msg, "cc is taken") {
		t.Fatalf("taken nick: %q %v", msg, err)
	}
}
