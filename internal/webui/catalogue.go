// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

// Self-hosted catalogues (docs/catalogue.md).
//
// Served to Catalogue clients (no signature; a private catalogue needs its token, in the path or as a bearer header):
//
//	GET /catalogue/{id}/index.json                       a public catalogue's index.json
//	GET /catalogue/{id}/{token}/index.json               a private one's (the token in the path, for clients that cannot send headers)
//	GET /catalogue/{id}/files/{sha256}/{name}            an installation file or picture the catalogue serves itself (with the token segment too when private)
//
// Owner API (admin devices, requests signed like the Connections calls, see auth.go):
//
//	GET  /phe/catalogues                                 the catalogues with their addresses
//	POST /phe/catalogues                                 {id, name, public, priority, sync_interval}
//	POST /phe/catalogues/update                          {id, name?, public?, priority?, sync_interval?, rotate_token?}
//	POST /phe/catalogues/remove                          {id}
//	POST /phe/catalogue/upload                           multipart: catalogue, app_id, version, name, file (+ summary, description, category, license, homepage,
//	                                                     platform, package, arch, boards, min_core, legacy, icon, screenshot...)
//	POST /phe/catalogue/release/remove                   {catalogue, app_id, version}
//	GET  /phe/catalogue/apps?catalogue=                  the merged apps with the winning source and the sources that lost, and the uploaded releases
//	GET  /phe/catalogue/sources?catalogue=               the federated sources
//	POST /phe/catalogue/sources                          {catalogue, name, url, token, priority, mirror}
//	POST /phe/catalogue/sources/update                   {id, name?, url?, token?, priority?, enabled?, mirror?}
//	POST /phe/catalogue/sources/remove                   {id}
//	POST /phe/catalogue/sources/sync                     {id} or {catalogue}: sync now
//	POST /phe/catalogue/sources/mirror                   {source, app_id, mirror: -1 (follow the source) | 0 | 1}

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/phorepoc/cloud-companion/internal/catalogue"
	"github.com/phorepoc/cloud-companion/internal/store"
)

type catAPI struct {
	a  *auth
	st *store.Store
	m  *catalogue.Manager
}

func (c *catAPI) admin(w http.ResponseWriter, r *http.Request) bool {
	if !c.a.adminDevice(r) {
		bad(w, 403, "sign in as the owner")
		return false
	}
	return true
}

func publicBase(r *http.Request) string {
	if v := strings.TrimRight(os.Getenv("PHECLOUD_PUBLIC_URL"), "/"); v != "" {
		return v
	}
	in := infoOf(r)
	return in.Scheme + "://" + in.Host
}

// prefix is the address a catalogue's files and index live under, as the client should see it.
func prefix(base string, c store.Catalogue) string {
	p := base + "/catalogue/" + c.ID
	if !c.Public {
		p += "/" + c.Token
	}
	return p
}

// allowed decides whether a request may read a catalogue: public ones by anybody (a token segment is then not part of the address), private ones with the right
// token, in the path or as `Authorization: Bearer`. Wrong tokens count as failures of the client address (8 a minute) and look like a missing catalogue.
func (c *catAPI) allowed(w http.ResponseWriter, r *http.Request) (store.Catalogue, bool) {
	cat, ok := c.st.Catalogue(r.PathValue("id"))
	tok := r.PathValue("token")
	if !ok {
		http.NotFound(w, r)
		return cat, false
	}
	if cat.Public {
		if tok != "" {
			http.NotFound(w, r)
			return cat, false
		}
		return cat, true
	}
	if c.a.limited(r) {
		http.Error(w, "too many attempts", http.StatusTooManyRequests)
		return cat, false
	}
	if tok == "" {
		tok = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	if cat.Token == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(cat.Token)) != 1 {
		c.a.failed(r)
		http.NotFound(w, r)
		return cat, false
	}
	return cat, true
}

func (c *catAPI) serveIndex(w http.ResponseWriter, r *http.Request) {
	cat, ok := c.allowed(w, r)
	if !ok {
		return
	}
	idx, _ := c.m.Index(cat, prefix(publicBase(r), cat))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(idx)
}

func (c *catAPI) serveFile(w http.ResponseWriter, r *http.Request) {
	cat, ok := c.allowed(w, r)
	if !ok {
		return
	}
	sha := r.PathValue("sha")
	// only what the catalogue's current index offers: another catalogue's file is not reachable by guessing its hash
	if _, files := c.m.Index(cat, prefix(publicBase(r), cat)); files[sha] == "" {
		http.NotFound(w, r)
		return
	}
	f, fi, err := c.m.OpenBlob(sha) // the content is checked against its SHA-256 before it is sent
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	name := catalogue.SafeName(r.PathValue("name"))
	ct := mime.TypeByExtension("." + catalogue.PackageOf(name))
	if ct == "" || strings.HasPrefix(ct, "text/") {
		ct = "application/octet-stream"
	}
	if strings.HasPrefix(ct, "image/") && strings.Contains(ct, "svg") {
		ct = "application/octet-stream" // never let the browser run a hosted SVG as part of this origin
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("X-Content-SHA256", sha)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

func (c *catAPI) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /catalogue/{id}/index.json", c.serveIndex)
	mux.HandleFunc("GET /catalogue/{id}/{token}/index.json", c.serveIndex)
	mux.HandleFunc("GET /catalogue/{id}/files/{sha}/{name}", c.serveFile)
	mux.HandleFunc("GET /catalogue/{id}/{token}/files/{sha}/{name}", c.serveFile)

	mux.HandleFunc("/phe/catalogues", func(w http.ResponseWriter, r *http.Request) {
		if !c.admin(w, r) {
			return
		}
		base := publicBase(r)
		if r.Method == http.MethodGet {
			out := []map[string]any{}
			for _, cat := range c.st.Catalogues() {
				out = append(out, c.describe(cat, base))
			}
			reply(w, 200, map[string]any{"catalogues": out})
			return
		}
		var in struct {
			ID, Name     string
			Public       bool
			Priority     *int
			SyncInterval *int `json:"sync_interval"`
		}
		if r.Method != http.MethodPost || !readJSON(w, r, &in) {
			return
		}
		cat := store.Catalogue{ID: in.ID, Name: in.Name, Public: in.Public, Priority: 100, SyncInterval: 6 * 3600}
		if in.Priority != nil {
			cat.Priority = *in.Priority
		}
		if in.SyncInterval != nil {
			cat.SyncInterval = *in.SyncInterval
		}
		cat, err := c.st.CatalogueAdd(cat)
		if err != nil {
			bad(w, 400, err.Error())
			return
		}
		reply(w, 200, map[string]any{"catalogue": c.describe(cat, base)})
	})
	mux.HandleFunc("/phe/catalogues/update", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID, Name     string
			Public       *bool
			Priority     *int
			SyncInterval *int `json:"sync_interval"`
			RotateToken  bool `json:"rotate_token"`
		}
		if !c.admin(w, r) || !readJSON(w, r, &in) {
			return
		}
		cat, ok := c.st.Catalogue(in.ID)
		if !ok {
			bad(w, 404, "unknown catalogue")
			return
		}
		if in.Name != "" {
			cat.Name = in.Name
		}
		if in.Public != nil {
			cat.Public = *in.Public
		}
		if in.Priority != nil {
			cat.Priority = *in.Priority
		}
		if in.SyncInterval != nil {
			if *in.SyncInterval < 0 {
				bad(w, 400, "the interval is seconds, 0 for manual only")
				return
			}
			cat.SyncInterval = *in.SyncInterval
		}
		if in.RotateToken || (!cat.Public && cat.Token == "") {
			cat.Token = store.NewCatToken() // the old address stops working at once
		}
		if err := c.st.CatalogueUpdate(cat); err != nil {
			bad(w, 500, "could not save")
			return
		}
		reply(w, 200, map[string]any{"catalogue": c.describe(cat, publicBase(r))})
	})
	mux.HandleFunc("/phe/catalogues/remove", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ ID string }
		if !c.admin(w, r) || !readJSON(w, r, &in) {
			return
		}
		if err := c.st.CatalogueRemove(in.ID); err != nil {
			bad(w, 500, "could not remove")
			return
		}
		reply(w, 200, map[string]any{"ok": true})
	})
	c.libraryRoutes(mux)
	mux.HandleFunc("/phe/catalogue/apps", func(w http.ResponseWriter, r *http.Request) {
		if !c.admin(w, r) {
			return
		}
		cat, ok := c.st.Catalogue(r.URL.Query().Get("catalogue"))
		if !ok {
			bad(w, 404, "unknown catalogue")
			return
		}
		_, rep, _ := c.m.Build(cat, prefix(publicBase(r), cat))
		rels := []catalogue.Release{}
		for _, rs := range c.m.Hosted(cat.ID) {
			rels = append(rels, rs...)
		}
		if rep == nil {
			rep = []catalogue.AppReport{}
		}
		reply(w, 200, map[string]any{"apps": rep, "releases": rels})
	})
	mux.HandleFunc("/phe/catalogue/sources", func(w http.ResponseWriter, r *http.Request) {
		if !c.admin(w, r) {
			return
		}
		if r.Method == http.MethodGet {
			reply(w, 200, map[string]any{"sources": c.sources(r.URL.Query().Get("catalogue"))})
			return
		}
		var in struct {
			Catalogue, Name, URL, Token string
			Priority                    *int
			Mirror                      bool
		}
		if r.Method != http.MethodPost || !readJSON(w, r, &in) {
			return
		}
		if _, ok := c.st.Catalogue(in.Catalogue); !ok {
			bad(w, 404, "unknown catalogue")
			return
		}
		if err := catalogue.CheckSourceURL(in.URL); err != nil {
			bad(w, 400, err.Error())
			return
		}
		src := store.CatSource{Catalogue: in.Catalogue, Name: strings.TrimSpace(in.Name), URL: in.URL, Token: in.Token, Priority: 50, Mirror: in.Mirror}
		if in.Priority != nil {
			src.Priority = *in.Priority
		}
		if src.Name == "" {
			if u, err := url.Parse(in.URL); err == nil {
				src.Name = u.Host
			}
		}
		src, err := c.st.CatSourceAdd(src)
		if err != nil {
			bad(w, 500, "could not save")
			return
		}
		reply(w, 200, map[string]any{"source": c.sourceView(src)})
	})
	mux.HandleFunc("/phe/catalogue/sources/update", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID, Name, URL string
			Token         *string
			Priority      *int
			Enabled       *bool
			Mirror        *bool
		}
		if !c.admin(w, r) || !readJSON(w, r, &in) {
			return
		}
		src, ok := c.st.CatSource(in.ID)
		if !ok {
			bad(w, 404, "unknown source")
			return
		}
		if in.Name != "" {
			src.Name = in.Name
		}
		if in.URL != "" {
			if err := catalogue.CheckSourceURL(in.URL); err != nil {
				bad(w, 400, err.Error())
				return
			}
			src.URL = in.URL
		}
		if in.Token != nil {
			src.Token = *in.Token
		}
		if in.Priority != nil {
			src.Priority = *in.Priority
		}
		if in.Enabled != nil {
			src.Enabled = *in.Enabled
		}
		if in.Mirror != nil {
			src.Mirror = *in.Mirror
		}
		if err := c.st.CatSourceUpdate(src); err != nil {
			bad(w, 500, "could not save")
			return
		}
		reply(w, 200, map[string]any{"source": c.sourceView(src)})
	})
	mux.HandleFunc("/phe/catalogue/sources/remove", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ ID string }
		if !c.admin(w, r) || !readJSON(w, r, &in) {
			return
		}
		src, ok := c.st.CatSource(in.ID)
		if !ok {
			bad(w, 404, "unknown source")
			return
		}
		var shas []string
		for sha := range c.st.CatMirrors(src.ID) {
			shas = append(shas, sha)
		}
		c.st.CatSourceRemove(in.ID)
		for _, s := range shas {
			c.m.Prune(s)
		}
		reply(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("/phe/catalogue/sources/sync", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ ID, Catalogue string }
		if !c.admin(w, r) || !readJSON(w, r, &in) {
			return
		}
		var srcs []store.CatSource
		if in.ID != "" {
			if s, ok := c.st.CatSource(in.ID); ok {
				srcs = append(srcs, s)
			}
		} else {
			srcs = c.st.CatSources(in.Catalogue)
		}
		if len(srcs) == 0 {
			bad(w, 404, "no such source")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
		defer cancel()
		res := []map[string]any{}
		for _, s := range srcs {
			if !s.Enabled {
				continue
			}
			sum, err := c.m.SyncSource(ctx, s.ID)
			e := map[string]any{"id": s.ID, "name": s.Name, "ok": err == nil, "summary": sum}
			if err != nil {
				e["detail"] = err.Error()
			}
			res = append(res, e)
		}
		reply(w, 200, map[string]any{"results": res})
	})
	mux.HandleFunc("/phe/catalogue/sources/mirror", func(w http.ResponseWriter, r *http.Request) {
		var raw struct {
			Source string `json:"source"`
			AppID  string `json:"app_id"`
			Mirror int    `json:"mirror"`
		}
		if !c.admin(w, r) || !readJSON(w, r, &raw) {
			return
		}
		if raw.Mirror < -1 || raw.Mirror > 1 {
			bad(w, 400, "mirror is -1 (follow the source), 0 or 1")
			return
		}
		if ok, _ := c.st.CatSourceItemMirror(raw.Source, raw.AppID, raw.Mirror); !ok {
			bad(w, 404, "that source does not list this app")
			return
		}
		reply(w, 200, map[string]any{"ok": true})
	})
}

func (c *catAPI) describe(cat store.Catalogue, base string) map[string]any {
	idx := prefix(base, cat) + "/index.json"
	apps := map[string]bool{}
	for id := range c.m.Hosted(cat.ID) {
		apps[id] = true
	}
	d := map[string]any{"id": cat.ID, "name": cat.Name, "public": cat.Public, "priority": cat.Priority, "sync_interval": cat.SyncInterval,
		"created": cat.Created, "index_url": idx, "hosted_apps": len(apps), "sources": len(c.st.CatSources(cat.ID))}
	if !cat.Public {
		d["token"] = cat.Token
	}
	return d
}

func (c *catAPI) sourceView(s store.CatSource) map[string]any {
	items := c.st.CatSourceItems(s.ID)
	mirrored := 0
	for _, m := range c.st.CatMirrors(s.ID) {
		if m.State == "ok" {
			mirrored++
		}
	}
	return map[string]any{"id": s.ID, "catalogue": s.Catalogue, "name": s.Name, "url": s.URL, "has_token": s.Token != "", "priority": s.Priority,
		"enabled": s.Enabled, "mirror": s.Mirror, "last_sync": s.LastSync, "last_error": s.LastError, "items": len(items), "mirrored_files": mirrored,
		"mirror_problems": c.problems(s.ID)}
}

func (c *catAPI) problems(source string) []store.CatMirror {
	out := []store.CatMirror{}
	for _, m := range c.st.CatMirrors(source) {
		if m.State != "ok" {
			out = append(out, m)
		}
	}
	return out
}

func (c *catAPI) sources(catalogueID string) []map[string]any {
	out := []map[string]any{}
	for _, s := range c.st.CatSources(catalogueID) {
		v := c.sourceView(s)
		per := []map[string]any{}
		for _, it := range c.st.CatSourceItems(s.ID) {
			per = append(per, map[string]any{"app_id": it.AppID, "mirror": it.Mirror})
		}
		v["apps"] = per
		out = append(out, v)
	}
	return out
}

func formBool(s string) bool { b, _ := strconv.ParseBool(s); return b }

func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		out = append(out, strings.ToLower(p))
	}
	return out
}

