// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package weather

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

func queryEscape(s string) string { return url.QueryEscape(s) }

// ---- Open-Meteo: already speaks WMO codes and daily highs and lows ----

type openMeteo struct{}

func (openMeteo) search(ctx context.Context, c *client, cfg Config, q string) ([]Place, error) {
	return openMeteoSearch(ctx, c, cfg, q)
}

func (openMeteo) forecast(ctx context.Context, c *client, cfg Config, p Place) (Forecast, error) {
	var j struct {
		Current struct {
			Temperature float64 `json:"temperature_2m"`
			Code        int     `json:"weather_code"`
			IsDay       int     `json:"is_day"`
		}
		Daily struct {
			Time []string  `json:"time"`
			Max  []float64 `json:"temperature_2m_max"`
			Min  []float64 `json:"temperature_2m_min"`
			Code []int     `json:"weather_code"`
		}
	}
	u := fmt.Sprintf("%s/v1/forecast?latitude=%.4f&longitude=%.4f&current=temperature_2m,weather_code,is_day"+
		"&daily=weather_code,temperature_2m_max,temperature_2m_min&forecast_days=3&timezone=auto",
		or(cfg.BaseURL, "https://api.open-meteo.com"), p.Lat, p.Lon)
	if cfg.Units == "f" {
		u += "&temperature_unit=fahrenheit"
	}
	if err := c.get(ctx, u, &j); err != nil {
		return Forecast{}, err
	}
	f := Forecast{Temperature: round(j.Current.Temperature), Code: j.Current.Code, Day: j.Current.IsDay == 1}
	for i, d := range j.Daily.Time {
		if i < len(j.Daily.Max) && i < len(j.Daily.Min) && i < len(j.Daily.Code) {
			f.Days = append(f.Days, Day{Date: d, High: round(j.Daily.Max[i]), Low: round(j.Daily.Min[i]), Code: j.Daily.Code[i]})
		}
	}
	return f, nil
}

// ---- MET Norway: hourly series with symbol codes, always Celsius ----

type metNo struct{}

func (metNo) search(ctx context.Context, c *client, cfg Config, q string) ([]Place, error) {
	return openMeteoSearch(ctx, c, cfg, q)
}

// symbolCode turns a MET Norway symbol ("lightrainshowers_day") into a WMO code and whether it is day.
func symbolCode(s string) (int, bool) {
	day := !strings.HasSuffix(s, "_night")
	base := strings.SplitN(s, "_", 2)[0]
	switch {
	case strings.Contains(base, "thunder"):
		return 95, day
	case strings.Contains(base, "snowshowers"):
		return 85, day
	case strings.Contains(base, "sleet"):
		return 66, day
	case strings.Contains(base, "snow"):
		return byStrength(base, 71, 73, 75), day
	case strings.Contains(base, "rainshowers"):
		return byStrength(base, 80, 80, 82), day
	case strings.Contains(base, "rain"):
		return byStrength(base, 61, 63, 65), day
	case base == "fog":
		return 45, day
	case base == "cloudy":
		return 3, day
	case base == "partlycloudy":
		return 2, day
	case base == "fair":
		return 1, day
	}
	return 0, day // clearsky
}

// byStrength picks the light, moderate or heavy code for a MET Norway symbol ("lightsnow", "snow", "heavysnow").
func byStrength(base string, light, moderate, heavy int) int {
	switch {
	case strings.HasPrefix(base, "light"):
		return light
	case strings.HasPrefix(base, "heavy"):
		return heavy
	}
	return moderate
}

func (metNo) forecast(ctx context.Context, c *client, cfg Config, p Place) (Forecast, error) {
	if cfg.Contact == "" {
		return Forecast{}, fmt.Errorf("MET Norway asks for a contact address (an e-mail or a site) in the weather settings")
	}
	var j struct {
		Properties struct {
			Timeseries []struct {
				Time string
				Data struct {
					Instant struct {
						Details struct {
							T float64 `json:"air_temperature"`
						}
					}
					Next1 *struct {
						Summary struct {
							Symbol string `json:"symbol_code"`
						}
					} `json:"next_1_hours"`
					Next6 *struct {
						Summary struct {
							Symbol string `json:"symbol_code"`
						}
					} `json:"next_6_hours"`
				}
			}
		}
	}
	u := fmt.Sprintf("%s/weatherapi/locationforecast/2.0/compact?lat=%.4f&lon=%.4f", or(cfg.BaseURL, "https://api.met.no"), p.Lat, p.Lon)
	if err := c.get(ctx, u, &j); err != nil {
		return Forecast{}, err
	}
	ts := j.Properties.Timeseries
	if len(ts) == 0 {
		return Forecast{}, fmt.Errorf("MET Norway sent no forecast")
	}
	conv := func(t float64) int {
		if cfg.Units == "f" {
			return round(t*9/5 + 32)
		}
		return round(t)
	}
	f := Forecast{Temperature: conv(ts[0].Data.Instant.Details.T)}
	if n := ts[0].Data.Next1; n != nil {
		f.Code, f.Day = symbolCode(n.Summary.Symbol)
	} else if n := ts[0].Data.Next6; n != nil {
		f.Code, f.Day = symbolCode(n.Summary.Symbol)
	}
	// The series is in UTC and has no time zone: days are split at the place's solar midnight (longitude / 15 h), close
	// enough for a high and a low. A day's conditions are the 6-hour symbol from around its midday.
	zone := time.FixedZone("", int(p.Lon/15*3600))
	type acc struct {
		hi, lo float64
		code   int
		noon   time.Duration
		set    bool
	}
	days := map[string]*acc{}
	for _, t := range ts {
		at, err := time.Parse(time.RFC3339, t.Time)
		if err != nil {
			continue
		}
		local := at.In(zone)
		d := local.Format("2006-01-02")
		a := days[d]
		if a == nil {
			a = &acc{hi: t.Data.Instant.Details.T, lo: t.Data.Instant.Details.T, noon: 99 * time.Hour}
			days[d] = a
		}
		a.hi, a.lo = max(a.hi, t.Data.Instant.Details.T), min(a.lo, t.Data.Instant.Details.T)
		if t.Data.Next6 != nil {
			off := (time.Duration(local.Hour())*time.Hour - 12*time.Hour).Abs()
			if off < a.noon {
				a.noon = off
				a.code, _ = symbolCode(t.Data.Next6.Summary.Symbol)
				a.set = true
			}
		}
	}
	keys := make([]string, 0, len(days))
	for d := range days {
		keys = append(keys, d)
	}
	sort.Strings(keys)
	for _, d := range keys {
		if len(f.Days) == 3 {
			break
		}
		a := days[d]
		code := a.code
		if !a.set {
			code = f.Code
		}
		f.Days = append(f.Days, Day{Date: d, High: conv(a.hi), Low: conv(a.lo), Code: code})
	}
	return f, nil
}

// ---- OpenWeatherMap: current weather + the 5-day / 3-hour forecast, with its own place search ----

type owm struct{}

// owmCode turns an OpenWeatherMap condition id into a WMO code.
func owmCode(id int) int {
	switch {
	case id >= 200 && id < 300:
		return 95
	case id >= 300 && id < 400:
		return 53
	case id == 500:
		return 61
	case id == 501:
		return 63
	case id >= 502 && id <= 504:
		return 65
	case id == 511:
		return 66
	case id >= 520 && id < 600:
		return 80
	case id == 600:
		return 71
	case id == 601:
		return 73
	case id == 602:
		return 75
	case id >= 611 && id <= 616:
		return 66
	case id >= 620 && id < 700:
		return 85
	case id >= 700 && id < 800:
		return 45
	case id == 801:
		return 1
	case id == 802:
		return 2
	case id >= 803:
		return 3
	}
	return 0
}

func (owm) search(ctx context.Context, c *client, cfg Config, q string) ([]Place, error) {
	var j []struct {
		Name, State, Country string
		Lat, Lon             float64
	}
	u := or(cfg.BaseURL, "https://api.openweathermap.org") + "/geo/1.0/direct?limit=10&q=" + queryEscape(q) + "&appid=" + queryEscape(cfg.Key)
	if err := c.get(ctx, u, &j); err != nil {
		return nil, hideKey(err, cfg.Key)
	}
	out := []Place{}
	for _, r := range j {
		out = append(out, Place{Name: r.Name, Detail: join(r.State, r.Country), Lat: r.Lat, Lon: r.Lon})
	}
	return out, nil
}

func hideKey(err error, key string) error {
	if key == "" {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), key, "…"))
}

func (owm) forecast(ctx context.Context, c *client, cfg Config, p Place) (Forecast, error) {
	units := map[string]string{"c": "metric", "f": "imperial"}[cfg.Units]
	base := or(cfg.BaseURL, "https://api.openweathermap.org")
	q := fmt.Sprintf("lat=%.4f&lon=%.4f&units=%s&appid=%s", p.Lat, p.Lon, units, queryEscape(cfg.Key))
	var now struct {
		Main    struct{ Temp float64 }
		Weather []struct {
			ID   int
			Icon string
		}
	}
	if err := c.get(ctx, base+"/data/2.5/weather?"+q, &now); err != nil {
		return Forecast{}, hideKey(err, cfg.Key)
	}
	f := Forecast{Temperature: round(now.Main.Temp), Day: true}
	if len(now.Weather) > 0 {
		f.Code = owmCode(now.Weather[0].ID)
		f.Day = !strings.HasSuffix(now.Weather[0].Icon, "n")
	}
	var fc struct {
		List []struct {
			Dt   int64
			Main struct {
				TempMin float64 `json:"temp_min"`
				TempMax float64 `json:"temp_max"`
			} `json:"main"`
			Weather []struct{ ID int }
		}
		City struct{ Timezone int }
	}
	if err := c.get(ctx, base+"/data/2.5/forecast?"+q, &fc); err != nil {
		return f, nil // the current reading is still worth showing
	}
	zone := time.FixedZone("", fc.City.Timezone)
	type acc struct {
		hi, lo float64
		code   int
		noon   time.Duration
	}
	days := map[string]*acc{}
	var order []string
	for _, e := range fc.List {
		local := time.Unix(e.Dt, 0).In(zone)
		d := local.Format("2006-01-02")
		a := days[d]
		if a == nil {
			a = &acc{hi: e.Main.TempMax, lo: e.Main.TempMin, noon: 99 * time.Hour}
			days[d] = a
			order = append(order, d)
		}
		a.hi, a.lo = max(a.hi, e.Main.TempMax), min(a.lo, e.Main.TempMin)
		if off := (time.Duration(local.Hour())*time.Hour - 12*time.Hour).Abs(); off < a.noon && len(e.Weather) > 0 {
			a.noon, a.code = off, owmCode(e.Weather[0].ID)
		}
	}
	for _, d := range order {
		if len(f.Days) == 3 {
			break
		}
		a := days[d]
		f.Days = append(f.Days, Day{Date: d, High: round(a.hi), Low: round(a.lo), Code: a.code})
	}
	return f, nil
}
