// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

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

// A WebDAV account's files through the Files API: a top folder named after the account, its folders listed with "~id/" paths,
// a photo streamed through a ticket with a byte range; uploads, folders, moves, deletes and share links refused as read-only.
func TestRemoteFolders(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "Photos"), 0o755)
	os.WriteFile(filepath.Join(dir, "Photos", "a.jpg"), []byte("JPEGDATA-0123456789"), 0o644)
	dav := &webdav.Handler{Prefix: "/dav", FileSystem: webdav.Dir(dir), LockSystem: webdav.NewMemLS()}
	davSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "me" || p != "pw" {
			http.Error(w, "no", 401)
			return
		}
		dav.ServeHTTP(w, r)
	}))
	defer davSrv.Close()

	st := store.OpenTest(t)
	a, _ := st.AddAccount(store.Account{Kind: "webdav", Provider: "generic", Name: "My NAS", Config: `{"url":"` + davSrv.URL + `/dav/","user":"me"}`})
	st.AddAccount(store.Account{Kind: "imap", Name: "Mail", Config: `{}`})
	mux := http.NewServeMux()
	fa := newFilesAPI(newAuth(st), st, t.TempDir())
	fa.secret = func(store.Account) (string, error) { return "pw", nil }
	fa.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	id, key, _ := st.NewDevice("d", false)
	dev := &fakeDevice{id, key, srv}

	code, j := dev.call(t, "GET", "/phe/files", nil)
	top := j["entries"].([]any)
	if code != 200 || len(top) != 1 {
		t.Fatalf("the top: %d %v", code, j)
	}
	m := top[0].(map[string]any)
	if m["name"] != "My NAS" || m["path"] != "~"+a.ID || m["source"] != "WebDAV" || m["readonly"] != true || m["dir"] != true {
		t.Fatalf("the account's folder: %v", m)
	}
	code, j = dev.call(t, "GET", "/phe/files?path=~"+a.ID+"/Photos", nil)
	if code != 200 || len(j["entries"].([]any)) != 1 {
		t.Fatalf("a folder in it: %d %v", code, j)
	}
	if e := j["entries"].([]any)[0].(map[string]any); e["path"] != "~"+a.ID+"/Photos/a.jpg" || e["type"] != "image/jpeg" || e["readonly"] != true {
		t.Fatalf("a photo in it: %v", e)
	}
	if code, _ := dev.call(t, "GET", "/phe/files?path=~"+a.ID+"/Nope", nil); code != 404 {
		t.Fatalf("a missing folder: %d", code)
	}
	if code, _ := dev.call(t, "GET", "/phe/files?path=~nosuchaccount", nil); code != 404 {
		t.Fatalf("no such account: %d", code)
	}

	_, j = dev.call(t, "POST", "/phe/files/ticket", map[string]any{"path": "~" + a.ID + "/Photos/a.jpg"})
	req, _ := http.NewRequest("GET", srv.URL+j["url"].(string), nil)
	req.Header.Set("Range", "bytes=4-7")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 206 || string(b) != "DATA" || res.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("streamed through a ticket: %d %q", res.StatusCode, b)
	}
	if code, _ := dev.call(t, "POST", "/phe/files/ticket", map[string]any{"path": "~" + a.ID + "/Photos"}); code == 200 {
		t.Fatal("a ticket for a folder")
	}

	for _, c := range []struct{ url, what string }{{"/phe/files/mkdir", `path`}, {"/phe/files/delete", `path`}, {"/phe/files/move", `from`}} {
		code, j := dev.call(t, "POST", c.url, map[string]any{c.what: "~" + a.ID + "/Photos/a.jpg", "to": "x.jpg"})
		if code != 422 || !strings.Contains(j["error"].(string), "read-only") {
			t.Fatalf("%s: %d %v", c.url, code, j)
		}
	}
	if code, _ := dev.call(t, "POST", "/phe/files/move", map[string]any{"from": "x.jpg", "to": "~" + a.ID + "/x.jpg"}); code != 422 {
		t.Fatal("moving into it")
	}
	if code, _ := dev.call(t, "POST", "/phe/files/share", map[string]any{"path": "~" + a.ID + "/Photos/a.jpg", "days": 1}); code != 422 {
		t.Fatal("a share link")
	}
	if code, _ := dev.call(t, "POST", "/phe/files/upload?path=~"+a.ID, nil); code != 422 {
		t.Fatalf("an upload: %d", code)
	}
}
