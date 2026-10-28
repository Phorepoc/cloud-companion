// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package catalogue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/phorepoc/cloud-companion/internal/store"
)

// Summary is what one sync of a source did.
type Summary struct {
	Items        int `json:"items"`
	Skipped      int `json:"skipped"` // listed items that were unusable (bad id, no valid release)
	Mirrored     int `json:"mirrored"`
	MirrorFailed int `json:"mirror_failed"`
}

const maxListing = 32 << 20

// CheckSourceURL accepts http(s) addresses only.
func CheckSourceURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("the address of a source is an http(s) URL")
	}
	return nil
}

func (m *Manager) get(ctx context.Context, raw, token string, sameHost *url.URL) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "phecloud-catalogue/1")
	// the bearer token belongs to the source: never sent to another host (a release file may live elsewhere, e.g. on GitHub)
	if token != "" && (sameHost == nil || req.URL.Host == sameHost.Host) {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := m.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("%s answered %d", req.URL.Host, resp.StatusCode)
	}
	return resp, nil
}

// normalizeItem checks a listed item and makes its addresses absolute; false when it is unusable.
func normalizeItem(base *url.URL, it map[string]any) (string, bool) {
	id := str(it["id"])
	if !store.IsCatalogueID(id) || str(it["name"]) == "" {
		return "", false
	}
	abs := func(s string) string {
		if s == "" {
			return ""
		}
		u, err := base.Parse(s)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return ""
		}
		return u.String()
	}
	if s := str(it["icon_url"]); s != "" {
		if a := abs(s); a != "" {
			it["icon_url"] = a
		} else {
			delete(it, "icon_url")
		}
	}
	shots := []any{}
	for _, s := range anyList(it["screenshot_urls"]) {
		if a := abs(str(s)); a != "" {
			shots = append(shots, a)
		}
	}
	it["screenshot_urls"] = shots
	good := []any{}
	for _, x := range anyList(it["resolved"]) {
		r, _ := x.(map[string]any)
		u := abs(str(r["url"]))
		if r == nil || u == "" || !IsSHA256(strings.ToLower(str(r["sha256"]))) {
			continue
		}
		r["url"], r["sha256"] = u, strings.ToLower(str(r["sha256"]))
		name := SafeName(str(r["asset"]))
		if name == "" {
			if pu, err := url.Parse(u); err == nil {
				name = SafeName(pu.Path)
			}
		}
		if name == "" {
			name = str(r["sha256"])
		}
		r["asset"] = name
		good = append(good, r)
	}
	if len(good) == 0 {
		return "", false
	}
	it["resolved"] = good
	return id, true
}

func anyList(v any) []any { l, _ := v.([]any); return l }

// SyncSource fetches a source's listing, stores it and, for what is set to be mirrored, downloads the files. Only one sync of a source runs at a time.
func (m *Manager) SyncSource(ctx context.Context, id string) (Summary, error) {
	var sum Summary
	m.mu.Lock()
	if m.syncing[id] {
		m.mu.Unlock()
		return sum, errors.New("this source is syncing already")
	}
	m.syncing[id] = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.syncing, id); m.mu.Unlock() }()

	src, ok := m.St.CatSource(id)
	if !ok {
		return sum, errors.New("unknown source")
	}
	sum, err := m.syncOne(ctx, src)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	m.St.CatSourceStatus(id, msg)
	return sum, err
}

func (m *Manager) syncOne(ctx context.Context, src store.CatSource) (Summary, error) {
	var sum Summary
	base, err := url.Parse(src.URL)
	if err != nil {
		return sum, err
	}
	resp, err := m.get(ctx, src.URL, src.Token, nil)
	if err != nil {
		return sum, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxListing+1))
	if err != nil {
		return sum, err
	}
	if len(body) > maxListing {
		return sum, errors.New("the listing is larger than 32 MiB")
	}
	var doc struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Items == nil {
		return sum, errors.New("not an index.json (no items list)")
	}
	stored := map[string]string{}
	for _, it := range doc.Items {
		id, ok := normalizeItem(base, it)
		if !ok {
			sum.Skipped++
			continue
		}
		b, _ := json.Marshal(it)
		stored[id] = string(b)
	}
	sum.Items = len(stored)
	if err := m.St.CatSourceItemsReplace(src.ID, stored); err != nil {
		return sum, err
	}
	// mirror what is set to be mirrored
	for _, row := range m.St.CatSourceItems(src.ID) {
		if !(src.Mirror && row.Mirror != 0) && row.Mirror != 1 {
			continue
		}
		var it map[string]any
		if json.Unmarshal([]byte(row.Data), &it) != nil {
			continue
		}
		for _, x := range anyList(it["resolved"]) {
			r, _ := x.(map[string]any)
			sha := str(r["sha256"])
			if m.blobOK(sha) {
				m.St.CatMirrorSet(src.ID, sha, "ok", "")
				sum.Mirrored++
				continue
			}
			if ctx.Err() != nil {
				return sum, ctx.Err()
			}
			if state, e := m.download(ctx, src, base, str(r["url"]), sha); e != nil {
				m.St.CatMirrorSet(src.ID, sha, state, e.Error())
				sum.MirrorFailed++
			} else {
				m.St.CatMirrorSet(src.ID, sha, "ok", "")
				sum.Mirrored++
			}
		}
	}
	return sum, nil
}

// download fetches one release file into the blob store and keeps it only when it hashes to what the source's listing says.
func (m *Manager) download(ctx context.Context, src store.CatSource, base *url.URL, raw, sha string) (string, error) {
	resp, err := m.get(ctx, raw, src.Token, base)
	if err != nil {
		return "failed", err
	}
	defer resp.Body.Close()
	if _, _, err := m.PutBlob(resp.Body, m.maxFile(), sha); err != nil {
		if strings.HasPrefix(err.Error(), "sha256 mismatch") {
			return "mismatch", err
		}
		return "failed", err
	}
	return "ok", nil
}

// Loop syncs the sources whose catalogue's interval has passed, checking every tick, until ctx ends.
func (m *Manager) Loop(ctx context.Context, tick time.Duration) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.SyncDue(ctx, time.Now())
		}
	}
}

// SyncDue runs one pass of the automatic sync and returns how many sources it synced.
func (m *Manager) SyncDue(ctx context.Context, now time.Time) int {
	n := 0
	for _, c := range m.St.Catalogues() {
		if c.SyncInterval <= 0 {
			continue
		}
		for _, s := range m.St.CatSources(c.ID) {
			if !s.Enabled || now.Unix()-s.LastSync < int64(c.SyncInterval) {
				continue
			}
			n++
			if _, err := m.SyncSource(ctx, s.ID); err != nil {
				slog.Warn("catalogue sync", "catalogue", c.ID, "source", s.Name, "err", err)
			}
		}
	}
	return n
}
