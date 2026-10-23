// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

// The Connections API of the web app (admin devices only, requests signed like the passkey calls, see auth.go):
//
//	GET  /api/connections                 the accounts (no secrets) with status and last sync
//	POST /api/connections/test            {kind, config, secret}              -> {ok, detail}
//	POST /api/connections                 {kind, provider, name, config, secret}   (tests first, then stores and starts a sync)
//	POST /api/connections/sync            {id}
//	POST /api/connections/remove          {id}
//	GET  /api/duplicates                  likely duplicate hub contacts
//	POST /api/duplicates/merge            {keep, drop}   folds `drop` into `keep`, moves its links, deletes it
//	POST /api/duplicates/dismiss          {a, b}         "not the same person"
//	GET  /api/links?hub=<id>              the (source, entry) links of a hub contact

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"time"

	"github.com/phorepoc/phecloud/internal/connectors"
	"github.com/phorepoc/phecloud/internal/davhub"
	"github.com/phorepoc/phecloud/internal/store"
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
	mux.HandleFunc("/api/connections", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/api/connections/test", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/api/connections/sync", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/api/connections/remove", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ ID string }
		if !c.admin(w, r) || !readJSON(w, r, &in) {
			return
		}
		c.m.Remove(in.ID)
		reply(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/duplicates", func(w http.ResponseWriter, r *http.Request) {
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
		reply(w, 200, map[string]any{"suggestions": out, "contacts": len(cs)})
	})
	mux.HandleFunc("/api/duplicates/dismiss", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ A, B string }
		if !c.admin(w, r) || !readJSON(w, r, &in) {
			return
		}
		c.st.SyncedSet("_dup", "no:"+in.A+"|"+in.B, "1")
		reply(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/duplicates/merge", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Keep, Drop string }
		if !c.admin(w, r) || !readJSON(w, r, &in) {
			return
		}
		if c.hub == nil || in.Keep == "" || in.Keep == in.Drop {
			bad(w, 400, "bad request")
			return
		}
		cs, err := c.hub.Contacts()
		if err != nil {
			bad(w, 502, err.Error())
			return
		}
		var a, b *int
		for i := range cs {
			switch connectors.HubID(cs[i].Href) {
			case in.Keep:
				x := i
				a = &x
			case in.Drop:
				x := i
				b = &x
			}
		}
		if a == nil || b == nil {
			bad(w, 404, "one of the contacts no longer exists")
			return
		}
		merged := connectors.Merge(cs[*a], cs[*b])
		if _, err := c.hub.SaveContact(merged); err != nil {
			bad(w, 502, err.Error())
			return
		}
		if err := c.hub.DeleteRaw("card", path.Base(cs[*b].Href)); err != nil {
			bad(w, 502, err.Error())
			return
		}
		c.st.MoveLinks(in.Drop, in.Keep)
		// the dropped entry's source keeps pointing at the merged contact: the next sync of that account would re-create the
		// dropped hub contact, so remember where its entries went
		reply(w, 200, map[string]any{"ok": true, "contact": merged.Name})
	})
	mux.HandleFunc("/api/links", func(w http.ResponseWriter, r *http.Request) {
		if !c.admin(w, r) {
			return
		}
		reply(w, 200, map[string]any{"links": c.st.Links(r.URL.Query().Get("hub"))})
	})
}
