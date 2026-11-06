// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phorepoc/cloud-companion/internal/store"
	"golang.org/x/net/webdav"
)

// The WebDAV server as a file manager uses it: a phone login, the owner's files (listed, read, written, moved, deleted) and a
// read-only folder for a WebDAV account next to them, with a range of its photo and a copy from it into the owner's files.
func TestDAVServer(t *testing.T) {
	far := t.TempDir()
	os.MkdirAll(filepath.Join(far, "Photos"), 0o755)
	os.WriteFile(filepath.Join(far, "Photos", "a.jpg"), []byte("JPEGDATA-0123456789"), 0o644)
	dav := &webdav.Handler{Prefix: "/dav", FileSystem: webdav.Dir(far), LockSystem: webdav.NewMemLS()}
	farSrv := httptest.NewServer(dav)
	defer farSrv.Close()

	st := store.OpenTest(t)
	st.AddAccount(store.Account{Kind: "webdav", Provider: "generic", Name: "My NAS", Config: `{"url":"` + farSrv.URL + `/dav/"}`})
	pass, err := st.AddSIPUser("n9")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(dir, ".phe-shares"), []byte("x"), 0o644)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644)
	os.Symlink(outside, filepath.Join(dir, "away"))
	mux := http.NewServeMux()
	fa := newFilesAPI(newAuth(st), st, dir)
	fa.secret = func(store.Account) (string, error) { return "", nil }
	fa.routes(mux)
	srv := httptest.NewServer(securityHeaders(mux))
	defer srv.Close()

	do := func(method, p, body string, hdr ...string) (int, string) {
		t.Helper()
		r, _ := http.NewRequest(method, srv.URL+p, strings.NewReader(body))
		r.SetBasicAuth("n9", pass)
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}

	r, _ := http.NewRequest("PROPFIND", srv.URL+"/dav/", nil)
	r.SetBasicAuth("n9", "wrong")
	if res, _ := http.DefaultClient.Do(r); res.StatusCode != 401 || !strings.Contains(res.Header.Get("WWW-Authenticate"), "Basic") {
		t.Fatalf("a wrong password: %d", res.StatusCode)
	}
	code, body := do("PROPFIND", "/dav/", "", "Depth", "1")
	if code != 207 || !strings.Contains(body, "notes.txt") || !strings.Contains(body, "My%20NAS") || strings.Contains(body, ".phe-") {
		t.Fatalf("the top: %d %s", code, body)
	}
	if code, body := do("PROPFIND", "/dav/My%20NAS/Photos/", "", "Depth", "1"); code != 207 || !strings.Contains(body, "a.jpg") || !strings.Contains(body, "image/jpeg") {
		t.Fatalf("a folder of the account: %d %s", code, body)
	}
	if code, body := do("GET", "/dav/My%20NAS/Photos/a.jpg", "", "Range", "bytes=4-7"); code != 206 || body != "DATA" {
		t.Fatalf("a range of its photo: %d %q", code, body)
	}
	if code, body := do("GET", "/dav/My%20NAS/Photos/a.jpg", ""); code != 200 || body != "JPEGDATA-0123456789" {
		t.Fatalf("its photo: %d %q", code, body)
	}
	for _, c := range []struct{ method, p string }{{"PUT", "/dav/My%20NAS/new.txt"}, {"DELETE", "/dav/My%20NAS/Photos/a.jpg"}, {"MKCOL", "/dav/My%20NAS/New"}} {
		if code, _ := do(c.method, c.p, "x"); code < 400 {
			t.Fatalf("%s %s on a read-only folder: %d", c.method, c.p, code)
		}
	}
	if _, err := os.Stat(filepath.Join(far, "Photos", "a.jpg")); err != nil {
		t.Fatal("the other service's photo was deleted")
	}
	if code, _ := do("MOVE", "/dav/notes.txt", "", "Destination", srv.URL+"/dav/My%20NAS/notes.txt"); code < 400 {
		t.Fatalf("moving into a read-only folder: %d", code)
	}

	if code, _ := do("PUT", "/dav/new.txt", "written over WebDAV"); code != 201 {
		t.Fatalf("PUT: %d", code)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "new.txt")); string(b) != "written over WebDAV" {
		t.Fatalf("on disk: %q", b)
	}
	if code, _ := do("MKCOL", "/dav/Docs", ""); code != 201 {
		t.Fatalf("MKCOL: %d", code)
	}
	if code, _ := do("MOVE", "/dav/new.txt", "", "Destination", srv.URL+"/dav/Docs/moved.txt"); code != 201 {
		t.Fatalf("MOVE: %d", code)
	}
	if code, _ := do("COPY", "/dav/My%20NAS/Photos/a.jpg", "", "Destination", srv.URL+"/dav/Docs/a.jpg"); code != 201 {
		t.Fatalf("a copy from the account: %d", code)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "Docs", "a.jpg")); string(b) != "JPEGDATA-0123456789" {
		t.Fatalf("the copy: %q", b)
	}
	if code, _ := do("DELETE", "/dav/Docs/moved.txt", ""); code != 204 {
		t.Fatalf("DELETE: %d", code)
	}
	for _, p := range []string{"/dav/away/secret.txt", "/dav/.phe-shares"} {
		if code, body := do("GET", p, ""); code < 400 || strings.Contains(body, "secret") {
			t.Fatalf("%s: %d %q", p, code, body)
		}
	}
	if code, _ := do("PUT", "/dav/away/planted.txt", "x"); code < 400 {
		t.Fatalf("writing through a link out of the folder: %d", code)
	}
	if _, err := os.Stat(filepath.Join(outside, "planted.txt")); err == nil {
		t.Fatal("a file was written outside the folder")
	}
}
