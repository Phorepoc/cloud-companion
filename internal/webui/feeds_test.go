// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/phorepoc/cloud-companion/internal/feeds"
	"github.com/phorepoc/cloud-companion/internal/store"
)

func TestFeedsAPI(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rss" {
			w.Write([]byte("<html>not a feed</html>"))
			return
		}
		w.Write([]byte(`<rss version="2.0"><channel><title>News</title><item><title>Hello</title><link>https://n.test/1</link><pubDate>Fri, 30 Oct 2026 10:00:00 GMT</pubDate></item></channel></rss>`))
	}))
	defer up.Close()
	st := store.OpenTest(t)
	mux := http.NewServeMux()
	(&feedsAPI{a: newAuth(st), st: st, f: feeds.New()}).routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mk := func(admin bool) *fakeDevice {
		id, key, err := st.NewDevice("d", admin)
		if err != nil {
			t.Fatal(err)
		}
		return &fakeDevice{id, key, srv}
	}
	owner, phone := mk(true), mk(false)
	if code, _ := (&fakeDevice{"nobody", []byte("x"), srv}).call(t, "GET", "/phe/feeds", nil); code != 403 {
		t.Fatalf("an unpaired caller got %d", code)
	}
	if code, j := phone.call(t, "GET", "/phe/feeds", nil); code != 200 || len(j["posts"].([]any)) != 0 {
		t.Fatalf("no feeds yet: %d %v", code, j)
	}
	add := map[string]any{"feeds": []map[string]any{{"url": up.URL + "/rss"}}}
	if code, _ := phone.call(t, "POST", "/phe/feeds", add); code != 403 {
		t.Fatal("only the owner changes the feeds")
	}
	if code, _ := owner.call(t, "POST", "/phe/feeds", map[string]any{"feeds": []map[string]any{{"url": up.URL + "/page"}}}); code != 422 {
		t.Fatal("an address that is not a feed is refused")
	}
	if code, _ := owner.call(t, "POST", "/phe/feeds", map[string]any{"feeds": []map[string]any{{"url": "file:///etc/passwd"}}}); code != 400 {
		t.Fatal("only http and https")
	}
	if code, j := owner.call(t, "POST", "/phe/feeds", add); code != 200 || j["feeds"].([]any)[0].(map[string]any)["title"] != "News" {
		t.Fatalf("adding a feed: %d %v", code, j)
	}
	code, j := phone.call(t, "GET", "/phe/feeds", nil)
	ps, _ := j["posts"].([]any)
	if code != 200 || len(ps) != 1 || ps[0].(map[string]any)["title"] != "Hello" || ps[0].(map[string]any)["feed"] != "News" {
		t.Fatalf("the posts: %d %v", code, j)
	}
}
