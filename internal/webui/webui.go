// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package webui embeds the built web app (web/ -> internal/webui/dist) and serves it with the WebSocket endpoint.
package webui

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/coder/websocket"

	"github.com/phorepoc/phecloud/internal/proto"
	"github.com/phorepoc/phecloud/internal/server"
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
func Handler(s *server.Server) http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(distFS, "dist")
	files := http.FileServer(http.FS(sub))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "sw.js") {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	}))
	newAuth(s.Store).routes(mux)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		c.SetReadLimit(1 << 20)
		s.ServeConn(&wsIO{c: c, ctx: r.Context()}, r.RemoteAddr)
	})
	return mux
}
