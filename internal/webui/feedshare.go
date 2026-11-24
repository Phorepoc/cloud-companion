// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package webui

// Sharing the owner's feeds with feed readers (docs/feeds.md), the way catalogues are shared: off unless the owner turns it on,
// then public (anybody with the address) or private (the address holds a secret token, or the reader sends it as a bearer
// header). No signature, so any reader can use it: the Etesian phone's Feeds, FreshRSS, a desktop reader.
//
//	GET /feeds/index.json             {title, feeds, posts}: the feeds and their posts merged, newest first (Feeds on the phone)
//	GET /feeds/opml                   the list as OPML, to import into any reader
//	GET /feeds/atom.xml               the posts as one Atom feed
//	GET /feeds/{token}/...            the same, when private (the token in the path, for readers that cannot send headers)
//
// Owner API (admin devices, signed like the other /phe calls, see auth.go):
//
//	GET  /phe/feeds/share             {mode, addresses}
//	POST /phe/feeds/share             {mode?: "off" | "public" | "private", rotate_token?}

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"strings"
	"time"

	"github.com/phorepoc/cloud-companion/internal/feeds"
	"github.com/phorepoc/cloud-companion/internal/store"
)

const feedShareSetting = "feeds_share"

type feedShare struct {
	Mode  string `json:"mode"` // off, public or private
	Token string `json:"token,omitempty"`
}

func (fa *feedsAPI) share() feedShare {
	s := feedShare{Mode: "off"}
	if v := fa.st.Setting(feedShareSetting); v != "" {
		json.Unmarshal([]byte(v), &s)
	}
	if s.Mode != "public" && s.Mode != "private" {
		s.Mode = "off"
	}
	return s
}

// shareBase is where the shared feeds live, as a reader should see it.
func shareBase(r *http.Request, s feedShare) string {
	p := publicBase(r) + "/feeds"
	if s.Mode == "private" {
		p += "/" + s.Token
	}
	return p
}

func (fa *feedsAPI) describeShare(r *http.Request, s feedShare) map[string]any {
	d := map[string]any{"mode": s.Mode}
	if s.Mode != "off" {
		b := shareBase(r, s)
		d["addresses"] = map[string]string{"index": b + "/index.json", "opml": b + "/opml", "atom": b + "/atom.xml"}
	}
	return d
}

// shareAllowed decides whether a reader may read the shared feeds, as allowed() does for catalogues: nothing when sharing is off,
// anybody when public (a token segment is then not part of the address), the right token when private. Wrong tokens count as
// failures of the client address and look like a missing page.
func (fa *feedsAPI) shareAllowed(w http.ResponseWriter, r *http.Request) bool {
	s, tok := fa.share(), r.PathValue("token")
	switch s.Mode {
	case "public":
		if tok != "" {
			http.NotFound(w, r)
			return false
		}
		return true
	case "private":
		if fa.a.limited(r) {
			http.Error(w, "too many attempts", http.StatusTooManyRequests)
			return false
		}
		if tok == "" {
			tok = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if s.Token == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(s.Token)) != 1 {
			fa.a.failed(r)
			http.NotFound(w, r)
			return false
		}
		return true
	}
	http.NotFound(w, r)
	return false
}

func (fa *feedsAPI) posts(r *http.Request) feeds.Result {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	return fa.f.All(ctx, fa.list())
}

func (fa *feedsAPI) serveIndex(w http.ResponseWriter, r *http.Request) {
	if !fa.shareAllowed(w, r) {
		return
	}
	res := fa.posts(r)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{"title": "Cloud Companion feeds", "feeds": fa.list(), "posts": res.Posts, "errors": res.Errors})
}

type opmlOutline struct {
	Text   string `xml:"text,attr"`
	Title  string `xml:"title,attr"`
	Type   string `xml:"type,attr"`
	XMLURL string `xml:"xmlUrl,attr"`
}

func (fa *feedsAPI) serveOPML(w http.ResponseWriter, r *http.Request) {
	if !fa.shareAllowed(w, r) {
		return
	}
	var doc struct {
		XMLName xml.Name      `xml:"opml"`
		Version string        `xml:"version,attr"`
		Title   string        `xml:"head>title"`
		Body    []opmlOutline `xml:"body>outline"`
	}
	doc.Version, doc.Title = "2.0", "Cloud Companion feeds"
	for _, f := range fa.list() {
		doc.Body = append(doc.Body, opmlOutline{Text: f.Title, Title: f.Title, Type: "rss", XMLURL: f.URL})
	}
	w.Header().Set("Content-Type", "text/x-opml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(doc)
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
}

type atomText struct {
	Type string `xml:"type,attr,omitempty"`
	Body string `xml:",chardata"`
}

type atomEntryOut struct {
	ID      string    `xml:"id"`
	Title   string    `xml:"title"`
	Link    atomLink  `xml:"link"`
	Updated string    `xml:"updated"`
	Author  string    `xml:"author>name"`
	Summary *atomText `xml:"summary,omitempty"`
	Content *atomText `xml:"content,omitempty"`
}

func (fa *feedsAPI) serveAtom(w http.ResponseWriter, r *http.Request) {
	if !fa.shareAllowed(w, r) {
		return
	}
	res := fa.posts(r)
	var doc struct {
		XMLName xml.Name       `xml:"http://www.w3.org/2005/Atom feed"`
		ID      string         `xml:"id"`
		Title   string         `xml:"title"`
		Updated string         `xml:"updated"`
		Link    atomLink       `xml:"link"`
		Entries []atomEntryOut `xml:"entry"`
	}
	self := shareBase(r, fa.share()) + "/atom.xml"
	doc.ID, doc.Title, doc.Link = self, "Cloud Companion feeds", atomLink{Href: self, Rel: "self"}
	doc.Updated = time.Now().UTC().Format(time.RFC3339)
	if len(res.Posts) > 0 {
		doc.Updated = res.Posts[0].Published.UTC().Format(time.RFC3339)
	}
	for _, p := range res.Posts {
		e := atomEntryOut{ID: "urn:phecloud:post:" + p.ID, Title: p.Title, Link: atomLink{Href: p.Link}, Author: p.Feed,
			Updated: p.Published.UTC().Format(time.RFC3339)}
		if p.Summary != "" {
			e.Summary = &atomText{Body: p.Summary}
		}
		if p.HTML != "" {
			e.Content = &atomText{Type: "html", Body: p.HTML}
		}
		doc.Entries = append(doc.Entries, e)
	}
	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(doc)
}

func (fa *feedsAPI) shareRoutes(mux *http.ServeMux) {
	for _, p := range []string{"/feeds", "/feeds/{token}"} {
		mux.HandleFunc("GET "+p+"/index.json", fa.serveIndex)
		mux.HandleFunc("GET "+p+"/opml", fa.serveOPML)
		mux.HandleFunc("GET "+p+"/atom.xml", fa.serveAtom)
	}
	mux.HandleFunc("GET /phe/feeds/share", func(w http.ResponseWriter, r *http.Request) {
		if !fa.a.adminDevice(r) {
			bad(w, 403, "sign in as the owner")
			return
		}
		reply(w, 200, fa.describeShare(r, fa.share()))
	})
	mux.HandleFunc("POST /phe/feeds/share", func(w http.ResponseWriter, r *http.Request) {
		if !fa.a.adminDevice(r) {
			bad(w, 403, "sign in as the owner")
			return
		}
		var in struct {
			Mode        *string `json:"mode"`
			RotateToken bool    `json:"rotate_token"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		s := fa.share()
		if in.Mode != nil {
			switch *in.Mode {
			case "off", "public", "private":
				s.Mode = *in.Mode
			default:
				bad(w, 400, "mode is off, public or private")
				return
			}
		}
		if in.RotateToken || (s.Mode == "private" && s.Token == "") {
			s.Token = store.NewCatToken() // the old address stops working at once
		}
		b, _ := json.Marshal(s)
		if err := fa.st.SetSetting(feedShareSetting, string(b)); err != nil {
			bad(w, 500, "could not save the setting")
			return
		}
		reply(w, 200, fa.describeShare(r, s))
	})
}
