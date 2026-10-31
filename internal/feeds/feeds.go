// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package feeds is the news behind the desktop's Feeds widget and the Feeds part of Events (after the Etesian phone's and
// Plasma's Feeds): the owner's RSS 2.0 and Atom feeds, read by the server (a browser cannot read most feeds itself) and merged
// newest first. Each feed is kept for 30 minutes; a feed that fails keeps its last posts and reports the error.
package feeds

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Feed is one of the owner's feeds: its address and the title it gives itself.
type Feed struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// Post is one item of a feed.
type Post struct {
	Feed      string    `json:"feed"` // the feed's title
	FeedURL   string    `json:"feedURL"`
	Title     string    `json:"title"`
	Link      string    `json:"link"`
	Published time.Time `json:"published"`
	Summary   string    `json:"summary,omitempty"`
}

// Result is the merged posts and the feeds that could not be read (address -> error).
type Result struct {
	Posts  []Post            `json:"posts"`
	Errors map[string]string `json:"errors,omitempty"`
}

const (
	maxBody  = 4 << 20 // a feed larger than this is cut off
	maxPosts = 30      // per feed
	keepFor  = 30 * time.Minute
)

// Service reads feeds and keeps them for a while.
type Service struct {
	HTTP *http.Client
	mu   sync.Mutex
	got  map[string]cached
}

type cached struct {
	at    time.Time
	title string
	posts []Post
}

// New returns a Service with a 20 second HTTP client.
func New() *Service { return &Service{HTTP: &http.Client{Timeout: 20 * time.Second}, got: map[string]cached{}} }

// Check reports whether u can be a feed address (http or https, with a host).
func Check(u string) error {
	p, err := url.Parse(strings.TrimSpace(u))
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
		return errors.New("a feed address starts with http:// or https://")
	}
	return nil
}

// Read fetches one feed (or returns it from the cache) and returns its title and posts.
func (s *Service) Read(ctx context.Context, u string) (string, []Post, error) {
	s.mu.Lock()
	c, ok := s.got[u]
	s.mu.Unlock()
	if ok && time.Since(c.at) < keepFor {
		return c.title, c.posts, nil
	}
	title, posts, err := s.fetch(ctx, u)
	if err != nil {
		if ok { // keep the last good posts
			return c.title, c.posts, err
		}
		return "", nil, err
	}
	s.mu.Lock()
	s.got[u] = cached{time.Now(), title, posts}
	s.mu.Unlock()
	return title, posts, nil
}

// All reads every feed at once and merges their posts, newest first.
func (s *Service) All(ctx context.Context, list []Feed) Result {
	type one struct {
		f     Feed
		title string
		posts []Post
		err   error
	}
	out := make(chan one, len(list))
	for _, f := range list {
		go func(f Feed) {
			t, p, err := s.Read(ctx, f.URL)
			out <- one{f, t, p, err}
		}(f)
	}
	res := Result{Posts: []Post{}}
	for range list {
		o := <-out
		if o.err != nil {
			if res.Errors == nil {
				res.Errors = map[string]string{}
			}
			res.Errors[o.f.URL] = o.err.Error()
		}
		name := o.f.Title
		if name == "" {
			name = o.title
		}
		for _, p := range o.posts {
			p.Feed = name
			res.Posts = append(res.Posts, p)
		}
	}
	sort.SliceStable(res.Posts, func(i, j int) bool { return res.Posts[i].Published.After(res.Posts[j].Published) })
	return res
}

// Forget drops the cache (after the list changes).
func (s *Service) Forget() {
	s.mu.Lock()
	s.got = map[string]cached{}
	s.mu.Unlock()
}

func (s *Service) fetch(ctx context.Context, u string) (string, []Post, error) {
	if err := Check(u); err != nil {
		return "", nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", "cloud-companion feeds")
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml")
	res, err := s.HTTP.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("the feed answered %s", res.Status)
	}
	return Parse(io.LimitReader(res.Body, maxBody), u)
}

// Parse reads an RSS 2.0 or Atom document; feedURL fills in each post's feed address and resolves relative links.
func Parse(r io.Reader, feedURL string) (string, []Post, error) {
	var doc struct {
		XMLName xml.Name
		// RSS 2.0
		Channel struct {
			Title string    `xml:"title"`
			Items []rssItem `xml:"item"`
		} `xml:"channel"`
		// Atom
		Title   string      `xml:"title"`
		Entries []atomEntry `xml:"entry"`
		// RSS 1.0 (RDF) puts items next to the channel
		Items []rssItem `xml:"item"`
	}
	d := xml.NewDecoder(r)
	d.Strict = false
	d.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil } // most feeds are UTF-8; others read as is
	if err := d.Decode(&doc); err != nil {
		return "", nil, errors.New("not a feed (RSS or Atom)")
	}
	base, _ := url.Parse(feedURL)
	abs := func(l string) string {
		l = strings.TrimSpace(l)
		if p, err := url.Parse(l); err == nil && base != nil {
			return base.ResolveReference(p).String()
		}
		return l
	}
	var title string
	var posts []Post
	switch strings.ToLower(doc.XMLName.Local) {
	case "rss", "rdf":
		title = clean(doc.Channel.Title)
		for _, it := range append(doc.Channel.Items, doc.Items...) {
			posts = append(posts, Post{Title: clean(it.Title), Link: abs(it.Link), Published: when(it.PubDate, it.Date), Summary: short(it.Description)})
		}
	case "feed":
		title = clean(doc.Title)
		for _, e := range doc.Entries {
			link := ""
			for _, l := range e.Links {
				if l.Rel == "" || l.Rel == "alternate" {
					link = l.Href
					break
				}
			}
			posts = append(posts, Post{Title: clean(e.Title), Link: abs(link), Published: when(e.Published, e.Updated), Summary: short(e.Summary + e.Content)})
		}
	default:
		return "", nil, errors.New("not a feed (RSS or Atom)")
	}
	if len(posts) > maxPosts {
		posts = posts[:maxPosts]
	}
	for i := range posts {
		posts[i].FeedURL = feedURL
		posts[i].Feed = title
	}
	return title, posts, nil
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	PubDate     string `xml:"pubDate"`
	Date        string `xml:"date"` // dc:date
	Description string `xml:"description"`
}

type atomEntry struct {
	Title string `xml:"title"`
	Links []struct {
		Href string `xml:"href,attr"`
		Rel  string `xml:"rel,attr"`
	} `xml:"link"`
	Published string `xml:"published"`
	Updated   string `xml:"updated"`
	Summary   string `xml:"summary"`
	Content   string `xml:"content"`
}

var layouts = []string{time.RFC1123Z, time.RFC1123, time.RFC3339, "Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST",
	"2 Jan 2006 15:04:05 -0700", "2006-01-02T15:04:05Z07:00", "2006-01-02"}

func when(vals ...string) time.Time {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		for _, l := range layouts {
			if t, err := time.Parse(l, v); err == nil {
				return t.UTC()
			}
		}
	}
	return time.Time{}
}

// clean collapses whitespace in a title.
func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

// short is the summary as plain text, at most 200 characters.
func short(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
			b.WriteRune(' ')
		case !in:
			b.WriteRune(r)
		}
	}
	t := clean(strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'", "&nbsp;", " ").Replace(b.String()))
	if r := []rune(t); len(r) > 200 {
		t = string(r[:199]) + "…"
	}
	return t
}
