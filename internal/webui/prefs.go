// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package webui

// The web app's own state that follows the person from browser to browser: the desktop's windows and dock, its widgets
// and notes. Any paired device reads and writes it; the values are the app's JSON, kept as text.
//
//	GET /phe/prefs        -> {prefs: {key: value}}
//	PUT /phe/prefs/{key}  {value}   key: a-z, 0-9, '.', '-', up to 40; value: up to 256 KiB

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/phorepoc/cloud-companion/internal/store"
)

type prefsAPI struct {
	a  *auth
	st *store.Store
}

const (
	prefsPrefix = "prefs."
	prefsMax    = 256 << 10
)

var prefKey = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,39}$`)

func (pa *prefsAPI) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /phe/prefs", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := pa.a.signedDevice(r); !ok {
			bad(w, 403, "pair this device first")
			return
		}
		p, err := pa.st.SettingsWithPrefix(prefsPrefix)
		if err != nil {
			bad(w, 500, err.Error())
			return
		}
		reply(w, 200, map[string]any{"prefs": p})
	})
	mux.HandleFunc("PUT /phe/prefs/{key}", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := pa.a.signedDevice(r); !ok {
			bad(w, 403, "pair this device first")
			return
		}
		key := r.PathValue("key")
		if !prefKey.MatchString(key) {
			bad(w, 400, "bad key")
			return
		}
		var in struct{ Value string }
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, prefsMax+1024)).Decode(&in) != nil || len(in.Value) > prefsMax {
			bad(w, 400, "bad value")
			return
		}
		if err := pa.st.SetSetting(prefsPrefix+key, in.Value); err != nil {
			bad(w, 500, err.Error())
			return
		}
		reply(w, 200, map[string]any{})
	})
}
