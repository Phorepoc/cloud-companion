// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package webui embeds the built web app (web/ -> internal/webui/dist) and serves it with the WebSocket endpoint.
package webui

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/coder/websocket"

	"github.com/phorepoc/cloud-companion/internal/catalogue"
	"github.com/phorepoc/cloud-companion/internal/connectors"
	"github.com/phorepoc/cloud-companion/internal/feeds"
	"github.com/phorepoc/cloud-companion/internal/proto"
	"github.com/phorepoc/cloud-companion/internal/server"
	"github.com/phorepoc/cloud-companion/internal/weather"
)

//go:embed all:dist
var distFS embed.FS

type wsIO struct {
	c   *websocket.Conn
	ctx context.Context
}

func (w *wsIO) Read() (string, []byte, error) {
	_, b, err := w.c.Read(w.ctx)
	if err != nil {
		return "", nil, err
	}
	t, err := proto.TypeOf(b)
	return t, b, err
}

func (w *wsIO) Write(v any) error {
	b, err := proto.Marshal(v)
	if err != nil {
		return err
	}
	return w.c.Write(w.ctx, websocket.MessageBinary, b)
}

func (w *wsIO) Close() error { return w.c.CloseNow() }

// Handler serves the web app at / and the frame WebSocket at /ws.
func Handler(s *server.Server, m *connectors.Manager, cat *catalogue.Manager) http.Handler {
	trust, err := ParseTrust(os.Getenv("PHECLOUD_TRUSTED_PROXIES"))
	if err != nil {
		slog.Error("PHECLOUD_TRUSTED_PROXIES ignored (nobody is trusted)", "err", err)
		trust = &Trust{}
	}
	mux := http.NewServeMux()
	sub, _ := fs.Sub(distFS, "dist")
	files := http.FileServer(http.FS(sub))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "sw.js") {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	}))
	a := newAuth(s.Store)
	a.routes(mux)
	a.androidRoutes(mux)
	a.pairRoutes(mux)
	(&smsAPI{a: a, st: s.Store, hub: s.Hub}).routes(mux)
	if cat != nil {
		(&catAPI{a: a, st: s.Store, m: cat}).routes(mux)
	}
	var vault *connectors.Vault
	if m != nil {
		vault = m.V
	}
	(&weatherAPI{a: a, st: s.Store, v: vault, w: weather.New()}).routes(mux)
	(&feedsAPI{a: a, st: s.Store, f: feeds.New()}).routes(mux)
	filesDir := os.Getenv("PHECLOUD_FILES_DIR")
	if filesDir == "" {
		filesDir = filepath.Join(envOr("PHECLOUD_TLSDIR", "/data"), "files")
	}
	newFilesAPI(a, s.Store, filesDir).routes(mux)
	if m != nil {
		(&conns{a: a, st: s.Store, hub: s.Hub, m: m}).routes(mux)
		(&oauthAPI{a: a, m: m, st: map[string]*oauthState{}}).routes(mux)
	}
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		in := infoOf(r)
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{in.Host}})
		if err != nil {
			return
		}
		c.SetReadLimit(1 << 20)
		s.ServeConn(&wsIO{c: c, ctx: r.Context()}, in.ClientIP)
	})
	return securityHeaders(trust.withInfo(mux))
}

// securityHeaders adds the headers every response of the web app should carry: no content sniffing, no framing, no referrer leaks, a content policy that only allows what the app
// itself loads (its own scripts and styles, images and sockets from its own origin).
func securityHeaders(h http.Handler) http.Handler {
	const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; " +
		"connect-src 'self' ws: wss:; media-src 'self' blob:; worker-src 'self' blob:; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("Content-Security-Policy", csp)
		hd.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		h.ServeHTTP(w, r)
	})
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
