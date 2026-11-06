// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package nextcloud is a stand-in Nextcloud for the browser suites (web/test/nctalk.sh, web/test/remotefiles.sh), on port 80 under
// the alias host "nextcloud". It answers what Cloud Companion sends (internal/nctalk, internal/remote) the way Nextcloud 35 does:
// the OCS user API and Talk's rooms and chat (basic auth, OCS-APIRequest header), WebDAV reads of the files (PROPFIND, GET) and the
// sign-in from another app (Login Flow v2) with its pages for the browser. Everything is in memory, seeded with what the suites'
// setup made in a real one: users alice, bob, carol, cc and ann (password pw-<name>-12345), ann's folder "CC Photos" with sea.png, next to the
// skeleton files every new account has.
package nextcloud

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/phorepoc/cloud-companion/internal/fakes"
)

func init() {
	fakes.Register(fakes.Fake{Name: "nextcloud", Run: func(ctx context.Context) error { return Serve(ctx, ":80") }})
}

// seaPNG is a 2x2 PNG, the photo remotefiles.sh put into ann's folder.
var seaPNG, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAFklEQVR4nGP8z8DAwMDAxMDAwMDAAAANHQEDasKb6QAAAABJRU5ErkJggg==")

// A file or folder of a user's (a folder has no data).
type node struct {
	dir  bool
	data []byte
	mod  time.Time
}

type room struct {
	Token       string    `json:"token"`
	Type        int       `json:"type"`
	Name        string    `json:"name"`
	DisplayName string    `json:"displayName"`
	members     []string  // user IDs
	msgs        []message // oldest first
}

type message struct {
	ID               int64           `json:"id"`
	Token            string          `json:"token"`
	ActorType        string          `json:"actorType"`
	ActorID          string          `json:"actorId"`
	ActorDisplayName string          `json:"actorDisplayName"`
	Timestamp        int64           `json:"timestamp"`
	Message          string          `json:"message"`
	Params           json.RawMessage `json:"messageParameters"`
	SystemMessage    string          `json:"systemMessage"`
	MessageType      string          `json:"messageType"`
}

// A login flow: open until the person grants access in the browser, then its app password is handed over once.
type flow struct {
	login, appPass string
}

// Server is one fake Nextcloud.
type Server struct {
	mu      sync.Mutex
	pass    map[string]string          // user → password
	apps    map[string]string          // app password → user
	files   map[string]map[string]node // user → path ("" is the top, "CC Photos/sea.png") → node
	rooms   map[string]*room
	flows   map[string]*flow  // poll token → flow
	byLogin map[string]string // login (flow page) token → poll token
	lastMsg int64
}

// New is a server with the suites' users and files.
func New() *Server {
	s := &Server{pass: map[string]string{}, apps: map[string]string{}, files: map[string]map[string]node{},
		rooms: map[string]*room{}, flows: map[string]*flow{}, byLogin: map[string]string{}}
	t := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, u := range []string{"alice", "bob", "carol", "cc", "ann"} {
		s.pass[u] = "pw-" + u + "-12345"
		// what a new Nextcloud account starts with (a few of its skeleton files)
		s.files[u] = map[string]node{"": {dir: true, mod: t}, "Documents": {dir: true, mod: t}, "Photos": {dir: true, mod: t},
			"Readme.md": {data: []byte("# Welcome to Nextcloud!\n"), mod: t}}
	}
	s.files["ann"]["CC Photos"] = node{dir: true, mod: t}
	s.files["ann"]["CC Photos/sea.png"] = node{data: seaPNG, mod: t}
	return s
}

// Serve runs a new server on addr until ctx ends.
func Serve(ctx context.Context, addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	hs := &http.Server{Handler: New().Handler()}
	go func() { <-ctx.Done(); hs.Close() }()
	if err := hs.Serve(l); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Handler routes like Nextcloud's: the paths do not depend on the Host.
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /status.php", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"installed": true, "maintenance": false, "version": "35.0.2.1", "productname": "Nextcloud"})
	})
	m.HandleFunc("/ocs/v2.php/", s.ocs)
	m.HandleFunc("/remote.php/dav/files/{user}/", s.dav)
	m.HandleFunc("POST /index.php/login/v2", s.flowStart)
	m.HandleFunc("POST /index.php/login/v2/poll", s.flowPoll)
	m.HandleFunc("/index.php/login/v2/flow/{token}", s.flowPage)
	return m
}

// user is who the request's basic auth signs in as (a password or an app password), "" when nobody.
func (s *Server) user(r *http.Request) string {
	u, p, ok := r.BasicAuth()
	if !ok {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if pw, ok := s.pass[u]; ok && pw == p {
		return u
	}
	if s.apps[p] == u && u != "" {
		return u
	}
	return ""
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
