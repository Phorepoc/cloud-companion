// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package signal fakes signal-cli-rest-api (https://github.com/bbernhard/signal-cli-rest-api) as its README and Swagger describe
// it: /v1/about, /v1/accounts, /v1/qrcodelink, /v1/groups/{number}, /v2/send, and /v1/receive/{number} as a websocket (json-rpc
// mode) or a list (normal mode). The browser suite (web/test/signal.sh) runs it on signal-cli:8080 with nothing linked, as a fresh
// real API is; the Go tests in internal/signal link a number and push envelopes.
package signal

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/phorepoc/cloud-companion/internal/fakes"
	"rsc.io/qr"
)

func init() {
	fakes.Register(fakes.Fake{Name: "signal", Run: func(ctx context.Context) error {
		return Serve(ctx, ":8080", New("json-rpc"))
	}})
}

// Unregistered is a number Signal does not know: sending to it fails as the real API does.
const Unregistered = "+306999999999"

// API is the fake's state, all in memory.
type API struct {
	Mode string // json-rpc or normal

	mu       sync.Mutex
	accounts []string          // linked numbers
	groups   map[string]string // group JSON lists by number
	queue    []string          // envelopes waiting to be received (normal mode)
	sent     []map[string]any  // /v2/send bodies
	push     chan string       // envelopes for the websocket (json-rpc mode)
}

// New is an API with nothing linked.
func New(mode string) *API {
	return &API{Mode: mode, groups: map[string]string{}, push: make(chan string, 16)}
}

// Link adds a linked number with its groups (a JSON list as /v1/groups gives it, or "").
func (m *API) Link(number, groups string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accounts = append(m.accounts, number)
	m.groups[number] = groups
}

// Envelope hands one received envelope (JSON) to the app.
func (m *API) Envelope(s string) {
	if m.Mode == "json-rpc" {
		m.push <- s
		return
	}
	m.mu.Lock()
	m.queue = append(m.queue, s)
	m.mu.Unlock()
}

// Sent lists the /v2/send bodies so far.
func (m *API) Sent() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.sent)
}

func (m *API) linked(number string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Contains(m.accounts, number)
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (m *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case p == "/v1/about":
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"versions":["v1","v2"],"build":2,"mode":"` + m.Mode + `","version":"0.101"}`))
	case p == "/v1/accounts":
		m.mu.Lock()
		b, _ := json.Marshal(append([]string{}, m.accounts...))
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	case p == "/v1/qrcodelink":
		// the real one encodes a sgnl://linkdevice URI with fresh keys; any QR code will do here
		c, err := qr.Encode("sgnl://linkdevice?uuid=fake&pub_key=fake&device_name="+r.URL.Query().Get("device_name"), qr.M)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		c.Scale = 8
		w.Header().Set("Content-Type", "image/png")
		w.Write(c.PNG())
	case strings.HasPrefix(p, "/v1/groups/"):
		n := strings.TrimPrefix(p, "/v1/groups/")
		if !m.linked(n) {
			fail(w, 400, "User "+n+" is not registered.")
			return
		}
		m.mu.Lock()
		g := m.groups[n]
		m.mu.Unlock()
		if g == "" {
			g = "[]"
		}
		w.Write([]byte(g))
	case strings.HasPrefix(p, "/v1/receive/"):
		if n := strings.TrimPrefix(p, "/v1/receive/"); !m.linked(n) {
			fail(w, 400, "User "+n+" is not registered.")
			return
		}
		if m.Mode == "json-rpc" {
			m.websocket(w, r)
			return
		}
		m.mu.Lock()
		q := m.queue
		m.queue = nil
		m.mu.Unlock()
		w.Write([]byte("[" + strings.Join(q, ",") + "]"))
	case p == "/v2/send" && r.Method == "POST":
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		if n, _ := in["number"].(string); !m.linked(n) {
			fail(w, 400, "User "+n+" is not registered.")
			return
		}
		if rc, _ := in["recipients"].([]any); len(rc) == 1 && rc[0] == Unregistered {
			fail(w, 400, `Failed to send message: Unregistered user "`+Unregistered+`"`)
			return
		}
		m.mu.Lock()
		m.sent = append(m.sent, in)
		m.mu.Unlock()
		w.WriteHeader(201)
		w.Write([]byte(`{"timestamp":"1793910347000"}`))
	default:
		fail(w, 404, "not found")
	}
}

func (m *API) websocket(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	for {
		select {
		case s := <-m.push:
			if c.Write(r.Context(), websocket.MessageText, []byte(s)) != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

// Serve runs h on addr until ctx ends.
func Serve(ctx context.Context, addr string, h http.Handler) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() { <-ctx.Done(); srv.Close() }()
	if err := srv.Serve(l); err != http.ErrServerClosed {
		return err
	}
	return nil
}
