// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package weather

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSymbolCode(t *testing.T) {
	for sym, want := range map[string]int{"clearsky_day": 0, "fair_night": 1, "partlycloudy_day": 2, "cloudy": 3, "fog": 45,
		"lightrain": 61, "rain": 63, "heavyrain": 65, "lightrainshowers_day": 80, "heavyrainshowers_night": 82, "sleet": 66,
		"lightsnow": 71, "snow": 73, "heavysnow": 75, "snowshowers_day": 85, "rainandthunder": 95} {
		if got, _ := symbolCode(sym); got != want {
			t.Errorf("%s: got %d, want %d", sym, got, want)
		}
	}
	if _, day := symbolCode("clearsky_night"); day {
		t.Error("_night should be night")
	}
}

func TestOWMCode(t *testing.T) {
	for id, want := range map[int]int{211: 95, 301: 53, 500: 61, 502: 65, 521: 80, 601: 73, 621: 85, 741: 45, 800: 0, 802: 2, 804: 3} {
		if got := owmCode(id); got != want {
			t.Errorf("%d: got %d, want %d", id, got, want)
		}
	}
}

// fake answers each source's requests from canned JSON, by path.
func fake(t *testing.T, seen *[]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.URL.String()+" UA="+r.UserAgent())
		switch r.URL.Path {
		case "/v1/forecast":
			w.Write([]byte(`{"current":{"temperature_2m":4.4,"weather_code":3,"is_day":1},"daily":{"time":["2026-10-31","2026-11-01"],
				"temperature_2m_max":[6.2,7],"temperature_2m_min":[1.6,-0.6],"weather_code":[3,61]}}`))
		case "/v1/search":
			w.Write([]byte(`{"results":[{"name":"Oulu","latitude":65.01,"longitude":25.47,"admin1":"North Ostrobothnia","country":"Finland"},
				{"name":"Oulu","latitude":46.33,"longitude":-91.41,"admin1":"Wisconsin","country":"United States"}]}`))
		case "/weatherapi/locationforecast/2.0/compact":
			w.Write([]byte(`{"properties":{"timeseries":[
				{"time":"2026-10-31T10:00:00Z","data":{"instant":{"details":{"air_temperature":5.0}},"next_1_hours":{"summary":{"symbol_code":"lightrain"}},"next_6_hours":{"summary":{"symbol_code":"rain"}}}},
				{"time":"2026-10-31T16:00:00Z","data":{"instant":{"details":{"air_temperature":2.0}},"next_6_hours":{"summary":{"symbol_code":"cloudy"}}}},
				{"time":"2026-11-01T10:00:00Z","data":{"instant":{"details":{"air_temperature":8.0}},"next_6_hours":{"summary":{"symbol_code":"clearsky_day"}}}}]}}`))
		case "/data/2.5/weather":
			w.Write([]byte(`{"main":{"temp":40.2},"weather":[{"id":601,"icon":"13n"}]}`))
		case "/data/2.5/forecast":
			w.Write([]byte(`{"city":{"timezone":0},"list":[{"dt":1793444400,"main":{"temp_min":30,"temp_max":38},"weather":[{"id":500}]},
				{"dt":1793455200,"main":{"temp_min":28,"temp_max":41},"weather":[{"id":800}]}]}`))
		case "/geo/1.0/direct":
			w.Write([]byte(`[{"name":"Athens","state":"Georgia","country":"US","lat":33.96,"lon":-83.38}]`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestSources(t *testing.T) {
	var seen []string
	srv := fake(t, &seen)
	defer srv.Close()
	ctx, s := context.Background(), New()
	at := Place{Name: "Oulu", Lat: 65.01, Lon: 25.47}

	f, err := s.Forecast(ctx, Config{Provider: "open-meteo", Units: "c", BaseURL: srv.URL}, at)
	if err != nil || f.Temperature != 4 || f.Code != 3 || f.Description != "Overcast" || len(f.Days) != 2 || f.Days[1].Low != -1 || f.Unit != "°C" {
		t.Fatalf("open-meteo: %+v %v", f, err)
	}
	if _, err := s.Forecast(ctx, Config{Provider: "open-meteo", Units: "c", BaseURL: srv.URL}, at); err != nil || len(seen) != 1 {
		t.Fatalf("a second reading within 30 minutes should come from the cache (%d requests)", len(seen))
	}
	if _, err := s.Forecast(ctx, Config{Provider: "met-no", Units: "c", BaseURL: srv.URL}, at); err == nil {
		t.Fatal("met-no without a contact should be refused")
	}
	f, err = s.Forecast(ctx, Config{Provider: "met-no", Units: "f", BaseURL: srv.URL, Contact: "me@example.org"}, at)
	if err != nil || f.Temperature != 41 || f.Code != 61 || len(f.Days) != 2 || f.Days[0].Code != 63 || f.Days[0].High != 41 || f.Days[0].Low != 36 {
		t.Fatalf("met-no: %+v %v", f, err)
	}
	if !strings.Contains(seen[len(seen)-1], "me@example.org") {
		t.Fatal("met-no requests should carry the contact in the User-Agent")
	}
	f, err = s.Forecast(ctx, Config{Provider: "openweathermap", Units: "f", BaseURL: srv.URL, Key: "k1"}, at)
	if err != nil || f.Temperature != 40 || f.Code != 73 || f.Day || len(f.Days) != 1 || f.Days[0].High != 41 {
		t.Fatalf("openweathermap: %+v %v", f, err)
	}
	ps, err := s.Search(ctx, Config{Provider: "met-no", GeoURL: srv.URL}, "Oulu")
	if err != nil || len(ps) != 2 || ps[1].Detail != "Wisconsin, United States" {
		t.Fatalf("search: %+v %v", ps, err)
	}
	ps, err = s.Search(ctx, Config{Provider: "openweathermap", BaseURL: srv.URL, Key: "k1"}, "Athens")
	if err != nil || len(ps) != 1 || ps[0].Detail != "Georgia, US" {
		t.Fatalf("owm search: %+v %v", ps, err)
	}
	if err := Check(Config{Provider: "openweathermap", Units: "c"}); err == nil {
		t.Fatal("OpenWeatherMap without a key should not pass the check")
	}
}
