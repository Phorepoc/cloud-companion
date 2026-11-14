// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build asterisk

// Against the real Asterisk of the compose profile (deploy/asterisk/test.sh runs it): Cloud Companion registers as
// "cloud-companion", extension 201 plays a phone, and messages go both ways through Asterisk's own routing.

package sipsvc

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
)

func TestAsterisk(t *testing.T) {
	server := os.Getenv("ASTERISK")
	cfg := func(user, listen string) Cfg { return Cfg{Server: server, User: user, Listen: listen} }
	if _, err := Test(context.Background(), cfg("cloud-companion", ""), "wrong"); err == nil {
		t.Fatal("Asterisk took a wrong password")
	}
	if d, err := Test(context.Background(), cfg("cloud-companion", ""), os.Getenv("CC_SECRET")); err != nil {
		t.Fatal(err)
	} else {
		t.Log(d)
	}

	st := store.OpenTest(t)
	hub := chat.NewHub(st)
	st.AddSIPUser("n9")
	cc := &Service{Acc: "pbx", Name: "Asterisk", Cfg: cfg("cloud-companion", "0.0.0.0:5070"), Pass: os.Getenv("CC_SECRET"), Hub: hub, Log: quiet}
	if err := cc.Start(); err != nil {
		t.Fatal(err)
	}
	defer cc.Stop()
	hub.Add(cc)
	st.SetChatRoutes([]store.ChatRoute{{Kind: "prefix", Value: "pbx.", Conn: cc.ID()}})

	pst := store.OpenTest(t) // the phone's side
	phub := chat.NewHub(pst)
	pst.AddSIPUser("me")
	phone := &Service{Acc: "201", Name: "Phone 201", Cfg: cfg("201", "0.0.0.0:5080"), Pass: os.Getenv("P201"), Hub: phub, Log: quiet}
	if err := phone.Start(); err != nil {
		t.Fatal(err)
	}
	defer phone.Stop()

	// phone → Cloud Companion
	for i := 0; ; i++ {
		err := phone.Send(context.Background(), "", "cloud-companion", "hello from 201", "")
		if err == nil {
			break
		}
		if i > 40 {
			t.Fatalf("201 → cloud-companion: %v", err)
		}
		time.Sleep(250 * time.Millisecond) // the registrations may not be in yet

	}
	waitFor(t, "the message from 201 in the inbox", func() bool { return len(st.ChatFor("n9")) == 1 })
	m := st.ChatFor("n9")[0]
	if m.Peer != "201" || m.Body != "hello from 201" || hub.Address(m.Conn, m.Peer) != "pbx.201" {
		t.Fatalf("inbox: %+v (reply address %q)", m, hub.Address(m.Conn, m.Peer))
	}

	// Cloud Companion → phone, the way a device answers
	if err := hub.Send(context.Background(), "n9", "pbx.201", "hi 201, from the N9"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the answer on the phone", func() bool { return len(pst.ChatFor("me")) == 1 })
	if pm := pst.ChatFor("me")[0]; pm.Peer != "cloud-companion" || pm.Body != "hi 201, from the N9" || pm.PeerName != "n9" {
		t.Fatalf("on the phone: %+v", pm)
	}
}
