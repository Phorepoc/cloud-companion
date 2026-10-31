// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

// The Feeds API (internal/feeds). Any paired device reads the posts; only the owner changes the list, so a device cannot make
// the server fetch addresses of its choosing.
//
//	GET  /phe/feeds          {feeds, posts, errors}: the owner's feeds and their posts merged, newest first
//	POST /phe/feeds          (owner) {feeds: [{url, title}]}: replaces the list; each new address is read first

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/phorepoc/cloud-companion/internal/feeds"
	"github.com/phorepoc/cloud-companion/internal/store"
)

type feedsAPI struct {
	a  *auth
	st *store.Store
	f  *feeds.Service
}

const feedsSetting = "feeds"

func (fa *feedsAPI) list() []feeds.Feed {
	out := []feeds.Feed{}
	if s := fa.st.Setting(feedsSetting); s != "" {
		json.Unmarshal([]byte(s), &out)
	}
	return out
}

func (fa *feedsAPI) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /phe/feeds", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := fa.a.signedDevice(r); !ok {
			bad(w, 403, "pair this device first")
			return
		}
		list := fa.list()
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		res := fa.f.All(ctx, list)
		reply(w, 200, map[string]any{"feeds": list, "posts": res.Posts, "errors": res.Errors})
	})
	mux.HandleFunc("POST /phe/feeds", func(w http.ResponseWriter, r *http.Request) {
		if !fa.a.adminDevice(r) {
			bad(w, 403, "sign in as the owner")
			return
		}
		var in struct {
			Feeds []feeds.Feed `json:"feeds"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		if len(in.Feeds) > 50 {
			bad(w, 400, "at most 50 feeds")
			return
		}
		old := map[string]bool{}
		for _, f := range fa.list() {
			old[f.URL] = true
		}
		seen := map[string]bool{}
		out := []feeds.Feed{}
		for _, f := range in.Feeds {
			f.URL, f.Title = strings.TrimSpace(f.URL), strings.TrimSpace(f.Title)
			if err := feeds.Check(f.URL); err != nil {
				bad(w, 400, err.Error())
				return
			}
			if seen[f.URL] {
				continue
			}
			seen[f.URL] = true
			if !old[f.URL] { // tried before it is kept
				ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
				title, _, err := fa.f.Read(ctx, f.URL)
				cancel()
				if err != nil {
					bad(w, 422, f.URL+": "+err.Error())
					return
				}
				if f.Title == "" {
					f.Title = title
				}
			}
			out = append(out, f)
		}
		b, _ := json.Marshal(out)
		if err := fa.st.SetSetting(feedsSetting, string(b)); err != nil {
			bad(w, 500, "could not save the feeds")
			return
		}
		reply(w, 200, map[string]any{"feeds": out})
	})
}
