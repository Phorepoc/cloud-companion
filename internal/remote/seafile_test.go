// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package remote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// a Seafile server as its web API documents it (api2): a password or token login, two-step login for one user, libraries (two
// with the same name, one encrypted, one listed twice), a folder, and a download link that serves byte ranges
func seafileServer(t *testing.T) *httptest.Server {
	var srv *httptest.Server
	photo := strings.NewReader("JPEGDATA-0123456789")
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Token tok-1" && r.Header.Get("Authorization") != "Token api-token-2" {
			http.Error(w, `{"detail":"Invalid token"}`, 401)
			return false
		}
		return true
	}
	js := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("POST /api2/auth-token/", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch {
		case r.Form.Get("username") == "otp@example.org":
			w.Header().Set("X-Seafile-OTP", "required")
			http.Error(w, `{"non_field_errors":["Two factor auth token is missing."]}`, 400)
		case r.Form.Get("username") == "me@example.org" && r.Form.Get("password") == "pw":
			js(w, map[string]string{"token": "tok-1"})
		default:
			http.Error(w, `{"non_field_errors":["Unable to login with provided credentials."]}`, 400)
		}
	})
	mux.HandleFunc("GET /api2/auth/ping/", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			js(w, "pong")
		}
	})
	mux.HandleFunc("GET /api2/repos/", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		js(w, []map[string]any{
			{"id": "aaaaaa11-1", "name": "My Library", "size": 10, "mtime": 1700000000, "encrypted": false, "type": "repo"},
			{"id": "bbbbbb22-2", "name": "Shared", "size": 1, "mtime": 1700000000, "encrypted": false, "type": "srepo"},
			{"id": "cccccc33-3", "name": "Shared", "size": 1, "mtime": 1700000000, "encrypted": false, "type": "grepo"},
			{"id": "bbbbbb22-2", "name": "Shared", "size": 1, "mtime": 1700000000, "encrypted": false, "type": "grepo"},
			{"id": "dddddd44-4", "name": "Secret", "size": 1, "mtime": 1700000000, "encrypted": true, "type": "repo"},
		})
	})
	mux.HandleFunc("GET /api2/repos/{id}/dir/", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		switch r.PathValue("id") + r.URL.Query().Get("p") {
		case "aaaaaa11-1/":
			js(w, []map[string]any{{"type": "dir", "name": "Photos", "mtime": 1700000000}, {"type": "file", "name": "a.txt", "size": 3, "mtime": 1700000000}})
		case "aaaaaa11-1/Photos":
			js(w, []map[string]any{{"type": "file", "name": "beach & sun.jpg", "size": 19, "mtime": 1700000001}})
		default:
			http.Error(w, `{"error_msg":"Folder /x not found."}`, 404)
		}
	})
	mux.HandleFunc("GET /api2/repos/{id}/file/", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		if r.PathValue("id") != "aaaaaa11-1" || r.URL.Query().Get("p") != "/Photos/beach & sun.jpg" {
			http.Error(w, `{"error_msg":"File not found"}`, 404)
			return
		}
		js(w, srv.URL+"/seafhttp/files/one-time-token/beach%20%26%20sun.jpg")
	})
	mux.HandleFunc("GET /seafhttp/files/one-time-token/{name}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "x.jpg", time.Unix(1700000001, 0), photo) // no login: the link is the permission
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestSeafile(t *testing.T) {
	srv := seafileServer(t)
	ctx := context.Background()
	if _, err := NewSeafile(ctx, srv.URL, "me@example.org", "wrong", false); !errors.Is(err, ErrAuth) {
		t.Fatalf("a wrong password: %v", err)
	}
	if _, err := NewSeafile(ctx, srv.URL, "otp@example.org", "pw", false); !errors.Is(err, ErrAuth) || !strings.Contains(err.Error(), "API token") {
		t.Fatalf("two-step login: %v", err)
	}
	if _, err := NewSeafile(ctx, srv.URL, "anyone", "api-token-2", false); err != nil {
		t.Fatalf("an API token: %v", err)
	}
	sf, err := NewSeafile(ctx, srv.URL+"/", "me@example.org", "pw", false)
	if err != nil {
		t.Fatal(err)
	}
	top, err := sf.List(ctx, "")
	var names []string
	for _, e := range top {
		names = append(names, e.Name)
	}
	if err != nil || strings.Join(names, "|") != "My Library|Shared (bbbbbb)|Shared (cccccc)" || !top[0].Dir {
		t.Fatalf("the libraries (encrypted left out, a shared name told apart, one listed twice kept once): %q %v", names, err)
	}
	l, err := sf.List(ctx, "My Library")
	if err != nil || len(l) != 2 || l[0].Path != "My Library/Photos" || !l[0].Dir || l[1].Size != 3 {
		t.Fatalf("a library: %+v %v", l, err)
	}
	l, err = sf.List(ctx, "My Library/Photos")
	if err != nil || len(l) != 1 || l[0].Type != "image/jpeg" || l[0].Mod.Unix() != 1700000001 {
		t.Fatalf("a folder: %+v %v", l, err)
	}
	if _, err := sf.List(ctx, "Nope/x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no such library: %v", err)
	}
	if e, err := sf.Stat(ctx, "My Library/Photos/beach & sun.jpg"); err != nil || e.Size != 19 {
		t.Fatalf("stat: %+v %v", e, err)
	}
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Range", "bytes=4-7")
	sf.Serve(rec, r, "My Library/Photos/beach & sun.jpg")
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != 206 || string(body) != "DATA" || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("a range through the download link: %d %q", rec.Code, body)
	}
	rec = httptest.NewRecorder()
	sf.Serve(rec, httptest.NewRequest("GET", "/x", nil), "My Library/nope.jpg")
	if rec.Code != 404 {
		t.Fatalf("a missing file: %d", rec.Code)
	}
}
