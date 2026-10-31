// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

// The Weather API (internal/weather). Any paired device reads the weather and searches places; only the owner changes
// where the weather comes from. Requests are signed like the other /phe calls (auth.go).
//
//	GET  /phe/weather?lat=&lon=&name=&detail=   the weather at that place (the device's own pick), else at the default place
//	                                            -> {forecast} or {forecast: null, place: false} when there is no place yet
//	GET  /phe/weather/places?q=                 up to 10 places called q -> {places}
//	GET  /phe/weather/config                    (owner) {config, hasKey, providers}
//	POST /phe/weather/config                    (owner) {provider, units, place, baseURL, geoURL, contact, key, clearKey}
//	                                            tries the source first; an empty key keeps the stored one

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/phorepoc/cloud-companion/internal/connectors"
	"github.com/phorepoc/cloud-companion/internal/store"
	"github.com/phorepoc/cloud-companion/internal/weather"
)

type weatherAPI struct {
	a  *auth
	st *store.Store
	v  *connectors.Vault // seals the API key; nil: no keys can be stored (keyless sources still work)
	w  *weather.Service
}

const (
	weatherSetting    = "weather"
	weatherKeySetting = "weather.key"
)

// config is the stored configuration with its key opened.
func (wa *weatherAPI) config() weather.Config {
	cfg := weather.Defaults()
	if s := wa.st.Setting(weatherSetting); s != "" {
		json.Unmarshal([]byte(s), &cfg)
	}
	if k := wa.st.Setting(weatherKeySetting); k != "" && wa.v != nil {
		if b, err := base64.StdEncoding.DecodeString(k); err == nil {
			if plain, err := wa.v.Open(b); err == nil {
				cfg.Key = string(plain)
			}
		}
	}
	return cfg
}

func (wa *weatherAPI) signed(w http.ResponseWriter, r *http.Request) bool {
	if _, _, ok := wa.a.signedDevice(r); !ok {
		bad(w, 403, "pair this device first")
		return false
	}
	return true
}

func (wa *weatherAPI) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /phe/weather", func(w http.ResponseWriter, r *http.Request) {
		if !wa.signed(w, r) {
			return
		}
		cfg := wa.config()
		q := r.URL.Query()
		var p weather.Place
		lat, e1 := strconv.ParseFloat(q.Get("lat"), 64)
		lon, e2 := strconv.ParseFloat(q.Get("lon"), 64)
		switch {
		case e1 == nil && e2 == nil && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180:
			p = weather.Place{Name: q.Get("name"), Detail: q.Get("detail"), Lat: lat, Lon: lon}
		case cfg.Place != nil:
			p = *cfg.Place
		default:
			reply(w, 200, map[string]any{"forecast": nil, "place": false})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		f, err := wa.w.Forecast(ctx, cfg, p)
		if err != nil {
			bad(w, 502, err.Error())
			return
		}
		reply(w, 200, map[string]any{"forecast": f, "place": true})
	})
	mux.HandleFunc("GET /phe/weather/places", func(w http.ResponseWriter, r *http.Request) {
		if !wa.signed(w, r) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		ps, err := wa.w.Search(ctx, wa.config(), r.URL.Query().Get("q"))
		if err != nil {
			bad(w, 502, err.Error())
			return
		}
		reply(w, 200, map[string]any{"places": ps})
	})
	mux.HandleFunc("/phe/weather/config", func(w http.ResponseWriter, r *http.Request) {
		if !wa.a.adminDevice(r) {
			bad(w, 403, "sign in as the owner")
			return
		}
		cur := wa.config()
		if r.Method == http.MethodGet {
			reply(w, 200, map[string]any{"config": cur, "hasKey": cur.Key != "", "canKeepKeys": wa.v != nil, "providers": weather.Providers})
			return
		}
		var in struct {
			weather.Config
			Key      string `json:"key"`
			ClearKey bool   `json:"clearKey"`
		}
		if r.Method != http.MethodPost || !readJSON(w, r, &in) {
			return
		}
		cfg := in.Config
		switch {
		case in.Key != "":
			if wa.v == nil {
				bad(w, 400, "this server cannot keep API keys (no vault configured)")
				return
			}
			cfg.Key = in.Key
		case !in.ClearKey:
			cfg.Key = cur.Key
		}
		if err := weather.Check(cfg); err != nil {
			bad(w, 400, err.Error())
			return
		}
		// Tried before it is kept: a wrong key or address fails here, not later on every desktop.
		at := weather.Place{Name: "Oslo", Lat: 59.91, Lon: 10.75}
		if cfg.Place != nil {
			at = *cfg.Place
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		test := weather.New()
		test.HTTP = wa.w.HTTP
		if _, err := test.Forecast(ctx, cfg, at); err != nil {
			bad(w, 422, "the weather source did not answer: "+err.Error())
			return
		}
		b, _ := json.Marshal(cfg)
		if err := wa.st.SetSetting(weatherSetting, string(b)); err != nil {
			bad(w, 500, "could not save the settings")
			return
		}
		key := ""
		if cfg.Key != "" {
			key = base64.StdEncoding.EncodeToString(wa.v.Seal([]byte(cfg.Key)))
		}
		wa.st.SetSetting(weatherKeySetting, key)
		wa.w.Forget()
		reply(w, 200, map[string]any{"config": cfg, "hasKey": cfg.Key != ""})
	})
}
