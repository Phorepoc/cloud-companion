// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package weather is the weather behind the desktop's Weather widget and /phe/weather: the current conditions and the next days
// for a place, and the place search of the place picker, from one of several sources the owner picks:
//
//	open-meteo      Open-Meteo (default): free, no account or key; its address can point at a self-hosted Open-Meteo
//	met-no          MET Norway's Locationforecast: free, no key; asks for a contact (e-mail or site) in the User-Agent
//	openweathermap  OpenWeatherMap: needs an API key (the free "current weather + 5 day forecast" plan is enough)
//
// Every source is reported the same way: temperatures in the owner's unit and conditions as WMO weather codes (what Open-Meteo
// uses and the Etesian weather icons draw), so the clients never care where the weather came from. Readings are kept for
// 30 minutes per place and unit.
package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Place is a town found by Search: its name, "region, country" and where it is.
type Place struct {
	Name   string  `json:"name"`
	Detail string  `json:"detail,omitempty"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
}

// Day is one day's forecast (today first).
type Day struct {
	Date string `json:"date"` // YYYY-MM-DD, the place's own date
	High int    `json:"high"`
	Low  int    `json:"low"`
	Code int    `json:"code"`
}

// Forecast is the weather at a place now and for the next days.
type Forecast struct {
	Place       Place     `json:"place"`
	Temperature int       `json:"temperature"`
	Unit        string    `json:"unit"` // "°C" or "°F"
	Code        int       `json:"code"` // WMO weather code
	Description string    `json:"description"`
	Day         bool      `json:"day"`
	Days        []Day     `json:"days"`
	Source      string    `json:"source"`
	Fetched     time.Time `json:"fetched"`
}

// Config is the owner's choice of source (stored as JSON, see Service.Config); Key is never sent back to a client.
type Config struct {
	Provider string `json:"provider"`          // open-meteo | met-no | openweathermap
	Units    string `json:"units"`             // c | f
	Place    *Place `json:"place,omitempty"`   // the default place, for devices that have not picked one
	BaseURL  string `json:"baseURL,omitempty"` // the source's own address (a self-hosted Open-Meteo; tests)
	GeoURL   string `json:"geoURL,omitempty"`  // Open-Meteo's geocoding address (place search for open-meteo and met-no)
	Contact  string `json:"contact,omitempty"` // met-no: who to contact about this server's requests
	Key      string `json:"-"`                 // openweathermap: the API key
}

// Providers lists the sources, for the settings page: id, name, whether it needs a key, and a line about it.
var Providers = []map[string]any{
	{"id": "open-meteo", "name": "Open-Meteo", "key": false, "about": "Free, no account. Can point at your own Open-Meteo."},
	{"id": "met-no", "name": "MET Norway", "key": false, "about": "Free, no account. Asks for a contact address."},
	{"id": "openweathermap", "name": "OpenWeatherMap", "key": true, "about": "Needs an API key (the free plan is enough)."},
}

type source interface {
	forecast(ctx context.Context, c *client, cfg Config, p Place) (Forecast, error)
	search(ctx context.Context, c *client, cfg Config, q string) ([]Place, error)
}

var sources = map[string]source{"open-meteo": openMeteo{}, "met-no": metNo{}, "openweathermap": owm{}}

// Check says what is wrong with a configuration, or nil.
func Check(cfg Config) error {
	if _, ok := sources[cfg.Provider]; !ok {
		return fmt.Errorf("unknown weather source %q", cfg.Provider)
	}
	if cfg.Units != "c" && cfg.Units != "f" {
		return errors.New("units must be c or f")
	}
	if cfg.Provider == "openweathermap" && cfg.Key == "" {
		return errors.New("OpenWeatherMap needs an API key")
	}
	for _, u := range []string{cfg.BaseURL, cfg.GeoURL} {
		if u != "" && !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
			return fmt.Errorf("%q is not an http(s) address", u)
		}
	}
	return nil
}

// Defaults is the configuration before the owner changes anything.
func Defaults() Config { return Config{Provider: "open-meteo", Units: "c"} }

// Service answers forecasts and searches for the current configuration, keeping readings for MaxAge.
type Service struct {
	HTTP   *http.Client
	MaxAge time.Duration
	mu     sync.Mutex
	cache  map[string]Forecast
}

func New() *Service {
	return &Service{HTTP: &http.Client{Timeout: 10 * time.Second}, MaxAge: 30 * time.Minute, cache: map[string]Forecast{}}
}

// Forecast returns the weather at p, from the cache while it is fresh.
func (s *Service) Forecast(ctx context.Context, cfg Config, p Place) (Forecast, error) {
	src, ok := sources[cfg.Provider]
	if !ok {
		return Forecast{}, fmt.Errorf("unknown weather source %q", cfg.Provider)
	}
	k := fmt.Sprintf("%s|%s|%s|%.3f,%.3f", cfg.Provider, cfg.BaseURL, cfg.Units, p.Lat, p.Lon)
	s.mu.Lock()
	f, hit := s.cache[k]
	s.mu.Unlock()
	if hit && time.Since(f.Fetched) < s.MaxAge {
		f.Place = p
		return f, nil
	}
	f, err := src.forecast(ctx, &client{s.HTTP, cfg}, cfg, p)
	if err != nil {
		return Forecast{}, err
	}
	f.Place, f.Source, f.Fetched = p, cfg.Provider, time.Now()
	f.Unit = map[string]string{"c": "°C", "f": "°F"}[cfg.Units]
	f.Description = Describe(f.Code)
	s.mu.Lock()
	s.cache[k] = f
	s.mu.Unlock()
	return f, nil
}

// Search finds up to 10 places called q.
func (s *Service) Search(ctx context.Context, cfg Config, q string) ([]Place, error) {
	src, ok := sources[cfg.Provider]
	if !ok {
		return nil, fmt.Errorf("unknown weather source %q", cfg.Provider)
	}
	if len(strings.TrimSpace(q)) < 2 {
		return []Place{}, nil
	}
	return src.search(ctx, &client{s.HTTP, cfg}, cfg, strings.TrimSpace(q))
}

// Forget drops the cached readings (after the configuration changed).
func (s *Service) Forget() {
	s.mu.Lock()
	s.cache = map[string]Forecast{}
	s.mu.Unlock()
}

// Describe names a WMO weather code, as the phone does (Weather.kt).
func Describe(code int) string {
	switch {
	case code == 0:
		return "Clear"
	case code == 1:
		return "Mainly clear"
	case code == 2:
		return "Partly cloudy"
	case code == 3:
		return "Overcast"
	case code == 45 || code == 48:
		return "Fog"
	case code >= 51 && code <= 57:
		return "Drizzle"
	case code >= 61 && code <= 67:
		return "Rain"
	case code >= 71 && code <= 77:
		return "Snow"
	case code >= 80 && code <= 82:
		return "Showers"
	case code == 85 || code == 86:
		return "Snow showers"
	case code >= 95:
		return "Thunderstorm"
	}
	return ""
}

type client struct {
	h   *http.Client
	cfg Config
}

// get fetches a JSON document into v.
func (c *client) get(ctx context.Context, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	ua := "cloud-companion (+https://github.com/phorepoc/cloud-companion)"
	if c.cfg.Contact != "" {
		ua += " " + c.cfg.Contact
	}
	req.Header.Set("User-Agent", ua)
	resp, err := c.h.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("%s answered %d: %s", strings.SplitN(url, "?", 2)[0], resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v)
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return strings.TrimRight(s, "/")
}

func round(f float64) int {
	if f < 0 {
		return -int(-f + 0.5)
	}
	return int(f + 0.5)
}

// openMeteoSearch is Open-Meteo's geocoding (also the place search for met-no, which has none of its own).
func openMeteoSearch(ctx context.Context, c *client, cfg Config, q string) ([]Place, error) {
	var j struct {
		Results []struct {
			Name, Admin1, Country string
			Latitude, Longitude   float64
		}
	}
	u := or(cfg.GeoURL, "https://geocoding-api.open-meteo.com") + "/v1/search?count=10&name=" + queryEscape(q)
	if err := c.get(ctx, u, &j); err != nil {
		return nil, err
	}
	out := []Place{}
	for _, r := range j.Results {
		out = append(out, Place{Name: r.Name, Detail: join(r.Admin1, r.Country), Lat: r.Latitude, Lon: r.Longitude})
	}
	return out, nil
}

func join(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if p != "" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, ", ")
}
