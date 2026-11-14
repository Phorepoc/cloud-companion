// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// The catalogue library (docs/catalogue.md, "The library"): one row per package file found in the library folder (kind "library") or stored by an
// earlier version of the catalogue (kind "stored", migration 4). cat_publish says which catalogues offer which file.

type CatFile struct {
	ID      string `json:"id"`
	Path    string `json:"path"` // relative to the library folder, with "/"; "" for kind stored
	Kind    string `json:"kind"` // library | stored
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	MTime   int64  `json:"mtime"`  // unix nanoseconds of the last scan that saw the file
	Status  string `json:"status"` // ok | missing | unreadable
	Error   string `json:"error"`
	Meta    string `json:"-"` // JSON: what the scanner read from the file
	Edits   string `json:"-"` // JSON: what the owner changed
	Legacy  string `json:"-"` // JSON: the whole release of a stored entry
	IconSHA string `json:"icon_sha"`
	Added   int64  `json:"added"`
	Scanned int64  `json:"scanned"`
	Changed int64  `json:"changed"`
}

const catFileCols = `id,path,kind,sha256,size,mtime,status,COALESCE(error,''),COALESCE(meta,''),COALESCE(edits,''),COALESCE(legacy,''),COALESCE(icon_sha,''),added,scanned,changed`

func (s *Store) CatFilePut(f CatFile) error {
	_, err := s.db.Exec(`INSERT INTO cat_library(id,path,kind,sha256,size,mtime,status,error,meta,edits,legacy,icon_sha,added,scanned,changed) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (id) DO UPDATE SET path=excluded.path,kind=excluded.kind,sha256=excluded.sha256,size=excluded.size,mtime=excluded.mtime,status=excluded.status,
		error=excluded.error,meta=excluded.meta,edits=excluded.edits,legacy=excluded.legacy,icon_sha=excluded.icon_sha,scanned=excluded.scanned,changed=excluded.changed`,
		f.ID, f.Path, f.Kind, f.SHA256, f.Size, f.MTime, f.Status, f.Error, f.Meta, f.Edits, f.Legacy, f.IconSHA, f.Added, f.Scanned, f.Changed)
	return err
}

func (s *Store) CatFiles() []CatFile {
	rows, err := s.db.Query(`SELECT ` + catFileCols + ` FROM cat_library ORDER BY path,id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []CatFile{}
	for rows.Next() {
		var f CatFile
		if rows.Scan(&f.ID, &f.Path, &f.Kind, &f.SHA256, &f.Size, &f.MTime, &f.Status, &f.Error, &f.Meta, &f.Edits, &f.Legacy, &f.IconSHA, &f.Added, &f.Scanned, &f.Changed) == nil {
			out = append(out, f)
		}
	}
	return out
}

func (s *Store) CatFile(id string) (CatFile, bool) {
	for _, f := range s.CatFiles() {
		if f.ID == id {
			return f, true
		}
	}
	return CatFile{}, false
}

// CatFileDelete removes the entry and every publication of it.
func (s *Store) CatFileDelete(id string) error {
	if _, err := s.db.Exec(`DELETE FROM cat_publish WHERE file_id=?`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM cat_library WHERE id=?`, id)
	return err
}

// CatPublishSet publishes (on) or unpublishes a file in a catalogue; it returns whether anything changed.
func (s *Store) CatPublishSet(catalogue, file string, on bool, created int64) (bool, error) {
	if !on {
		res, err := s.db.Exec(`DELETE FROM cat_publish WHERE catalogue=? AND file_id=?`, catalogue, file)
		if err != nil {
			return false, err
		}
		n, _ := res.RowsAffected()
		return n > 0, nil
	}
	if created == 0 {
		created = time.Now().Unix()
	}
	res, err := s.db.Exec(`INSERT OR IGNORE INTO cat_publish(catalogue,file_id,created) VALUES(?,?,?)`, catalogue, file, created)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CatUnpublishEverywhere unpublishes a file in all catalogues and returns where it was published.
func (s *Store) CatUnpublishEverywhere(file string) []string {
	cats := s.CatPublishedIn(file)
	s.db.Exec(`DELETE FROM cat_publish WHERE file_id=?`, file)
	return cats
}

// CatPublished maps file id -> time it was published, for one catalogue.
func (s *Store) CatPublished(catalogue string) map[string]int64 {
	out := map[string]int64{}
	rows, err := s.db.Query(`SELECT file_id,created FROM cat_publish WHERE catalogue=?`, catalogue)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var c int64
		if rows.Scan(&id, &c) == nil {
			out[id] = c
		}
	}
	return out
}

func (s *Store) CatPublishedIn(file string) []string {
	out := []string{}
	rows, err := s.db.Query(`SELECT catalogue FROM cat_publish WHERE file_id=? ORDER BY catalogue`, file)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil {
			out = append(out, c)
		}
	}
	return out
}

// CatFileBySHA finds a present library file with this content hash (to serve it).
func (s *Store) CatFileBySHA(sha string) (CatFile, bool) {
	rows, err := s.db.Query(`SELECT `+catFileCols+` FROM cat_library WHERE sha256=? AND kind='library' AND status<>'missing'`, sha)
	if err != nil {
		return CatFile{}, false
	}
	defer rows.Close()
	for rows.Next() {
		var f CatFile
		if rows.Scan(&f.ID, &f.Path, &f.Kind, &f.SHA256, &f.Size, &f.MTime, &f.Status, &f.Error, &f.Meta, &f.Edits, &f.Legacy, &f.IconSHA, &f.Added, &f.Scanned, &f.Changed) == nil {
			return f, true
		}
	}
	return CatFile{}, false
}

// migrateReleases (migration 4) moves every hosted release of the old model into the library as a "stored" entry, published in its catalogue. The
// old cat_releases rows stay in place, unused, as a copy.
func migrateReleases(tx *Tx) error {
	rows, err := tx.Query(`SELECT catalogue,app_id,version,data,created FROM cat_releases`)
	if err != nil {
		return err
	}
	type row struct {
		cat, app, ver, data string
		created             int64
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.cat, &r.app, &r.ver, &r.data, &r.created); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
	}
	rows.Close()
	now := time.Now().Unix()
	for _, r := range all {
		var rel struct {
			File struct {
				SHA256 string `json:"sha256"`
				Size   int64  `json:"size"`
			} `json:"file"`
			Icon *struct {
				SHA256 string `json:"sha256"`
			} `json:"icon"`
		}
		json.Unmarshal([]byte(r.data), &rel)
		h := sha256.Sum256([]byte(r.cat + "/" + r.app + "/" + r.ver))
		id := hex.EncodeToString(h[:8])
		icon := ""
		if rel.Icon != nil {
			icon = rel.Icon.SHA256
		}
		if _, err := tx.Exec(`INSERT INTO cat_library(id,path,kind,sha256,size,mtime,status,error,meta,edits,legacy,icon_sha,added,scanned,changed) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, "", "stored", rel.File.SHA256, rel.File.Size, 0, "ok", "", "", "", r.data, icon, r.created, now, r.created); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO cat_publish(catalogue,file_id,created) VALUES(?,?,?)`, r.cat, id, r.created); err != nil {
			return err
		}
	}
	return nil
}

// TestRunMigration4 inserts legacy cat_releases rows (catalogue|app|version -> JSON) and runs the data move of migration 4 again; for tests of the
// migration (the real one runs once, at open).
func (s *Store) TestRunMigration4(rows map[string]string) error {
	for k, data := range rows {
		p := splitKey(k)
		if _, err := s.db.Exec(`INSERT INTO cat_releases(catalogue,app_id,version,data,created) VALUES(?,?,?,?,?)`, p[0], p[1], p[2], data, 5); err != nil {
			return err
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := migrateReleases(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func splitKey(k string) [3]string {
	var out [3]string
	n := 0
	cur := ""
	for _, r := range k {
		if r == '|' && n < 2 {
			out[n] = cur
			cur = ""
			n++
			continue
		}
		cur += string(r)
	}
	out[n] = cur
	return out
}
