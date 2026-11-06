// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The OCS API: the signed-in user and Talk's conversations (rooms) and their chat, as JSON in Nextcloud's OCS envelope.
package nextcloud

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ocsReply writes {"ocs":{"meta":...,"data":...}} with the HTTP status (OCS v2 uses real HTTP codes).
func ocsReply(w http.ResponseWriter, code int, msg string, data any) {
	status := "ok"
	if code >= 300 {
		status = "failure"
	}
	if data == nil {
		data = []any{}
	}
	writeJSON(w, code, map[string]any{"ocs": map[string]any{
		"meta": map[string]any{"status": status, "statuscode": code, "message": msg}, "data": data}})
}

func (s *Server) ocs(w http.ResponseWriter, r *http.Request) {
	// like Nextcloud: an OCS call without the header is taken for a browser one and fails its CSRF check
	if r.Header.Get("OCS-APIRequest") != "true" {
		ocsReply(w, 401, "CSRF check failed", nil)
		return
	}
	me := s.user(r)
	if me == "" {
		ocsReply(w, 401, "Current user is not logged in", nil)
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/ocs/v2.php")
	switch {
	case p == "/cloud/user" && r.Method == "GET":
		ocsReply(w, 200, "OK", map[string]any{"id": me, "displayname": me, "enabled": true})
	case p == "/apps/spreed/api/v4/room" && r.Method == "GET":
		s.listRooms(w, me)
	case p == "/apps/spreed/api/v4/room" && r.Method == "POST":
		s.oneToOne(w, r, me)
	case strings.HasPrefix(p, "/apps/spreed/api/v1/chat/"):
		s.chat(w, r, me, strings.TrimPrefix(p, "/apps/spreed/api/v1/chat/"))
	default:
		ocsReply(w, 404, "Not found", nil)
	}
}

// view is the room as me sees it: in a one-to-one chat its name is the other user.
func (rm *room) view(me string) map[string]any {
	name, display := rm.Name, rm.DisplayName
	if rm.Type == 1 {
		for _, u := range rm.members {
			if u != me {
				name, display = u, u
			}
		}
	}
	var last any = []any{}
	if n := len(rm.msgs); n > 0 {
		last = rm.msgs[n-1]
	}
	return map[string]any{"token": rm.Token, "type": rm.Type, "name": name, "displayName": display, "lastMessage": last}
}

func (rm *room) has(u string) bool {
	for _, m := range rm.members {
		if m == u {
			return true
		}
	}
	return false
}

func (s *Server) listRooms(w http.ResponseWriter, me string) {
	s.mu.Lock()
	out := []any{}
	for _, rm := range s.rooms {
		if rm.has(me) {
			out = append(out, rm.view(me))
		}
	}
	s.mu.Unlock()
	ocsReply(w, 200, "OK", out)
}

// oneToOne makes (or finds) the one-to-one chat with the user invited.
func (s *Server) oneToOne(w http.ResponseWriter, r *http.Request, me string) {
	r.ParseForm()
	peer := r.PostForm.Get("invite")
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.PostForm.Get("roomType") != "1" {
		ocsReply(w, 400, "Only one-to-one chats here", nil)
		return
	}
	if _, ok := s.pass[peer]; !ok || peer == me {
		ocsReply(w, 404, "User not found", nil)
		return
	}
	for _, rm := range s.rooms {
		if rm.Type == 1 && rm.has(me) && rm.has(peer) {
			ocsReply(w, 200, "OK", rm.view(me))
			return
		}
	}
	rm := &room{Token: randHex(4), Type: 1, members: []string{me, peer}}
	s.rooms[rm.Token] = rm
	ocsReply(w, 201, "Created", rm.view(me))
}

// chat reads (the messages after lastKnownMessageId, at most limit) or posts a message.
func (s *Server) chat(w http.ResponseWriter, r *http.Request, me, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rm := s.rooms[token]
	if rm == nil || !rm.has(me) {
		ocsReply(w, 404, "Conversation not found", nil)
		return
	}
	switch r.Method {
	case "GET":
		after, _ := strconv.ParseInt(r.URL.Query().Get("lastKnownMessageId"), 10, 64)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 200 {
			limit = 100
		}
		out := []message{}
		for _, m := range rm.msgs {
			if m.ID > after && len(out) < limit {
				out = append(out, m)
			}
		}
		if len(out) == 0 {
			w.WriteHeader(http.StatusNotModified) // Talk: nothing new
			return
		}
		ocsReply(w, 200, "OK", out)
	case "POST":
		r.ParseForm()
		body := r.PostForm.Get("message")
		if strings.TrimSpace(body) == "" {
			ocsReply(w, 400, "Empty message", nil)
			return
		}
		s.lastMsg++
		m := message{ID: s.lastMsg, Token: token, ActorType: "users", ActorID: me, ActorDisplayName: me,
			Timestamp: time.Now().Unix(), Message: body, Params: []byte("[]"), MessageType: "comment"}
		rm.msgs = append(rm.msgs, m)
		ocsReply(w, 201, "Created", m)
	default:
		ocsReply(w, 405, "Method not allowed", nil)
	}
}
