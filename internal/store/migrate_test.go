// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"database/sql"
	"path/filepath"
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
