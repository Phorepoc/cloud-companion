// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package feeds

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const rss = `<?xml version="1.0"?><rss version="2.0"><channel><title> Tech  News </title>
<item><title>Older</title><link>/a</link><pubDate>Mon, 26 Oct 2026 10:00:00 +0000</pubDate><description>&lt;p&gt;Hello &amp;amp; bye&lt;/p&gt;</description></item>
<item><title>Newer</title><link>https://x.test/b</link><pubDate>Fri, 30 Oct 2026 10:00:00 GMT</pubDate></item>
</channel></rss>`

const atom = `<?xml version="1.0" encoding="utf-8"?><feed xmlns="http://www.w3.org/2005/Atom"><title>Blog</title>
<entry><title>Middle</title><link rel="alternate" href="https://blog.test/m"/><updated>2026-10-28T09:00:00Z</updated><summary>Sum</summary></entry>
</feed>`

func TestParse(t *testing.T) {
	title, posts, err := Parse(strings.NewReader(rss), "https://news.test/feed.xml")
	if err != nil || title != "Tech News" || len(posts) != 2 {
		t.Fatalf("rss: %q %v %v", title, posts, err)
	}
	if posts[0].Link != "https://news.test/a" || posts[0].Summary != "Hello & bye" || posts[0].Published.Day() != 26 {
		t.Errorf("rss post: %+v", posts[0])
	}
	title, posts, err = Parse(strings.NewReader(atom), "https://blog.test/atom")
	if err != nil || title != "Blog" || len(posts) != 1 || posts[0].Link != "https://blog.test/m" || posts[0].Published.Day() != 28 {
		t.Fatalf("atom: %q %+v %v", title, posts, err)
	}
	if _, _, err := Parse(strings.NewReader("<html><body>no</body></html>"), "https://x.test"); err == nil {
		t.Error("html should not parse as a feed")
	}
}

func TestAll(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Path {
		case "/rss":
			w.Write([]byte(rss))
		case "/atom":
			w.Write([]byte(atom))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	s := New()
	list := []Feed{{URL: srv.URL + "/rss"}, {URL: srv.URL + "/atom", Title: "My blog"}, {URL: srv.URL + "/gone"}}
	res := s.All(context.Background(), list)
	if len(res.Posts) != 3 || res.Posts[0].Title != "Newer" || res.Posts[1].Feed != "My blog" || res.Posts[2].Title != "Older" {
		t.Fatalf("merged newest first: %+v", res.Posts)
	}
	if res.Errors[srv.URL+"/gone"] == "" || len(res.Errors) != 1 {
		t.Errorf("errors: %v", res.Errors)
	}
	before := hits
	s.All(context.Background(), list[:2])
	if hits != before {
		t.Error("a feed read in the last 30 minutes should come from the cache")
	}
	if Check("ftp://x") == nil || Check("https://x.test/f") != nil {
		t.Error("Check")
	}
}
