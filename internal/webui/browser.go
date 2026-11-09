// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

// The Browser page's engine: Firefox in its own container (deploy/browser), shown through Neko's stream.
//
//	GET /phe/browser/ticket     (signed device) -> {configured, token, bridge}
//	/phe/browser/neko/...       the stream's API and socket; Neko checks the token
//	/phe/browser/bridge?t=...   the page's socket for tabs, navigation and downloads
//	/phe/browser/ext?s=...      the socket of the extension inside Firefox (shared secret)
//
// Neither Neko's port nor Firefox is reachable from outside: everything but the WebRTC media comes through here.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type browserHub struct {
	mu    sync.Mutex
	ext   *websocket.Conn
	pages map[*websocket.Conn]bool
	tabs  []byte // the last tab list, for a page that connects later
}

func (a *auth) browserRoutes(mux *http.ServeMux) {
	base, pass, secret := os.Getenv("PHECLOUD_BROWSER_URL"), os.Getenv("PHECLOUD_BROWSER_PASSWORD"), os.Getenv("PHECLOUD_BROWSER_SECRET")
	on := base != "" && pass != "" && secret != ""
	h := &browserHub{pages: map[*websocket.Conn]bool{}}
	mux.HandleFunc("/phe/browser/ticket", func(w http.ResponseWriter, r *http.Request) {
		id, _, ok := a.signedDevice(r)
		if !ok {
			bad(w, 403, "this device is not signed in")
			return
		}
		if !on {
			reply(w, 200, map[string]any{"configured": false})
			return
		}
		tok, err := browserLogin(r.Context(), base, id, pass)
		if err != nil {
			bad(w, 502, "the browser is starting or not reachable")
			return
		}
		reply(w, 200, map[string]any{"configured": true, "token": tok, "bridge": browserTicket(secret, time.Now().Add(2*time.Minute))})
	})
	if !on {
		return
	}
	target, err := url.Parse(base)
	if err != nil {
		return
	}
	mux.Handle("/phe/browser/neko/", http.StripPrefix("/phe/browser/neko", httputil.NewSingleHostReverseProxy(target)))
	mux.HandleFunc("/phe/browser/bridge", func(w http.ResponseWriter, r *http.Request) {
		if !browserTicketOK(secret, r.URL.Query().Get("t"), time.Now()) {
			bad(w, 403, "no valid ticket")
			return
		}
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{infoOf(r).Host}})
		if err != nil {
			return
		}
		h.servePage(r.Context(), c)
	})
	mux.HandleFunc("/phe/browser/ext", func(w http.ResponseWriter, r *http.Request) {
		if !hmac.Equal([]byte(r.URL.Query().Get("s")), []byte(secret)) {
			bad(w, 403, "wrong secret")
			return
		}
		// the extension's origin is moz-extension://<random>: the secret is what counts
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		h.serveExt(r.Context(), c)
	})
}

func (h *browserHub) servePage(ctx context.Context, c *websocket.Conn) {
	defer c.CloseNow()
	h.mu.Lock()
	h.pages[c] = true
	tabs, up := h.tabs, h.ext != nil
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.pages, c); h.mu.Unlock() }()
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"t":"engine","up":`+strconv.FormatBool(up)+`}`))
	if tabs != nil {
		_ = c.Write(ctx, websocket.MessageText, tabs)
	}
	for {
		_, m, err := c.Read(ctx)
		if err != nil {
			return
		}
		h.mu.Lock()
		e := h.ext
		h.mu.Unlock()
		if e != nil {
			_ = e.Write(ctx, websocket.MessageText, m)
		}
	}
}

func (h *browserHub) serveExt(ctx context.Context, c *websocket.Conn) {
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	h.mu.Lock()
	h.ext = c
	h.mu.Unlock()
	h.toPages(ctx, []byte(`{"t":"engine","up":true}`))
	defer func() {
		h.mu.Lock()
		if h.ext == c {
			h.ext = nil
		}
		h.mu.Unlock()
		h.toPages(context.Background(), []byte(`{"t":"engine","up":false}`))
	}()
	for {
		_, m, err := c.Read(ctx)
		if err != nil {
			return
		}
		if bytes.Contains(m[:min(len(m), 16)], []byte(`"t":"tabs"`)) {
			h.mu.Lock()
			h.tabs = m
			h.mu.Unlock()
		}
		h.toPages(ctx, m)
	}
}

func (h *browserHub) toPages(ctx context.Context, m []byte) {
	h.mu.Lock()
	cs := make([]*websocket.Conn, 0, len(h.pages))
	for c := range h.pages {
		cs = append(cs, c)
	}
	h.mu.Unlock()
	for _, c := range cs {
		wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_ = c.Write(wctx, websocket.MessageText, m)
		cancel()
	}
}

// browserLogin gets the stream's token for a device: each device is its own Neko session.
func browserLogin(ctx context.Context, base, device, pass string) (string, error) {
	body, _ := json.Marshal(map[string]string{"username": "device-" + device, "password": pass})
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(base, "/")+"/api/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if err != nil {
		return "", err
	}
	if res.StatusCode != 200 {
		return "", &url.Error{Op: "login", URL: base, Err: io.ErrUnexpectedEOF}
	}
	var v struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", err
	}
	return v.Token, nil
}

func browserTicket(secret string, exp time.Time) string {
	e := strconv.FormatInt(exp.Unix(), 10)
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte("browser." + e))
	return e + "." + hex.EncodeToString(m.Sum(nil))
}

func browserTicketOK(secret, t string, now time.Time) bool {
	e, _, ok := strings.Cut(t, ".")
	if !ok {
		return false
	}
	n, err := strconv.ParseInt(e, 10, 64)
	if err != nil || now.Unix() > n {
		return false
	}
	return hmac.Equal([]byte(t), []byte(browserTicket(secret, time.Unix(n, 0))))
}
