// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

// The Connections API of the web app (admin devices only, requests signed like the passkey calls, see auth.go):
//
//	GET  /phe/connections                 the accounts (no secrets) with status and last sync
//	POST /phe/connections/test            {kind, config, secret}              -> {ok, detail}
//	POST /phe/connections                 {kind, provider, name, config, secret}   (tests first, then stores and starts a sync)
//	POST /phe/connections/sync            {id}
//	POST /phe/connections/remove          {id}
//	GET  /phe/duplicates                  likely duplicate hub contacts
//	POST /phe/duplicates/merge            {keep, drop}   folds `drop` into `keep`, moves its links, deletes it
//	POST /phe/duplicates/dismiss          {a, b}         "not the same person"
//	GET  /phe/links?hub=<id>              the (source, entry) links of a hub contact

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/phorepoc/cloud-companion/internal/connectors"
	"github.com/phorepoc/cloud-companion/internal/davhub"
	"github.com/phorepoc/cloud-companion/internal/store"
)

type conns struct {
	a   *auth
	st  *store.Store
	hub *davhub.Client
	m   *connectors.Manager
}

func (c *conns) admin(w http.ResponseWriter, r *http.Request) bool {
	if !c.a.adminDevice(r) {
		bad(w, 403, "sign in as the owner")
		return false
	}
	return true
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(v); err != nil {
		bad(w, 400, "bad request")
		return false
	}
	return true
}

func (c *conns) routes(mux *http.ServeMux) {
	mux.HandleFunc("/phe/connections", func(w http.ResponseWriter, r *http.Request) {
		if !c.admin(w, r) {
			return
		}
		if r.Method == http.MethodGet {
			reply(w, 200, map[string]any{"accounts": c.st.Accounts(), "hub": c.hub != nil})
			return
		}
		var in struct{ Kind, Provider, Name, Config, Secret string }
		if r.Method != http.MethodPost || !readJSON(w, r, &in) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
		defer cancel()
		if _, err := c.m.Test(ctx, in.Kind, in.Config, in.Secret); err != nil {
			bad(w, 422, "the test failed: "+err.Error())
			return
		}
		a, err := c.m.Add(in.Kind, in.Provider, in.Name, in.Config, in.Secret)
		if err != nil {
			bad(w, 400, err.Error())
			return
		}
		go func() {
			bg, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			c.m.SyncNow(bg, a.ID)
		}()
		reply(w, 200, map[string]any{"account": a})
	})
	mux.HandleFunc("/phe/connections/test", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Kind, Config, Secret string }
		if !c.admin(w, r) || !readJSON(w, r, &in) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
		defer cancel()
		d, err := c.m.Test(ctx, in.Kind, in.Config, in.Secret)
		if err != nil {
			reply(w, 200, map[string]any{"ok": false, "detail": err.Error()})
			return
		}
		reply(w, 200, map[string]any{"ok": true, "detail": d})
	})
	mux.HandleFunc("/phe/connections/sync", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ ID string }
		if !c.admin(w, r) || !readJSON(w, r, &in) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()
		sum, err := c.m.SyncNow(ctx, in.ID)
		if err != nil {
			reply(w, 200, map[string]any{"ok": false, "detail": err.Error()})
			return
		}
		reply(w, 200, map[string]any{"ok": true, "detail": sum})
	})
	mux.HandleFunc("/phe/connections/remove", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ ID string }
		if !c.admin(w, r) || !readJSON(w, r, &in) {
			return
		}
		c.m.Remove(in.ID)
		reply(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("/phe/duplicates", func(w http.ResponseWriter, r *http.Request) {
		if !c.admin(w, r) {
			return
		}
		if c.hub == nil {
			bad(w, 503, "the hub is not configured")
			return
		}
		cs, err := c.hub.Contacts()
		if err != nil {
			bad(w, 502, err.Error())
			return
		}
		out := []connectors.Suggestion{}
		for _, s := range connectors.Duplicates(cs) {
			if _, no := c.st.SyncedGet("_dup", "no:"+s.A+"|"+s.B); no {
				continue
			}
			if _, no := c.st.SyncedGet("_dup", "no:"+s.B+"|"+s.A); no {
				continue
			}
			out = append(out, s)
		}
		// both entries on ONE device/service? then an explicit merge on that device is possible
		type sug struct {
			connectors.Suggestion
			SameSource string `json:"same_source,omitempty"`
			SourceName string `json:"source_name,omitempty"`
		}
		res := make([]sug, 0, len(out))
		for _, s := range out {
			x := sug{Suggestion: s}
			la, lb := c.st.Links(s.A), c.st.Links(s.B)
			if len(la) == 1 && len(lb) == 1 && la[0].Source == lb[0].Source {
				if a, ok := c.st.Account(la[0].Source); ok && a.Kind == "carddav" {
					x.SameSource, x.SourceName = a.ID, a.Name
				}
			}
			res = append(res, x)
		}
		reply(w, 200, map[string]any{"suggestions": res, "contacts": len(cs)})
	})
	mux.HandleFunc("/phe/duplicates/dismiss", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ A, B string }
		if !c.admin(w, r) || !readJSON(w, r, &in) {
			return
		}
		c.st.SyncedSet("_dup", "no:"+in.A+"|"+in.B, "1")
		reply(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("/phe/duplicates/merge", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Keep, Drop string }
		if !c.admin(w, r) || !readJSON(w, r, &in) {
			return
		}
		if c.hub == nil || in.Keep == "" || in.Keep == in.Drop {
			bad(w, 400, "bad request")
			return
		}
		merged, err := connectors.MergeHub(c.hub, c.st, in.Keep, in.Drop)
		if err == connectors.ErrGone {
			bad(w, 404, err.Error())
			return
		}
		if err != nil {
			bad(w, 502, err.Error())
			return
		}
		reply(w, 200, map[string]any{"ok": true, "contact": merged.Name})
	})
	mux.HandleFunc("/phe/devmerge/preview", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Account, A, B string }
		if !c.admin(w, r) || !readJSON(w, r, &in) {
			return
		}
		dav, pass, err := c.m.DAV(in.Account)
		if err != nil {
			bad(w, 404, err.Error())
			return
		}
		href := func(hub string) string {
			for _, l := range c.st.Links(hub) {
				if l.Source == in.Account {
					return l.EntryID
				}
			}
			return ""
		}
		ha, hb := href(in.A), href(in.B)
		if ha == "" || hb == "" {
			bad(w, 404, "these contacts are not both entries of that account")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		p, err := connectors.PreviewDeviceMerge(ctx, c.st, in.Account, dav, pass, ha, hb)
		if err != nil {
			bad(w, 502, err.Error())
			return
		}
		reply(w, 200, p)
	})
	mux.HandleFunc("/phe/devmerge/apply", func(w http.ResponseWriter, r *http.Request) { c.devMerge(w, r, false) })
	mux.HandleFunc("/phe/devmerge/undo", func(w http.ResponseWriter, r *http.Request) { c.devMerge(w, r, true) })
	mux.HandleFunc("/phe/links", func(w http.ResponseWriter, r *http.Request) {
		if !c.admin(w, r) {
			return
		}
		reply(w, 200, map[string]any{"links": c.st.Links(r.URL.Query().Get("hub"))})
	})
}

// devMerge applies or undoes a previewed merge on the device, then syncs that account so the hub copies follow.
func (c *conns) devMerge(w http.ResponseWriter, r *http.Request, undo bool) {
	var in struct{ ID string }
	if !c.admin(w, r) || !readJSON(w, r, &in) {
		return
	}
	m, ok := c.st.DevMergeGet(in.ID)
	if !ok {
		bad(w, 404, "unknown merge")
		return
	}
	dav, pass, err := c.m.DAV(m.Account)
	if err != nil {
		bad(w, 404, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if undo {
		err = connectors.UndoDeviceMerge(ctx, c.st, in.ID, dav, pass)
	} else {
		err = connectors.ApplyDeviceMerge(ctx, c.st, in.ID, dav, pass)
	}
	if err != nil {
		bad(w, 409, err.Error())
		return
	}
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		c.m.SyncNow(bg, m.Account)
	}()
	reply(w, 200, map[string]any{"ok": true, "id": in.ID, "undo_until": time.Now().Add(connectors.UndoWindow).Unix()})
}
