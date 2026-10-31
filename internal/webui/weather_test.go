// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/phorepoc/cloud-companion/internal/connectors"
	"github.com/phorepoc/cloud-companion/internal/store"
	"github.com/phorepoc/cloud-companion/internal/weather"
)

func TestWeatherAPI(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/forecast":
			w.Write([]byte(`{"current":{"temperature_2m":12,"weather_code":2,"is_day":1},"daily":{"time":["2026-10-31"],"temperature_2m_max":[14],"temperature_2m_min":[6],"weather_code":[2]}}`))
		case "/v1/search":
			w.Write([]byte(`{"results":[{"name":"Tampere","latitude":61.5,"longitude":23.79,"admin1":"Pirkanmaa","country":"Finland"}]}`))
		case "/data/2.5/weather":
			if r.URL.Query().Get("appid") != "secret-key" {
				w.WriteHeader(401)
				w.Write([]byte(`{"message":"Invalid API key ` + r.URL.Query().Get("appid") + `"}`))
				return
			}
			w.Write([]byte(`{"main":{"temp":3},"weather":[{"id":800,"icon":"01d"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer up.Close()
	st := store.OpenTest(t)
	v, err := connectors.OpenVault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&weatherAPI{a: newAuth(st), st: st, v: v, w: weather.New()}).routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mk := func(admin bool) *fakeDevice {
		id, key, err := st.NewDevice("d", admin)
		if err != nil {
			t.Fatal(err)
		}
		return &fakeDevice{id, key, srv}
	}
	owner, phone := mk(true), mk(false)

	if code, _ := (&fakeDevice{"nobody", []byte("x"), srv}).call(t, "GET", "/phe/weather", nil); code != 403 {
		t.Fatalf("an unpaired caller got %d", code)
	}
	if code, j := phone.call(t, "GET", "/phe/weather", nil); code != 200 || j["place"] != false {
		t.Fatalf("no place yet: %d %v", code, j)
	}
	if code, _ := phone.call(t, "POST", "/phe/weather/config", map[string]any{"provider": "open-meteo", "units": "c"}); code != 403 {
		t.Fatal("only the owner changes the weather source")
	}
	cfg := map[string]any{"provider": "open-meteo", "units": "c", "baseURL": up.URL, "geoURL": up.URL,
		"place": map[string]any{"name": "Tampere", "detail": "Pirkanmaa, Finland", "lat": 61.5, "lon": 23.79}}
	if code, j := owner.call(t, "POST", "/phe/weather/config", cfg); code != 200 {
		t.Fatalf("saving the config: %d %v", code, j)
	}
	code, j := phone.call(t, "GET", "/phe/weather", nil)
	f, _ := j["forecast"].(map[string]any)
	if code != 200 || f == nil || f["temperature"] != 12.0 || f["description"] != "Partly cloudy" || f["place"].(map[string]any)["name"] != "Tampere" {
		t.Fatalf("the default place's weather: %d %v", code, j)
	}
	code, j = phone.call(t, "GET", "/phe/weather?lat=65.01&lon=25.47&name=Oulu", nil)
	if f, _ := j["forecast"].(map[string]any); code != 200 || f["place"].(map[string]any)["name"] != "Oulu" {
		t.Fatalf("a device's own place: %d %v", code, j)
	}
	code, j = phone.call(t, "GET", "/phe/weather/places?q=Tamp", nil)
	if ps, _ := j["places"].([]any); code != 200 || len(ps) != 1 {
		t.Fatalf("place search: %d %v", code, j)
	}

	// OpenWeatherMap: a wrong key is refused before it is kept and never echoed; the right one is sealed, not stored plain.
	owm := map[string]any{"provider": "openweathermap", "units": "c", "baseURL": up.URL, "key": "wrong-key"}
	code, j = owner.call(t, "POST", "/phe/weather/config", owm)
	if code != 422 || strings.Contains(j["error"].(string), "wrong-key") {
		t.Fatalf("a wrong key: %d %v", code, j)
	}
	owm["key"] = "secret-key"
	if code, j := owner.call(t, "POST", "/phe/weather/config", owm); code != 200 || j["hasKey"] != true {
		t.Fatalf("the right key: %d %v", code, j)
	}
	if strings.Contains(st.Setting("weather.key"), "secret-key") || strings.Contains(st.Setting("weather"), "secret-key") {
		t.Fatal("the API key is stored in the clear")
	}
	code, j = owner.call(t, "GET", "/phe/weather/config", nil)
	if code != 200 || j["hasKey"] != true || strings.Contains(stringOf(j), "secret-key") {
		t.Fatalf("the config read back: %d %v", code, j)
	}
	delete(owm, "key") // saving again without a key keeps the stored one
	if code, j := owner.call(t, "POST", "/phe/weather/config", owm); code != 200 {
		t.Fatalf("keeping the key: %d %v", code, j)
	}
}

func stringOf(v any) string { return fmt.Sprint(v) }
