// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package webui

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/phorepoc/cloud-companion/internal/feeds"
	"github.com/phorepoc/cloud-companion/internal/store"
)

func TestFeedsShare(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<rss version="2.0"><channel><title>News</title><item><title>Hello</title><link>https://n.test/1</link><pubDate>Fri, 30 Oct 2026 10:00:00 GMT</pubDate><description>&lt;p&gt;Body&lt;/p&gt;</description></item></channel></rss>`))
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
	if code, _ := owner.call(t, "POST", "/phe/feeds", map[string]any{"feeds": []map[string]any{{"url": up.URL + "/rss"}}}); code != 200 {
		t.Fatal("adding a feed")
	}
	get := func(path, bearer string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}

	// Off until the owner turns it on.
	if code, j := owner.call(t, "GET", "/phe/feeds/share", nil); code != 200 || j["mode"] != "off" {
		t.Fatalf("off by default: %d %v", code, j)
	}
	if code, _ := get("/feeds/index.json", ""); code != 404 {
		t.Fatalf("nothing is served while off: %d", code)
	}
	if code, _ := phone.call(t, "POST", "/phe/feeds/share", map[string]any{"mode": "public"}); code != 403 {
		t.Fatal("only the owner shares the feeds")
	}
	if code, _ := owner.call(t, "POST", "/phe/feeds/share", map[string]any{"mode": "everyone"}); code != 400 {
		t.Fatal("an unknown mode is refused")
	}

	// Public: anybody, at the plain address; never with a token segment.
	code, j := owner.call(t, "POST", "/phe/feeds/share", map[string]any{"mode": "public"})
	if code != 200 || j["mode"] != "public" || !strings.HasSuffix(j["addresses"].(map[string]any)["index"].(string), "/feeds/index.json") {
		t.Fatalf("public: %d %v", code, j)
	}
	code, body := get("/feeds/index.json", "")
	var idx struct {
		Feeds []feeds.Feed `json:"feeds"`
		Posts []feeds.Post `json:"posts"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &idx) != nil || len(idx.Feeds) != 1 || len(idx.Posts) != 1 || idx.Posts[0].Title != "Hello" {
		t.Fatalf("public index: %d %s", code, body)
	}
	if code, _ := get("/feeds/abc/index.json", ""); code != 404 {
		t.Fatal("a public share has no token address")
	}
	code, body = get("/feeds/opml", "")
	var opml struct {
		Outlines []struct {
			URL   string `xml:"xmlUrl,attr"`
			Title string `xml:"title,attr"`
		} `xml:"body>outline"`
	}
	if code != 200 || xml.Unmarshal([]byte(body), &opml) != nil || len(opml.Outlines) != 1 || opml.Outlines[0].URL != up.URL+"/rss" || opml.Outlines[0].Title != "News" {
		t.Fatalf("opml: %d %s", code, body)
	}
	code, body = get("/feeds/atom.xml", "")
	var atom struct {
		Entries []struct {
			Title string `xml:"title"`
			Link  struct {
				Href string `xml:"href,attr"`
			} `xml:"link"`
		} `xml:"entry"`
	}
	if code != 200 || xml.Unmarshal([]byte(body), &atom) != nil || len(atom.Entries) != 1 || atom.Entries[0].Link.Href != "https://n.test/1" {
		t.Fatalf("atom: %d %s", code, body)
	}

	// Private: the token in the address or as a bearer header; the plain address and wrong tokens find nothing.
	code, j = owner.call(t, "POST", "/phe/feeds/share", map[string]any{"mode": "private"})
	index := j["addresses"].(map[string]any)["index"].(string)
	tok := strings.TrimSuffix(strings.TrimPrefix(index, srv.URL+"/feeds/"), "/index.json")
	if code != 200 || j["mode"] != "private" || len(tok) != 32 {
		t.Fatalf("private: %d %v", code, j)
	}
	if code, _ := get("/feeds/index.json", ""); code != 404 {
		t.Fatal("a private share is not at the plain address")
	}
	if code, _ := get("/feeds/"+tok+"/index.json", ""); code != 200 {
		t.Fatal("the token in the address")
	}
	if code, _ := get("/feeds/index.json", tok); code != 200 {
		t.Fatal("the token as a bearer header")
	}
	if code, _ := get("/feeds/"+strings.Repeat("0", 32)+"/opml", ""); code != 404 {
		t.Fatal("a wrong token finds nothing")
	}

	// A new token: the old address stops working at once.
	_, j = owner.call(t, "POST", "/phe/feeds/share", map[string]any{"rotate_token": true})
	if strings.Contains(j["addresses"].(map[string]any)["index"].(string), tok) {
		t.Fatal("the token changed")
	}
	if code, _ := get("/feeds/"+tok+"/index.json", ""); code != 404 {
		t.Fatal("the old token stopped working")
	}

	// Off again: nothing.
	owner.call(t, "POST", "/phe/feeds/share", map[string]any{"mode": "off"})
	if code, _ := get("/feeds/opml", ""); code != 404 {
		t.Fatal("off again")
	}
}
