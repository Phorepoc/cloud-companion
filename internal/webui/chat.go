// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package webui

// Chat connections and routing (docs/messaging.md), for the owner:
//
//	GET  /phe/routing         {connections:[{id,label,options}], routes:[{kind,value,conn,opt}], deliver:{conn:[login]}, logins:[...]}
//	PUT  /phe/routing         {routes:[...], deliver:{conn:[login]}}   replaces the rules (first match wins) and who gets which connection
//	POST /phe/sip/logins      {name}  -> {name, password, realm}   (the password is shown once)
//	POST /phe/sip/logins/remove {name}
//	POST /phe/signal/link     {api}   -> {png}   (base64; the QR code to scan in Signal → Settings → Linked devices)
//	POST /phe/telegram/login  {api_id, api_hash} -> {id}   starts a QR sign-in (docs/messaging.md)
//	GET  /phe/telegram/login?id=  -> {state: starting|waiting|password|done|failed, png, error, user_id, name}
//	POST /phe/telegram/login/password {id, password}   the two-step password, when the state asks for it

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
	"time"

	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/signal"
	"github.com/phorepoc/cloud-companion/internal/store"
	"github.com/phorepoc/cloud-companion/internal/telegram"
)

type chatAPI struct {
	a       *auth
	st      *store.Store
	hub     *chat.Hub
	dataDir string // where Telegram sessions are kept; empty: no Telegram sign-in

	mu     sync.Mutex
	logins map[string]*tgLogin
}

type tgLogin struct {
	l       *telegram.Login
	started time.Time
}

func (c *chatAPI) owner(w http.ResponseWriter, r *http.Request) bool {
	if !c.a.adminDevice(r) {
		bad(w, 403, "sign in as the owner")
		return false
	}
	return true
}

func (c *chatAPI) routes(mux *http.ServeMux) {
	mux.HandleFunc("/phe/routing", func(w http.ResponseWriter, r *http.Request) {
		if !c.owner(w, r) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			reply(w, 200, map[string]any{"connections": c.hub.Conns(), "routes": c.st.ChatRoutes(), "deliver": c.st.ChatDeliver(), "logins": c.st.SIPUsers()})
		case http.MethodPut:
			var in struct {
				Routes  []store.ChatRoute   `json:"routes"`
				Deliver map[string][]string `json:"deliver"`
			}
			if !readJSON(w, r, &in) {
				return
			}
			known := map[string]bool{}
			for _, ci := range c.hub.Conns() {
				known[ci.ID] = true
			}
			for _, rt := range in.Routes {
				if !known[rt.Conn] {
					bad(w, 400, "no connection "+rt.Conn)
					return
				}
			}
			if err := c.st.SetChatRoutes(in.Routes); err != nil {
				bad(w, 400, err.Error())
				return
			}
			for conn, logins := range in.Deliver {
				if !known[conn] {
					bad(w, 400, "no connection "+conn)
					return
				}
				if err := c.st.SetChatDeliver(conn, logins); err != nil {
					bad(w, 400, err.Error())
					return
				}
			}
			reply(w, 200, map[string]any{"ok": true})
		default:
			bad(w, 405, "GET or PUT")
		}
	})
	mux.HandleFunc("/phe/sip/logins", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name string }
		if !c.owner(w, r) || r.Method != http.MethodPost || !readJSON(w, r, &in) {
			return
		}
		pw, err := c.st.AddSIPUser(in.Name)
		if err != nil {
			bad(w, 400, err.Error())
			return
		}
		reply(w, 200, map[string]any{"name": in.Name, "password": pw, "realm": store.SIPRealm})
	})
	mux.HandleFunc("/phe/sip/logins/remove", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name string }
		if !c.owner(w, r) || r.Method != http.MethodPost || !readJSON(w, r, &in) {
			return
		}
		if err := c.st.RemoveSIPUser(in.Name); err != nil {
			bad(w, 404, err.Error())
			return
		}
		reply(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("/phe/signal/link", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ API string }
		if !c.owner(w, r) || r.Method != http.MethodPost || !readJSON(w, r, &in) {
			return
		}
		png, err := signal.LinkQR(r.Context(), in.API)
		if err != nil {
			bad(w, 502, err.Error())
			return
		}
		reply(w, 200, map[string]any{"png": base64.StdEncoding.EncodeToString(png)})
	})
	mux.HandleFunc("/phe/telegram/login", func(w http.ResponseWriter, r *http.Request) {
		if !c.owner(w, r) {
			return
		}
		switch r.Method {
		case http.MethodPost:
			var in struct {
				APIID   int    `json:"api_id"`
				APIHash string `json:"api_hash"`
			}
			if !readJSON(w, r, &in) {
				return
			}
			if c.dataDir == "" {
				bad(w, 503, "connections are off on this server")
				return
			}
			l, err := telegram.NewLogin(c.dataDir, in.APIID, in.APIHash)
			if err != nil {
				bad(w, 400, err.Error())
				return
			}
			id := rand.Text()
			c.mu.Lock()
			if c.logins == nil {
				c.logins = map[string]*tgLogin{}
			}
			for k, old := range c.logins { // a sign-in gives up after 5 minutes; forget it a while after
				if time.Since(old.started) > 15*time.Minute {
					old.l.Cancel()
					delete(c.logins, k)
				}
			}
			c.logins[id] = &tgLogin{l: l, started: time.Now()}
			c.mu.Unlock()
			reply(w, 200, map[string]any{"id": id})
		case http.MethodGet:
			if l := c.login(r.URL.Query().Get("id")); l != nil {
				reply(w, 200, l.State())
			} else {
				bad(w, 404, "no such sign-in; start again")
			}
		default:
			bad(w, 405, "GET or POST")
		}
	})
	mux.HandleFunc("/phe/telegram/login/password", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ ID, Password string }
		if !c.owner(w, r) || r.Method != http.MethodPost || !readJSON(w, r, &in) {
			return
		}
		l := c.login(in.ID)
		if l == nil {
			bad(w, 404, "no such sign-in; start again")
			return
		}
		l.Password(in.Password)
		reply(w, 200, map[string]any{"ok": true})
	})
}

func (c *chatAPI) login(id string) *telegram.Login {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t := c.logins[id]; t != nil {
		return t.l
	}
	return nil
}
