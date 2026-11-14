// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package remote

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/webdav"
)

// a WebDAV server (golang.org/x/net/webdav: the protocol as Go implements it) at /dav/files/me/, behind a login
func davServer(t *testing.T) (*httptest.Server, string) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "Photos", "Summer 2026"), 0o755)
	os.WriteFile(filepath.Join(dir, "Photos", "Summer 2026", "beach & sun.jpg"), []byte("JPEGDATA-0123456789"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644)
	h := &webdav.Handler{Prefix: "/dav/files/me", FileSystem: webdav.Dir(dir), LockSystem: webdav.NewMemLS()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "me" || p != "pw" {
			w.Header().Set("WWW-Authenticate", `Basic realm="dav"`)
			http.Error(w, "no", 401)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, dir
}

func TestWebDAV(t *testing.T) {
	srv, _ := davServer(t)
	ctx := context.Background()
	if _, err := NewWebDAV("ftp://x", "me", "pw", false); err == nil {
		t.Fatal("not an http address")
	}
	wrong, _ := NewWebDAV(srv.URL+"/dav/files/me", "me", "nope", false)
	if _, err := wrong.List(ctx, ""); !errors.Is(err, ErrAuth) {
		t.Fatalf("a wrong password: %v", err)
	}
	d, _ := NewWebDAV(srv.URL+"/dav/files/me", "me", "pw", false)
	top, err := d.List(ctx, "")
	if err != nil || len(top) != 2 || !top[0].Dir || top[0].Name != "Photos" || top[0].Path != "Photos" || top[1].Name != "notes.txt" || top[1].Size != 5 || top[1].Type != "text/plain" {
		t.Fatalf("the top: %+v %v", top, err)
	}
	in, err := d.List(ctx, "Photos/Summer 2026")
	if err != nil || len(in) != 1 || in[0].Name != "beach & sun.jpg" || in[0].Path != "Photos/Summer 2026/beach & sun.jpg" || in[0].Type != "image/jpeg" || in[0].Mod.IsZero() {
		t.Fatalf("a folder with spaces and &: %+v %v", in, err)
	}
	if _, err := d.List(ctx, "Nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a missing folder: %v", err)
	}
	if _, err := d.List(ctx, "../etc"); err == nil {
		t.Fatal("a path out of the top")
	}
	if e, err := d.Stat(ctx, "notes.txt"); err != nil || e.Dir || e.Size != 5 {
		t.Fatalf("stat: %+v %v", e, err)
	}

	// a player asking for part of a photo
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Range", "bytes=4-7")
	d.Serve(rec, r, "Photos/Summer 2026/beach & sun.jpg")
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != 206 || string(body) != "DATA" || rec.Header().Get("Content-Range") != "bytes 4-7/19" || rec.Header().Get("Content-Type") != "image/jpeg" ||
		!strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("a range: %d %q %v", rec.Code, body, rec.Header())
	}
	rec = httptest.NewRecorder()
	d.Serve(rec, httptest.NewRequest("GET", "/x", nil), "nope.txt")
	if rec.Code != 404 {
		t.Fatalf("a missing file: %d", rec.Code)
	}
}
