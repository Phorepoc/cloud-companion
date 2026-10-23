// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

// SMS through the user's phones (hub-design.md section 2).
//
// Device side (any paired device; requests signed with the device key like the other /api calls, X-Phe-Auth):
//
//	POST /sms/sync   {messages:[{id, number, dir:"in"|"out", body, ts, status?}], acks:[{out_id, id, status:"sent"|"delivered"|"failed", error?}]}
//	                 -> {accepted, pending:[{out_id, number, body}]}
//	  `id` is the PHONE's own message id: uploading the same id again changes nothing, so a retry after a dropped connection is safe.
//	  `acks` report on messages the web app asked the phone to send (`out_id` is the hub's id from `pending`; `id` the phone's id for it).
//	  The reply carries the phone's pending outbound queue.
//
// Web side (admin devices):
//
//	GET  /sms/conversations   conversations by number, with the hub contact name when the number matches one, and the devices
//	GET  /sms/thread?key=     the messages of one conversation
//	POST /sms/send            {device, number, body}  -> queued for that device (the phone's next sync picks it up)

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/phorepoc/phecloud/internal/davhub"
	"github.com/phorepoc/phecloud/internal/store"
)

type smsAPI struct {
	a   *auth
	st  *store.Store
	hub *davhub.Client
	mu  sync.Mutex
	nm  map[string]string // number key -> hub contact name
	nmT time.Time
}

// names maps number keys to hub contact names (cached for a minute).
func (s *smsAPI) names() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nm != nil && time.Since(s.nmT) < time.Minute {
		return s.nm
	}
	m := map[string]string{}
	if s.hub != nil {
		if cs, err := s.hub.Contacts(); err == nil {
			for _, c := range cs {
				for _, p := range c.Phones {
					if k := store.NumberKey(p.Value); k != "" {
						m[k] = c.Name
					}
				}
			}
		}
	}
	s.nm, s.nmT = m, time.Now()
	return m
}

func (s *smsAPI) routes(mux *http.ServeMux) {
	// Pairing for a phone service that cannot speak the frame protocol (the Android app): the same one-time code `phecloud paircode`
	// prints, the same device key. The code must come from a non-admin context (a first-ever device still becomes the admin, as
	// everywhere else). Wrong codes are rate-limited like the passkey codes.
	mux.HandleFunc("/sms/pair", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Code, Name string }
		if r.Method != http.MethodPost || json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil {
			bad(w, 400, "bad request")
			return
		}
		if s.a.limited(r) {
			bad(w, 429, "too many attempts, wait a minute")
			return
		}
		name := strings.TrimSpace(in.Name)
		if name == "" || len(name) > 40 {
			name = "Android phone"
		}
		id, key, _, err := s.st.Pair(strings.ToUpper(strings.TrimSpace(in.Code)), name)
		if err != nil {
			s.a.failed(r)
			bad(w, 403, "that pairing code did not work")
			return
		}
		reply(w, 200, map[string]any{"dev": id, "key": hex.EncodeToString(key)})
	})
	mux.HandleFunc("/sms/sync", func(w http.ResponseWriter, r *http.Request) {
		dev, _, ok := s.a.signedDevice(r)
		if !ok || r.Method != http.MethodPost {
			bad(w, 403, "not a paired device")
			return
		}
		var in struct {
			Messages []struct {
				ID, Number, Dir, Body, Status string
				TS                            int64
			}
			Acks []struct {
				OutID             int64 `json:"out_id"`
				ID, Status, Error string
			}
		}
		if !readJSON(w, r, &in) {
			return
		}
		if len(in.Messages) > 500 || len(in.Acks) > 500 {
			bad(w, 413, "at most 500 messages per sync")
			return
		}
		batch := make([]store.SMS, 0, len(in.Messages))
		for _, m := range in.Messages {
			batch = append(batch, store.SMS{DevMsgID: m.ID, Number: m.Number, Dir: m.Dir, Body: m.Body, TS: m.TS, Status: m.Status})
		}
		n, err := s.st.SMSUpload(dev, batch)
		if err != nil {
			bad(w, 500, "could not store the messages")
			return
		}
		for _, a := range in.Acks {
			s.st.SMSAck(dev, a.OutID, a.ID, a.Status, a.Error) // an unknown id is ignored: the phone must not stall on it
		}
		pend := []map[string]any{}
		for _, m := range s.st.SMSPending(dev) {
			pend = append(pend, map[string]any{"out_id": m.ID, "number": m.Number, "body": m.Body})
		}
		reply(w, 200, map[string]any{"accepted": n, "pending": pend})
	})
	mux.HandleFunc("/sms/conversations", func(w http.ResponseWriter, r *http.Request) {
		if !s.a.adminDevice(r) {
			bad(w, 403, "sign in as the owner")
			return
		}
		cs, names := s.st.SMSConversations(), s.names()
		out := make([]map[string]any, 0, len(cs))
		for _, c := range cs {
			out = append(out, map[string]any{"key": c.Key, "number": c.Number, "name": names[c.Key], "last": c.Last, "last_ts": c.LastTS, "last_dir": c.LastDir,
				"count": c.Count, "devices": strings.Split(c.Devices, ","), "last_failed": c.LastFail})
		}
		reply(w, 200, map[string]any{"conversations": out, "devices": s.st.SMSDevices()})
	})
	mux.HandleFunc("/sms/thread", func(w http.ResponseWriter, r *http.Request) {
		if !s.a.adminDevice(r) {
			bad(w, 403, "sign in as the owner")
			return
		}
		lim, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if lim <= 0 || lim > 500 {
			lim = 200
		}
		reply(w, 200, map[string]any{"messages": s.st.SMSThread(r.URL.Query().Get("key"), lim)})
	})
	mux.HandleFunc("/sms/send", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Device, Number, Body string }
		if r.Method != http.MethodPost || !s.a.adminDevice(r) || !readJSON(w, r, &in) {
			if r.Method == http.MethodPost && !s.a.adminDevice(r) {
				bad(w, 403, "sign in as the owner")
			}
			return
		}
		known := false
		for _, d := range s.st.SMSDevices() {
			if d.Device == in.Device {
				known = true
			}
		}
		if !known {
			bad(w, 404, "that device does not sync SMS")
			return
		}
		id, err := s.st.SMSQueue(in.Device, in.Number, in.Body)
		if err != nil {
			bad(w, 400, err.Error())
			return
		}
		reply(w, 200, map[string]any{"id": id})
	})
}
