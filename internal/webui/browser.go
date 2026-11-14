// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package webui

// The Browser page's engine: Firefox in its own container (deploy/browser), shown through Neko's stream.
//
//	GET /phe/browser/ticket     (signed device) -> {configured, token, bridge}
//	/phe/browser/neko/...       the stream's API and socket; Neko checks the token
//	/phe/browser/bridge?t=...   the page's socket for tabs, navigation and downloads
//	/phe/browser/ext?s=...      the socket of the extension inside Firefox (shared secret)
//	POST /phe/browser/upload?id=  (signed device) files picked on the device for a page's file input -> handed to the extension
//	GET /phe/browser/file/{tok}?s= (the extension) one of those files, once, within two minutes
//	GET /phe/browser/settings   (admin) -> {configured, override, memory: the extension's last report (memoryMB, auto, budget, open, loaded)}
//	PUT /phe/browser/settings   (admin) {override}: how many tabs Firefox keeps loaded, 0 = from its memory
//	POST /phe/browser/ask?s=&tab=&kind=&url= (the extension, for a phone link's view) {msg, def}: a page's alert, confirm or prompt,
//	                            answered on the phone -> {ok, text}
//
// Neither Neko's port nor Firefox is reachable from outside: everything but the WebRTC media comes through here.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
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

	"github.com/phorepoc/cloud-companion/internal/browser"
)

type browserHub struct {
	ff    *browser.Firefox // the phone link's cloud browser, on the same Firefox (nil: not this server's engine)
	mu    sync.Mutex
	ext   *websocket.Conn
	pages map[*websocket.Conn]bool
	tabs  []byte // the last tab list, for a page that connects later
	mem   []byte // the extension's last memory report
	files map[string]*heldFile
	st    interface {
		Setting(string) string
		SetSetting(string, string) error
	}
}

const browserOverrideKey = "browser.loaded-tabs"

// heldFile is a file picked on a device for a page in Firefox, until the extension takes it.
type heldFile struct {
	name, typ string
	data      []byte
	until     time.Time
}

const browserUploadMax = 200 << 20

func (a *auth) browserRoutes(mux *http.ServeMux, ff *browser.Firefox) {
	base, pass, secret := os.Getenv("PHECLOUD_BROWSER_URL"), os.Getenv("PHECLOUD_BROWSER_PASSWORD"), os.Getenv("PHECLOUD_BROWSER_SECRET")
	on := base != "" && pass != "" && secret != ""
	h := &browserHub{ff: ff, pages: map[*websocket.Conn]bool{}, st: a.st, files: map[string]*heldFile{}}
	mux.HandleFunc("GET /phe/browser/settings", func(w http.ResponseWriter, r *http.Request) {
		if !a.adminDevice(r) {
			bad(w, 403, "admin only")
			return
		}
		h.mu.Lock()
		mem := h.mem
		h.mu.Unlock()
		var m any
		if mem != nil {
			_ = json.Unmarshal(mem, &m)
		}
		n, _ := strconv.Atoi(a.st.Setting(browserOverrideKey))
		reply(w, 200, map[string]any{"configured": on, "override": n, "memory": m})
	})
	mux.HandleFunc("PUT /phe/browser/settings", func(w http.ResponseWriter, r *http.Request) {
		if !a.adminDevice(r) {
			bad(w, 403, "admin only")
			return
		}
		var in struct{ Override int }
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in) != nil || in.Override < 0 || in.Override > 500 {
			bad(w, 400, "enter a number from 0 to 500")
			return
		}
		if err := a.st.SetSetting(browserOverrideKey, strconv.Itoa(in.Override)); err != nil {
			bad(w, 500, err.Error())
			return
		}
		h.toExt(r.Context(), in.Override)
		reply(w, 200, map[string]any{"override": in.Override})
	})
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
	mux.HandleFunc("POST /phe/browser/upload", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := a.signedDevice(r); !ok {
			bad(w, 403, "this device is not signed in")
			return
		}
		id := r.URL.Query().Get("id")
		r.Body = http.MaxBytesReader(w, r.Body, browserUploadMax)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			bad(w, 413, "the files are too big (200 MB at most)")
			return
		}
		defer r.MultipartForm.RemoveAll()
		type out struct {
			Name string `json:"name"`
			Type string `json:"type"`
			Tok  string `json:"tok"`
		}
		var list []out
		h.mu.Lock()
		for k, f := range h.files { // what was never taken
			if time.Now().After(f.until) {
				delete(h.files, k)
			}
		}
		h.mu.Unlock()
		for _, fh := range r.MultipartForm.File["file"] {
			f, err := fh.Open()
			if err != nil {
				bad(w, 400, err.Error())
				return
			}
			b, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				bad(w, 400, err.Error())
				return
			}
			var t [16]byte
			_, _ = rand.Read(t[:])
			tok := hex.EncodeToString(t[:])
			typ := fh.Header.Get("Content-Type")
			h.mu.Lock()
			h.files[tok] = &heldFile{name: fh.Filename, typ: typ, data: b, until: time.Now().Add(2 * time.Minute)}
			h.mu.Unlock()
			list = append(list, out{fh.Filename, typ, tok})
		}
		m, _ := json.Marshal(map[string]any{"c": "files", "id": id, "files": list})
		h.mu.Lock()
		e := h.ext
		h.mu.Unlock()
		if e == nil {
			bad(w, 502, "the browser is not connected")
			return
		}
		_ = e.Write(r.Context(), websocket.MessageText, m)
		h.toPages(r.Context(), []byte(`{"t":"picked","id":`+strconv.Quote(id)+`}`))
		reply(w, 200, map[string]any{"files": len(list)})
	})
	mux.HandleFunc("GET /phe/browser/file/{tok}", func(w http.ResponseWriter, r *http.Request) {
		if !hmac.Equal([]byte(r.URL.Query().Get("s")), []byte(secret)) {
			bad(w, 403, "wrong secret")
			return
		}
		h.mu.Lock()
		f := h.files[r.PathValue("tok")]
		delete(h.files, r.PathValue("tok"))
		h.mu.Unlock()
		if f == nil || time.Now().After(f.until) {
			bad(w, 404, "no such file")
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(f.data)
	})
	mux.HandleFunc("POST /phe/browser/ask", func(w http.ResponseWriter, r *http.Request) {
		if !hmac.Equal([]byte(r.URL.Query().Get("s")), []byte(secret)) {
			bad(w, 403, "wrong secret")
			return
		}
		var in struct{ Msg, Def string }
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in)
		tab, _ := strconv.Atoi(r.URL.Query().Get("tab"))
		ans := browser.Answer{}
		if h.ff != nil {
			ans = h.ff.Ask(tab, browser.Dialog{Kind: r.URL.Query().Get("kind"), Message: in.Msg, Default: in.Def, URL: r.URL.Query().Get("url")})
		}
		reply(w, 200, map[string]any{"ok": ans.OK, "text": ans.Text})
	})
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
	c.SetReadLimit(64 << 20) // a phone view's page capture
	h.mu.Lock()
	h.ext = c
	h.mu.Unlock()
	if h.ff != nil {
		n := h.ff.Attach(func(ctx context.Context, b []byte) error { return c.Write(ctx, websocket.MessageText, b) })
		defer h.ff.Detach(n)
	}
	n, _ := strconv.Atoi(h.st.Setting(browserOverrideKey))
	h.toExt(ctx, n)
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
		if h.ff != nil && h.ff.Handle(m) {
			continue // the phone link's, not the pages'
		}
		head := m[:min(len(m), 16)]
		if bytes.Contains(head, []byte(`"t":"mem"`)) {
			h.mu.Lock()
			h.mem = m
			h.mu.Unlock()
			continue // for Settings, not the pages
		}
		if bytes.Contains(head, []byte(`"t":"tabs"`)) {
			h.mu.Lock()
			h.tabs = m
			h.mu.Unlock()
		}
		h.toPages(ctx, m)
	}
}

// toExt tells the extension how many tabs to keep loaded (0: from its memory).
func (h *browserHub) toExt(ctx context.Context, n int) {
	h.mu.Lock()
	e := h.ext
	h.mu.Unlock()
	if e != nil {
		_ = e.Write(ctx, websocket.MessageText, []byte(`{"c":"budget","n":`+strconv.Itoa(n)+`}`))
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
