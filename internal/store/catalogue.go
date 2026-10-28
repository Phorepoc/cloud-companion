// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"time"
)

// Self-hosted catalogues (docs/catalogue.md). A catalogue is a list of installable software served in the format of Phorepoc's index.json. It holds
// releases the owner uploaded (cat_releases: one row per app id and version, the JSON in `data` is internal/catalogue's Release) and federated
// sources (cat_sources) whose last fetched listing is kept per app in cat_source_items, so serving never waits for the network. cat_mirror says which
// release files of a source are cached locally and verified. The files themselves live on disk, named by their SHA-256.

var catIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,62}$`)

type Catalogue struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Public       bool   `json:"public"`
	Token        string `json:"token,omitempty"` // private catalogues: the secret in the URL path / bearer header
	Priority     int    `json:"priority"`        // of the hosted (local) entries against the sources
	SyncInterval int    `json:"sync_interval"`   // seconds between automatic syncs of its sources; 0 = only on request
	Created      int64  `json:"created"`
}

type CatRelease struct {
	Catalogue string
	AppID     string
	Version   string
	Data      string
	Created   int64
}

type CatSource struct {
	ID        string `json:"id"`
	Catalogue string `json:"catalogue"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Token     string `json:"-"` // sent as a bearer token when set (a token that is part of the URL needs no field)
	Priority  int    `json:"priority"`
	Enabled   bool   `json:"enabled"`
	Mirror    bool   `json:"mirror"`
	LastSync  int64  `json:"last_sync"`
	LastError string `json:"last_error"`
	Created   int64  `json:"created"`
}

// CatSourceItem is one app of a source's last listing; Mirror is the per-app override: -1 follows the source, 0 never, 1 always.
type CatSourceItem struct {
	AppID  string
	Data   string
	Mirror int
}

type CatMirror struct {
	SHA256 string `json:"sha256"`
	State  string `json:"state"` // ok | failed | mismatch
	Error  string `json:"error,omitempty"`
}

func IsCatalogueID(id string) bool { return catIDRe.MatchString(id) }

func NewCatToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) CatalogueAdd(c Catalogue) (Catalogue, error) {
	if !IsCatalogueID(c.ID) {
		return c, errors.New("the id is lower case letters, digits, '-' and '.', 2 to 63 characters")
	}
	if _, ok := s.Catalogue(c.ID); ok {
		return c, errors.New("a catalogue with that id exists")
	}
	if c.Name == "" {
		c.Name = c.ID
	}
	if !c.Public && c.Token == "" {
		c.Token = NewCatToken()
	}
	c.Created = time.Now().Unix()
	_, err := s.db.Exec(`INSERT INTO catalogues(id,name,public,token,priority,sync_interval,created) VALUES(?,?,?,?,?,?,?)`,
		c.ID, c.Name, b2i(c.Public), c.Token, c.Priority, c.SyncInterval, c.Created)
	return c, err
}

func (s *Store) Catalogues() []Catalogue {
	rows, err := s.db.Query(`SELECT id,name,public,COALESCE(token,''),priority,sync_interval,created FROM catalogues ORDER BY created,id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []Catalogue{}
	for rows.Next() {
		var c Catalogue
		var pub int
		if rows.Scan(&c.ID, &c.Name, &pub, &c.Token, &c.Priority, &c.SyncInterval, &c.Created) == nil {
			c.Public = pub != 0
			out = append(out, c)
		}
	}
	return out
}

func (s *Store) Catalogue(id string) (Catalogue, bool) {
	for _, c := range s.Catalogues() {
		if c.ID == id {
			return c, true
		}
	}
	return Catalogue{}, false
}

func (s *Store) CatalogueUpdate(c Catalogue) error {
	_, err := s.db.Exec(`UPDATE catalogues SET name=?,public=?,token=?,priority=?,sync_interval=? WHERE id=?`,
		c.Name, b2i(c.Public), c.Token, c.Priority, c.SyncInterval, c.ID)
	return err
}

// CatalogueRemove deletes a catalogue with its releases, sources and cached listings (the files on disk are the caller's to prune).
func (s *Store) CatalogueRemove(id string) error {
	for _, src := range s.CatSources(id) {
		if err := s.CatSourceRemove(src.ID); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec(`DELETE FROM cat_releases WHERE catalogue=?`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM catalogues WHERE id=?`, id)
	return err
}

func (s *Store) CatReleasePut(r CatRelease) error {
	if r.Created == 0 {
		r.Created = time.Now().Unix()
	}
	_, err := s.db.Exec(`INSERT INTO cat_releases(catalogue,app_id,version,data,created) VALUES(?,?,?,?,?)
		ON CONFLICT (catalogue,app_id,version) DO UPDATE SET data=excluded.data`, r.Catalogue, r.AppID, r.Version, r.Data, r.Created)
	return err
}

func (s *Store) CatReleases(catalogue string) []CatRelease {
	rows, err := s.db.Query(`SELECT app_id,version,data,created FROM cat_releases WHERE catalogue=? ORDER BY app_id,created,version`, catalogue)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []CatRelease{}
	for rows.Next() {
		r := CatRelease{Catalogue: catalogue}
		if rows.Scan(&r.AppID, &r.Version, &r.Data, &r.Created) == nil {
			out = append(out, r)
		}
	}
	return out
}

func (s *Store) CatReleaseDelete(catalogue, app, version string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM cat_releases WHERE catalogue=? AND app_id=? AND version=?`, catalogue, app, version)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) CatSourceAdd(c CatSource) (CatSource, error) {
	b := make([]byte, 6)
	rand.Read(b)
	c.ID = hex.EncodeToString(b)
	c.Created = time.Now().Unix()
	c.Enabled = true
	_, err := s.db.Exec(`INSERT INTO cat_sources(id,catalogue,name,url,token,priority,enabled,mirror,last_sync,last_error,created) VALUES(?,?,?,?,?,?,1,?,0,'',?)`,
		c.ID, c.Catalogue, c.Name, c.URL, c.Token, c.Priority, b2i(c.Mirror), c.Created)
	return c, err
}

// CatSources lists the sources of one catalogue ("" = of all).
func (s *Store) CatSources(catalogue string) []CatSource {
	q := `SELECT id,catalogue,name,url,COALESCE(token,''),priority,enabled,mirror,COALESCE(last_sync,0),COALESCE(last_error,''),created FROM cat_sources`
	args := []any{}
	if catalogue != "" {
		q += ` WHERE catalogue=?`
		args = append(args, catalogue)
	}
	rows, err := s.db.Query(q+` ORDER BY created,id`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []CatSource{}
	for rows.Next() {
		var c CatSource
		var en, mi int
		if rows.Scan(&c.ID, &c.Catalogue, &c.Name, &c.URL, &c.Token, &c.Priority, &en, &mi, &c.LastSync, &c.LastError, &c.Created) == nil {
			c.Enabled, c.Mirror = en != 0, mi != 0
			out = append(out, c)
		}
	}
	return out
}

func (s *Store) CatSource(id string) (CatSource, bool) {
	for _, c := range s.CatSources("") {
		if c.ID == id {
			return c, true
		}
	}
	return CatSource{}, false
}

func (s *Store) CatSourceUpdate(c CatSource) error {
	_, err := s.db.Exec(`UPDATE cat_sources SET name=?,url=?,token=?,priority=?,enabled=?,mirror=? WHERE id=?`,
		c.Name, c.URL, c.Token, c.Priority, b2i(c.Enabled), b2i(c.Mirror), c.ID)
	return err
}

func (s *Store) CatSourceStatus(id string, errText string) error {
	_, err := s.db.Exec(`UPDATE cat_sources SET last_sync=?,last_error=? WHERE id=?`, time.Now().Unix(), errText, id)
	return err
}

func (s *Store) CatSourceRemove(id string) error {
	for _, q := range []string{`DELETE FROM cat_source_items WHERE source=?`, `DELETE FROM cat_mirror WHERE source=?`, `DELETE FROM cat_sources WHERE id=?`} {
		if _, err := s.db.Exec(q, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CatSourceItems(source string) []CatSourceItem {
	rows, err := s.db.Query(`SELECT app_id,data,mirror FROM cat_source_items WHERE source=? ORDER BY app_id`, source)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []CatSourceItem{}
	for rows.Next() {
		var it CatSourceItem
		if rows.Scan(&it.AppID, &it.Data, &it.Mirror) == nil {
			out = append(out, it)
		}
	}
	return out
}

// CatSourceItemsReplace makes a source's stored listing exactly `items` (app id -> JSON); a per-app mirror override survives while the app stays listed.
func (s *Store) CatSourceItemsReplace(source string, items map[string]string) error {
	old := map[string]int{}
	for _, it := range s.CatSourceItems(source) {
		old[it.AppID] = it.Mirror
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM cat_source_items WHERE source=?`, source); err != nil {
		return err
	}
	for id, data := range items {
		m, ok := old[id]
		if !ok {
			m = -1
		}
		if _, err := tx.Exec(`INSERT INTO cat_source_items(source,app_id,data,mirror) VALUES(?,?,?,?)`, source, id, data, m); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) CatSourceItemMirror(source, app string, mirror int) (bool, error) {
	res, err := s.db.Exec(`UPDATE cat_source_items SET mirror=? WHERE source=? AND app_id=?`, mirror, source, app)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) CatMirrorSet(source, sha, state, errText string) error {
	_, err := s.db.Exec(`INSERT INTO cat_mirror(source,sha256,state,error,updated) VALUES(?,?,?,?,?)
		ON CONFLICT (source,sha256) DO UPDATE SET state=excluded.state,error=excluded.error,updated=excluded.updated`,
		source, sha, state, errText, time.Now().Unix())
	return err
}

func (s *Store) CatMirrors(source string) map[string]CatMirror {
	out := map[string]CatMirror{}
	rows, err := s.db.Query(`SELECT sha256,state,COALESCE(error,'') FROM cat_mirror WHERE source=?`, source)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var m CatMirror
		if rows.Scan(&m.SHA256, &m.State, &m.Error) == nil {
			out[m.SHA256] = m
		}
	}
	return out
}

// CatBlobReferenced says whether any hosted release or cached mirror still uses the file with this SHA-256 (hosted data names it in its JSON).
func (s *Store) CatBlobReferenced(sha string) bool {
	var n int
	if s.db.QueryRow(`SELECT COUNT(*) FROM cat_releases WHERE data LIKE ?`, "%"+sha+"%").Scan(&n) == nil && n > 0 {
		return true
	}
	return s.db.QueryRow(`SELECT COUNT(*) FROM cat_mirror WHERE sha256=? AND state='ok'`, sha).Scan(&n) == nil && n > 0
}
