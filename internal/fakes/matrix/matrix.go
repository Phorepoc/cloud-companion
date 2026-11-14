// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package matrix is a stand-in Matrix homeserver (as Conduit answers) for the browser suite web/test/matrix.sh: password login for the
// seeded user @cc:test.local (a wrong password is M_FORBIDDEN), logout, whoami, sync filters, an empty long-poll sync, and the device
// key upload and query that mautrix-go's encryption does on sign-in. Rooms and messages are not needed by the suite and answer
// M_UNRECOGNIZED.
package matrix

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/phorepoc/cloud-companion/internal/fakes"
)

const (
	// Addr is where Conduit listened in the suite (http://matrix:6167).
	Addr       = ":6167"
	ServerName = "test.local"
)

func init() {
	fakes.Register(fakes.Fake{Name: "matrix", Run: func(ctx context.Context) error {
		srv := &http.Server{Addr: Addr, Handler: New()}
		go func() { <-ctx.Done(); srv.Close() }()
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}})
}

type session struct{ user, device string }

// Server is the homeserver's in-memory state.
type Server struct {
	mu       sync.Mutex
	users    map[string]string                     // localpart → password
	tokens   map[string]session                    // access token → who
	keys     map[string]map[string]json.RawMessage // user → device → its signed device_keys
	otk      map[string]int                        // user|device → one-time keys uploaded
	SyncWait time.Duration                         // longest a sync waits (tests shorten it)
}

// New is a homeserver with the suite's account (cc / pw-cc-123).
func New() *Server {
	return &Server{
		users:    map[string]string{"cc": "pw-cc-123"},
		tokens:   map[string]session{},
		keys:     map[string]map[string]json.RawMessage{},
		otk:      map[string]int{},
		SyncWait: 30 * time.Second,
	}
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, errcode, msg string) {
	reply(w, code, map[string]string{"errcode": errcode, "error": msg})
}

func random(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// localpart takes a login identifier: "cc", "@cc" or "@cc:test.local".
func localpart(u string) string {
	u = strings.TrimPrefix(u, "@")
	if l, hs, ok := strings.Cut(u, ":"); ok && hs == ServerName {
		return l
	}
	return u
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case p == "/_matrix/client/versions":
		reply(w, 200, map[string]any{"versions": []string{"v1.1", "v1.2", "v1.3", "v1.4", "v1.5", "v1.6", "v1.7", "v1.8"}})
		return
	case p == "/_matrix/client/v3/login" && r.Method == http.MethodGet:
		reply(w, 200, map[string]any{"flows": []map[string]string{{"type": "m.login.password"}}})
		return
	case p == "/_matrix/client/v3/login" && r.Method == http.MethodPost:
		s.login(w, r)
		return
	}
	// the rest needs a signed-in device
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok == "" {
		tok = r.URL.Query().Get("access_token")
	}
	s.mu.Lock()
	who, ok := s.tokens[tok]
	s.mu.Unlock()
	if !ok {
		fail(w, 401, "M_UNKNOWN_TOKEN", "Unknown access token.")
		return
	}
	me := "@" + who.user + ":" + ServerName
	switch {
	case p == "/_matrix/client/v3/account/whoami":
		reply(w, 200, map[string]string{"user_id": me, "device_id": who.device})
	case p == "/_matrix/client/v3/logout" && r.Method == http.MethodPost:
		s.mu.Lock()
		delete(s.tokens, tok)
		s.mu.Unlock()
		reply(w, 200, map[string]any{})
	case strings.HasPrefix(p, "/_matrix/client/v3/user/") && strings.HasSuffix(p, "/filter") && r.Method == http.MethodPost:
		reply(w, 200, map[string]string{"filter_id": "0"})
	case p == "/_matrix/client/v3/keys/upload":
		s.upload(w, r, who)
	case p == "/_matrix/client/v3/keys/query":
		s.query(w, r)
	case p == "/_matrix/client/v3/sync":
		s.sync(w, r)
	default:
		log.Printf("fake matrix: %s %s not handled", r.Method, p)
		fail(w, 404, "M_UNRECOGNIZED", "Unrecognized request")
	}
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type       string `json:"type"`
		Identifier struct {
			Type, User string
		} `json:"identifier"`
		User     string `json:"user"`
		Password string `json:"password"`
		DeviceID string `json:"device_id"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Type != "m.login.password" {
		fail(w, 400, "M_BAD_JSON", "Bad login request.")
		return
	}
	u := req.Identifier.User
	if u == "" {
		u = req.User
	}
	u = localpart(u)
	s.mu.Lock()
	defer s.mu.Unlock()
	if pw, ok := s.users[u]; !ok || pw != req.Password {
		fail(w, 403, "M_FORBIDDEN", "Wrong username or password.")
		return
	}
	dev := req.DeviceID
	if dev == "" {
		dev = strings.ToUpper(random(5))
	}
	tok := random(16)
	s.tokens[tok] = session{u, dev}
	reply(w, 200, map[string]string{"user_id": "@" + u + ":" + ServerName, "access_token": tok, "device_id": dev, "home_server": ServerName})
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request, who session) {
	var req struct {
		DeviceKeys  json.RawMessage            `json:"device_keys"`
		OneTimeKeys map[string]json.RawMessage `json:"one_time_keys"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		fail(w, 400, "M_BAD_JSON", "Bad key upload.")
		return
	}
	me := "@" + who.user + ":" + ServerName
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(req.DeviceKeys) > 0 && string(req.DeviceKeys) != "null" {
		if s.keys[me] == nil {
			s.keys[me] = map[string]json.RawMessage{}
		}
		s.keys[me][who.device] = req.DeviceKeys
	}
	k := me + "|" + who.device
	s.otk[k] += len(req.OneTimeKeys)
	reply(w, 200, map[string]any{"one_time_key_counts": map[string]int{"signed_curve25519": s.otk[k]}})
}

func (s *Server) query(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceKeys map[string][]string `json:"device_keys"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		fail(w, 400, "M_BAD_JSON", "Bad key query.")
		return
	}
	out := map[string]map[string]json.RawMessage{}
	s.mu.Lock()
	for u, devs := range req.DeviceKeys {
		out[u] = map[string]json.RawMessage{}
		for d, k := range s.keys[u] {
			if len(devs) == 0 || contains(devs, d) {
				out[u][d] = k
			}
		}
	}
	s.mu.Unlock()
	reply(w, 200, map[string]any{"device_keys": out, "failures": map[string]any{}})
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// sync has nothing new to tell: a first sync answers at once, later ones wait out their timeout (a long poll).
func (s *Server) sync(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("since") != "" {
		wait, _ := strconv.Atoi(q.Get("timeout"))
		d := min(time.Duration(wait)*time.Millisecond, s.SyncWait)
		select {
		case <-r.Context().Done():
			return
		case <-time.After(d):
		}
	}
	reply(w, 200, map[string]any{
		"next_batch":                 "s1",
		"rooms":                      map[string]any{"join": map[string]any{}, "invite": map[string]any{}, "leave": map[string]any{}},
		"account_data":               map[string]any{"events": []any{}},
		"presence":                   map[string]any{"events": []any{}},
		"to_device":                  map[string]any{"events": []any{}},
		"device_lists":               map[string]any{"changed": []any{}, "left": []any{}},
		"device_one_time_keys_count": map[string]int{"signed_curve25519": 50},
	})
}
