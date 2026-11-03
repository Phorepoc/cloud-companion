// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateTwice(t *testing.T) {
	st := OpenTest(t)
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != len(migrations) {
		t.Fatalf("migrations recorded: %d %v", n, err)
	}
	if err := st.migrate(); err != nil { // a second run is a no-op
		t.Fatal(err)
	}
	if _, err := st.NewPairCode(60, true); err != nil {
		t.Fatal(err)
	}
}

// A SQLite file from before migrations existed (no schema_migrations, devices without the admin column) must open and keep its data.
func TestLegacySQLiteFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", "file:"+p)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE devices(id TEXT PRIMARY KEY, name TEXT, key BLOB NOT NULL, created INTEGER, revoked INTEGER DEFAULT 0, last_seen INTEGER)`,
		`CREATE TABLE pair_codes(code TEXT PRIMARY KEY, expires INTEGER NOT NULL, used INTEGER DEFAULT 0)`,
		`INSERT INTO devices(id,name,key,created) VALUES('dev1','Old phone',x'0102',1)`,
	} {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()
	st, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.DeviceName("dev1") != "Old phone" {
		t.Fatal("the old device is gone")
	}
	if _, _, err := st.Key("dev1"); err != nil {
		t.Fatal("the admin column was not added: ", err)
	}
}

func TestRewrite(t *testing.T) {
	q := `INSERT INTO synced(account,key,value) VALUES(?,?,?) ON CONFLICT(account,key) DO UPDATE SET value=excluded.value`
	if got := rewrite(Postgres, q); got != `INSERT INTO synced(account,key,value) VALUES($1,$2,$3) ON CONFLICT(account,key) DO UPDATE SET value=excluded.value` {
		t.Fatal(got)
	}
	if got := rewrite(MySQL, q); got != "INSERT INTO synced(account,`key`,value) VALUES(?,?,?) ON DUPLICATE KEY UPDATE value=VALUES(value)" {
		t.Fatal(got)
	}
	if got := rewrite(Postgres, `INSERT OR IGNORE INTO sms(a) VALUES(?)`); got != `INSERT INTO sms(a) VALUES($1) ON CONFLICT DO NOTHING` {
		t.Fatal(got)
	}
	if got := rewrite(MySQL, `INSERT OR IGNORE INTO sms(a) VALUES(?)`); got != `INSERT IGNORE INTO sms(a) VALUES(?)` {
		t.Fatal(got)
	}
	if got := rewrite(Postgres, `SELECT GROUP_CONCAT(DISTINCT device) FROM sms`); got != `SELECT string_agg(DISTINCT device, ',') FROM sms` {
		t.Fatal(got)
	}
}

// A database at schema version 3 with hosted releases opens, and the releases are in the library, published, untouched in the old table.
func TestMigration4MovesReleases(t *testing.T) {
	p := filepath.Join(t.TempDir(), "v3.db")
	old, err := sql.Open("sqlite", "file:"+p)
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, name VARCHAR(255), applied BIGINT)`,
		`INSERT INTO schema_migrations VALUES(1,'a',1),(2,'b',1),(3,'c',1)`,
		`CREATE TABLE catalogues(id TEXT PRIMARY KEY, name TEXT, public INTEGER DEFAULT 0, token TEXT, priority INTEGER DEFAULT 100, sync_interval INTEGER DEFAULT 21600, created INTEGER)`,
		`CREATE TABLE cat_releases(catalogue TEXT NOT NULL, app_id TEXT NOT NULL, version TEXT NOT NULL, data TEXT, created INTEGER, PRIMARY KEY(catalogue, app_id, version))`,
		`INSERT INTO catalogues VALUES('c1','C one',1,'',100,0,1)`,
		`INSERT INTO cat_releases VALUES('c1','app','1.0','{"file":{"sha256":"` + strings.Repeat("a", 64) + `","size":3},"icon":{"sha256":"` + strings.Repeat("b", 64) + `"}}',42)`,
	}
	for _, q := range stmts {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	old.Close()
	st, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	fs := st.CatFiles()
	if len(fs) != 1 || fs[0].Kind != "stored" || fs[0].SHA256 != strings.Repeat("a", 64) || fs[0].IconSHA != strings.Repeat("b", 64) || fs[0].Size != 3 {
		t.Fatalf("%+v", fs)
	}
	if pub := st.CatPublished("c1"); pub[fs[0].ID] != 42 {
		t.Fatalf("%v", pub)
	}
	if n := len(st.CatReleases("c1")); n != 1 {
		t.Fatalf("the old rows must stay: %d", n)
	}
}
