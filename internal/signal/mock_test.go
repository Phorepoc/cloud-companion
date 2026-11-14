// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package signal

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phorepoc/cloud-companion/internal/chat"
	fake "github.com/phorepoc/cloud-companion/internal/fakes/signal"
	"github.com/phorepoc/cloud-companion/internal/store"
)

const me = "+306900000001"

// the API is the fake in internal/fakes/signal, with me linked in the group Team
func mockAPI(mode string) *fake.API {
	m := fake.New(mode)
	m.Link(me, `[{"name":"Team","id":"group.dGVhbQ==","internal_id":"dGVhbQ==","members":["`+me+`","+306900000002"]}]`)
	return m
}

func wasSent(m *fake.API, to, body string) bool {
	for _, s := range m.Sent() {
		if rc, _ := s["recipients"].([]any); len(rc) == 1 && rc[0] == to && s["message"] == body && s["number"] == me {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; !ok(); i++ {
		if i > 100 {
			t.Fatal("timed out: " + what)
		}
		time.Sleep(50 * time.Millisecond)
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

const (
	fromBob    = `{"envelope":{"source":"+306900000002","sourceNumber":"+306900000002","sourceUuid":"b0b","sourceName":"Bob","timestamp":1793910347000,"dataMessage":{"timestamp":1793910347000,"message":"hello"}},"account":"` + me + `"}`
	hidden     = `{"envelope":{"source":"5e3c-uuid","sourceNumber":null,"sourceUuid":"5e3c-uuid","sourceName":"Carol","timestamp":1793910348000,"dataMessage":{"timestamp":1793910348000,"message":"no number"}},"account":"` + me + `"}`
	inGroup    = `{"envelope":{"sourceNumber":"+306900000002","sourceName":"Bob","timestamp":1793910349000,"dataMessage":{"timestamp":1793910349000,"message":"team?","groupInfo":{"groupId":"dGVhbQ==","type":"DELIVER"}}},"account":"` + me + `"}`
	withFile   = `{"envelope":{"sourceNumber":"+306900000002","sourceName":"Bob","timestamp":1793910350000,"dataMessage":{"timestamp":1793910350000,"message":"look","attachments":[{"contentType":"image/jpeg","filename":"cat.jpg"}]}},"account":"` + me + `"}`
	receipt    = `{"envelope":{"sourceNumber":"+306900000002","timestamp":1793910351000,"receiptMessage":{"isDelivery":true,"timestamps":[1]}},"account":"` + me + `"}`
	syncedSelf = `{"envelope":{"sourceNumber":"` + me + `","timestamp":1793910352000,"syncMessage":{"sentMessage":{"destination":"+306900000002","message":"from my phone"}}},"account":"` + me + `"}`
	reaction   = `{"envelope":{"sourceNumber":"+306900000002","timestamp":1793910353000,"dataMessage":{"timestamp":1793910353000,"message":null,"reaction":{"emoji":"👍"}}},"account":"` + me + `"}`
)

func TestBothModes(t *testing.T) {
	for _, mode := range []string{"json-rpc", "normal"} {
		t.Run(mode, func(t *testing.T) {
			m := mockAPI(mode)
			srv := httptest.NewServer(m)
			defer srv.Close()
			if d, err := Test(context.Background(), Cfg{API: srv.URL, Number: me}); err != nil || !strings.Contains(d, mode) {
				t.Fatalf("%q %v", d, err)
			}
			if _, err := Test(context.Background(), Cfg{API: srv.URL, Number: "+306911111111"}); err == nil || !strings.Contains(err.Error(), "not linked yet") {
				t.Fatalf("a number that is not linked: %v", err)
			}
			st := store.OpenTest(t)
			st.AddSIPUser("me")
			hub := chat.NewHub(st)
			var mu sync.Mutex
			ok := 0
			x := &Conn{Acc: "a", Name: "sig", Cfg: Cfg{API: srv.URL, Number: me}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Every: 50 * time.Millisecond,
				Status: func(err error) {
					mu.Lock()
					defer mu.Unlock()
					if err == nil {
						ok++
					}
				}}
			hub.Add(x)
			if err := x.Start(); err != nil {
				t.Fatal(err)
			}
			defer x.Stop()
			waitFor(t, "receiving", func() bool { mu.Lock(); defer mu.Unlock(); return ok > 0 })
			for _, e := range []string{receipt, syncedSelf, reaction, fromBob, hidden, inGroup, withFile} {
				m.Envelope(e)
			}
			waitFor(t, "the messages", func() bool { return inbox(st, "+306900000002", "look [file: cat.jpg]") != nil })
			if g := inbox(st, "+306900000002", "hello"); g == nil || g.PeerName != "Bob" || g.TS != 1793910347 {
				t.Fatalf("bob's message: %+v", g)
			}
			if inbox(st, "5e3c-uuid", "no number") == nil {
				t.Fatal("a sender with a hidden number")
			}
			if g := inbox(st, "group.dGVhbQ==", "team?"); g == nil || g.PeerName != "Bob (Team)" {
				t.Fatalf("the group message: %+v", g)
			}
			if n := len(st.ChatFor("me")); n != 4 {
				t.Fatalf("%d messages: receipts, your own other devices and reactions must not be passed on", n)
			}

			for to, body := range map[string]string{"+306900000002": "hi bob", "group.dGVhbQ==": "hi team", "5e3c-uuid": "hi carol"} {
				if err := x.Send(context.Background(), "me", to, body, ""); err != nil || !wasSent(m, to, body) {
					t.Fatalf("to %s: %v", to, err)
				}
			}
			if err := x.Send(context.Background(), "me", fake.Unregistered, "anyone?", ""); !errors.Is(err, chat.ErrNoRoute) {
				t.Fatalf("a number not on Signal: %v", err)
			}
		})
	}
}

func TestCfg(t *testing.T) {
	c := Cfg{Number: "+30 690 000 0001"}
	if err := c.norm(); err != nil || c.API != "http://signal-cli:8080" || c.Number != "+306900000001" {
		t.Fatalf("%v %+v", err, c)
	}
	if err := (&Cfg{Number: "6900000001"}).norm(); err == nil {
		t.Fatal("a number without its country code")
	}
}
