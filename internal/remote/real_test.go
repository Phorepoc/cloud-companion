// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build remote

// Against real servers (testdata/remote/test.sh runs Nextcloud and Seafile next to it).

package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func serveRange(t *testing.T, s Source, p string) {
	t.Helper()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Range", "bytes=4-7")
	s.Serve(rec, r, p)
	if b, _ := io.ReadAll(rec.Body); rec.Code != 206 || string(b) != "DATA" || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("a range of %s: %d %q %v", p, rec.Code, b, rec.Header())
	}
}

func do(t *testing.T, r *http.Request, want int) []byte {
	t.Helper()
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != want {
		t.Fatalf("%s %s: %s %s", r.Method, r.URL, res.Status, b)
	}
	return b
}

// Nextcloud: ann signs in with her email address; her files are found under her user ID.
func TestRealNextcloud(t *testing.T) {
	nc := os.Getenv("NEXTCLOUD")
	ctx := context.Background()
	for _, x := range []struct{ method, p, body string }{{"MKCOL", "CC Test", ""}, {"PUT", "CC Test/beach & sun.jpg", "JPEGDATA-0123456789"}, {"PUT", "notes.txt", "hello"}} {
		r, _ := http.NewRequest(x.method, nc+"/remote.php/dav/files/ann/"+(&url.URL{Path: x.p}).EscapedPath(), strings.NewReader(x.body))
		r.SetBasicAuth("ann", "pw-ann-12345")
		do(t, r, 201)
	}
	if _, err := Nextcloud(ctx, nc, "ann@example.org", "wrong", false); !errors.Is(err, ErrAuth) {
		t.Fatalf("a wrong password: %v", err)
	}
	d, err := Nextcloud(ctx, nc, "ann@example.org", "pw-ann-12345", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(d.Base.Path, "/remote.php/dav/files/ann/") {
		t.Fatalf("her files are at %s", d.Base)
	}
	top, err := d.List(ctx, "")
	var names []string
	for _, e := range top {
		names = append(names, e.Name)
	}
	if err != nil || !strings.Contains(strings.Join(names, "|"), "CC Test") || !strings.Contains(strings.Join(names, "|"), "notes.txt") {
		t.Fatalf("the top: %q %v", names, err)
	}
	l, err := d.List(ctx, "CC Test")
	if err != nil || len(l) != 1 || l[0].Name != "beach & sun.jpg" || l[0].Size != 19 {
		t.Fatalf("CC Test: %+v %v", l, err)
	}
	serveRange(t, d, "CC Test/beach & sun.jpg")
	if d, err := Test(ctx, "webdav", `{"url":"`+nc+`","user":"ann@example.org","nextcloud":true}`, "pw-ann-12345"); err != nil || !strings.HasPrefix(d, "signed in:") {
		t.Fatalf("Test: %q %v", d, err)
	}
	t.Log("signed in with an email address; folders listed; a photo streamed with a range")

	l2, err := StartNCLogin(ctx, nc, false)
	if err != nil || !strings.Contains(l2.Login, "/login/v2/flow/") {
		t.Fatalf("the sign-in: %+v %v", l2, err)
	}
	if res, err := l2.Poll(ctx); res != nil || err != nil {
		t.Fatalf("not approved yet: %+v %v", res, err)
	}
	t.Log("Nextcloud's sign-in from another app starts, and waits for approval")
}

// Seafile: a library made and a photo uploaded through Seafile's own API, then read through ours.
func TestRealSeafile(t *testing.T) {
	sfURL := os.Getenv("SEAFILE")
	ctx := context.Background()
	if _, err := NewSeafile(ctx, sfURL, "me@example.org", "wrong", false); !errors.Is(err, ErrAuth) {
		t.Fatalf("a wrong password: %v", err)
	}
	sf, err := NewSeafile(ctx, sfURL, "me@example.org", "sf-pw-12345", false)
	if err != nil {
		t.Fatal(err)
	}
	auth := func(r *http.Request) *http.Request { r.Header.Set("Authorization", "Token "+sf.token); return r }
	r, _ := http.NewRequest("POST", sfURL+"/api2/repos/", strings.NewReader("name=Holidays"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var repo struct {
		ID string `json:"repo_id"`
	}
	json.Unmarshal(do(t, auth(r), 200), &repo)
	r, _ = http.NewRequest("GET", sfURL+"/api2/repos/"+repo.ID+"/upload-link/", nil)
	var link string
	json.Unmarshal(do(t, auth(r), 200), &link)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "beach & sun.jpg")
	fw.Write([]byte("JPEGDATA-0123456789"))
	mw.WriteField("parent_dir", "/")
	mw.Close()
	r, _ = http.NewRequest("POST", link+"?ret-json=1", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	do(t, auth(r), 200)

	top, err := sf.List(ctx, "")
	var names []string
	for _, e := range top {
		names = append(names, e.Name)
	}
	if err != nil || !strings.Contains(strings.Join(names, "|"), "Holidays") {
		t.Fatalf("the libraries: %q %v", names, err)
	}
	l, err := sf.List(ctx, "Holidays")
	if err != nil || len(l) != 1 || l[0].Name != "beach & sun.jpg" || l[0].Size != 19 {
		t.Fatalf("a library: %+v %v", l, err)
	}
	serveRange(t, sf, "Holidays/beach & sun.jpg")
	if d, err := Test(ctx, "seafile", `{"url":"`+sfURL+`","user":"me@example.org"}`, "sf-pw-12345"); err != nil || !strings.HasPrefix(d, "signed in:") {
		t.Fatalf("Test: %q %v", d, err)
	}
	t.Log("signed in; libraries and a folder listed; a photo streamed with a range through Seafile's download link")
}
