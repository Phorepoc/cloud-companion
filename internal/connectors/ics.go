// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package connectors

// A subscribed calendar: a read-only iCalendar feed at a URL (Proton's calendar share link, a public holiday calendar, any ".ics" link). Every
// VEVENT becomes its own resource in the hub calendar (<account>-<id>.ics, with the feed's time zone definitions); events that change are rewritten,
// events that disappear from the feed are deleted. Nothing is ever written back. webcal:// is read as https://.

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const icsMaxBytes = 16 << 20

func fetchICS(ctx context.Context, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(raw), "webcal://") {
		raw = "https://" + raw[len("webcal://"):]
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", errors.New("the link must start with https:// (or webcal://)")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	req.Header.Set("Accept", "text/calendar, */*")
	resp, err := (&http.Client{Timeout: 45 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("the link answered HTTP %d (a share link may have been turned off or changed)", resp.StatusCode)
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, icsMaxBytes+1))
	if len(b) > icsMaxBytes {
		return "", errors.New("the calendar is larger than 16 MB")
	}
	s := string(b)
	if !strings.Contains(s, "BEGIN:VCALENDAR") {
		return "", errors.New("the link does not return an iCalendar feed")
	}
	return s, nil
}

// TestICS fetches the feed and counts its events.
func TestICS(ctx context.Context, c DAVCfg) (int, error) {
	s, err := fetchICS(ctx, c.URL)
	if err != nil {
		return 0, err
	}
	_, evs := splitICS(s)
	return len(evs), nil
}

type icsEvent struct{ id, text string }

// splitICS returns the feed's time zone definitions and its events (unfolded lines, CRLF).
func splitICS(s string) (zones string, events []icsEvent) {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\n ", ""), "\n\t", "") // unfold
	var cur []string
	var kind string
	for _, l := range strings.Split(s, "\n") {
		switch {
		case strings.HasPrefix(l, "BEGIN:VEVENT"):
			cur, kind = []string{l}, "event"
		case strings.HasPrefix(l, "BEGIN:VTIMEZONE"):
			cur, kind = []string{l}, "zone"
		case cur != nil && strings.HasPrefix(l, "END:VEVENT") && kind == "event":
			cur = append(cur, l)
			var uid, rid string
			for _, x := range cur {
				if strings.HasPrefix(x, "UID:") {
					uid = strings.TrimPrefix(x, "UID:")
				}
				if strings.HasPrefix(x, "RECURRENCE-ID") {
					rid = x[strings.Index(x, ":")+1:]
				}
			}
			if uid == "" {
				uid = fmt.Sprint(len(events)) + strings.Join(cur, "")
			}
			h := sha1.Sum([]byte(uid + "|" + rid))
			events = append(events, icsEvent{id: hex.EncodeToString(h[:6]), text: strings.Join(cur, "\r\n") + "\r\n"})
			cur = nil
		case cur != nil && strings.HasPrefix(l, "END:VTIMEZONE") && kind == "zone":
			cur = append(cur, l)
			zones += strings.Join(cur, "\r\n") + "\r\n"
			cur = nil
		case cur != nil:
			cur = append(cur, l)
		}
	}
	return zones, events
}

// SyncICS mirrors the feed into the hub calendar; returns how many events were written and how many deleted.
func SyncICS(ctx context.Context, account string, c DAVCfg, hub HubDAV, st SyncStore) (changed, removed int, err error) {
	feed, err := fetchICS(ctx, c.URL)
	if err != nil {
		return 0, 0, err
	}
	zones, evs := splitICS(feed)
	if len(evs) > 20000 {
		return 0, 0, errors.New("the calendar has more than 20000 events")
	}
	known := st.SyncedKeys(account, "ics:")
	present := map[string]bool{}
	for _, e := range evs {
		key := "ics:" + e.id
		present[key] = true
		sum := sha1.Sum([]byte(e.text))
		h := hex.EncodeToString(sum[:8])
		if old, ok := known[key]; ok && strings.HasPrefix(old, h+"|") {
			continue
		}
		name := account + "-" + e.id + ".ics"
		body := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Phecloud//ICS subscription//EN\r\n" + zones + e.text + "END:VCALENDAR\r\n"
		if err := hub.PutRaw("cal", name, body); err != nil {
			return changed, removed, err
		}
		st.SyncedSet(account, key, h+"|"+name)
		changed++
	}
	for key, v := range known {
		if present[key] {
			continue
		}
		if i := strings.Index(v, "|"); i >= 0 {
			if err := hub.DeleteRaw("cal", v[i+1:]); err != nil {
				return changed, removed, err
			}
		}
		st.SyncedDelete(account, key)
		removed++
	}
	return changed, removed, nil
}
