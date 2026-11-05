// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

// Chat connections and routing (docs/messaging.md), for the owner:
//
//	GET  /phe/routing         {connections:[{id,label,options}], routes:[{kind,value,conn,opt}], deliver:{conn:[login]}, logins:[...]}
//	PUT  /phe/routing         {routes:[...], deliver:{conn:[login]}}   replaces the rules (first match wins) and who gets which connection
//	POST /phe/sip/logins      {name}  -> {name, password, realm}   (the password is shown once)
//	POST /phe/sip/logins/remove {name}

import (
	"net/http"

	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
)

type chatAPI struct {
	a   *auth
	st  *store.Store
	hub *chat.Hub
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
}
