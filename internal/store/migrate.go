// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package store

// Versioned schema migrations, one list per dialect. `schema_migrations` records the versions that ran; Open applies the missing ones in order,
// each in its own transaction where the database allows it (MySQL commits DDL implicitly). To change the schema, APPEND a migration to all
// three lists (never edit a released one). An existing SQLite file from before migrations existed already has every table of version 1:
// its statements are IF NOT EXISTS and its two ALTERs tolerate "column exists", so version 1 simply runs and is recorded.

import (
	"fmt"
	"strings"
)

type migration struct {
	version int
	name    string
	stmts   map[Dialect][]string
	// tolerate lists SQLite statements whose failure is fine (ALTER TABLE ADD COLUMN on a database that has the column)
	tolerate bool
	// after runs in the same transaction once the statements have run (data moves SQL alone cannot do)
	after func(*Tx) error
}

var migrations = []migration{
	{version: 1, name: "base schema: devices, pairing, owner and passkeys, connected accounts, SMS, merges", tolerate: true, stmts: map[Dialect][]string{
		SQLite: {
			`CREATE TABLE IF NOT EXISTS devices(id TEXT PRIMARY KEY, name TEXT, key BLOB NOT NULL, created INTEGER, revoked INTEGER DEFAULT 0, last_seen INTEGER)`,
			`CREATE TABLE IF NOT EXISTS cookies(dev TEXT PRIMARY KEY, data BLOB, updated INTEGER)`,
			`CREATE TABLE IF NOT EXISTS settings(dev TEXT PRIMARY KEY, data BLOB)`,
			`CREATE TABLE IF NOT EXISTS pair_codes(code TEXT PRIMARY KEY, expires INTEGER NOT NULL, used INTEGER DEFAULT 0)`,
			`ALTER TABLE devices ADD COLUMN admin INTEGER DEFAULT 0`,
			`ALTER TABLE pair_codes ADD COLUMN admin INTEGER DEFAULT 0`,
			`CREATE TABLE IF NOT EXISTS owner(id INTEGER PRIMARY KEY CHECK(id=1), handle BLOB NOT NULL, recovery TEXT NOT NULL, created INTEGER)`,
			`CREATE TABLE IF NOT EXISTS passkeys(id BLOB PRIMARY KEY, cred TEXT NOT NULL, name TEXT, created INTEGER, last_used INTEGER)`,
			`CREATE TABLE IF NOT EXISTS accounts(id TEXT PRIMARY KEY, kind TEXT NOT NULL, provider TEXT, name TEXT, config TEXT, secret BLOB, status TEXT, last_sync INTEGER, last_error TEXT, created INTEGER)`,
			`CREATE TABLE IF NOT EXISTS synced(account TEXT NOT NULL, key TEXT NOT NULL, value TEXT, PRIMARY KEY(account, key))`,
			`CREATE TABLE IF NOT EXISTS contact_links(hub_id TEXT NOT NULL, source TEXT NOT NULL, entry_id TEXT NOT NULL, created INTEGER, PRIMARY KEY(source, entry_id))`,
			`CREATE INDEX IF NOT EXISTS contact_links_hub ON contact_links(hub_id)`,
			`CREATE TABLE IF NOT EXISTS sms(id INTEGER PRIMARY KEY AUTOINCREMENT, device TEXT NOT NULL, dev_msg_id TEXT, number TEXT NOT NULL, key TEXT NOT NULL, dir TEXT NOT NULL, body TEXT, ts INTEGER, status TEXT, error TEXT)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS sms_dev ON sms(device, dev_msg_id) WHERE dev_msg_id IS NOT NULL AND dev_msg_id != ''`,
			`CREATE INDEX IF NOT EXISTS sms_key ON sms(key, ts)`,
			`CREATE TABLE IF NOT EXISTS sms_devices(device TEXT PRIMARY KEY, label TEXT, number TEXT, last_sync INTEGER)`,
			`CREATE TABLE IF NOT EXISTS dev_merges(id TEXT PRIMARY KEY, account TEXT, a_href TEXT, a_body TEXT, a_etag TEXT, b_href TEXT, b_body TEXT, b_etag TEXT, merged_body TEXT, merged_etag TEXT, state TEXT, expires INTEGER)`,
		},
		Postgres: {
			`CREATE TABLE IF NOT EXISTS devices(id TEXT PRIMARY KEY, name TEXT, key BYTEA NOT NULL, created BIGINT, revoked BIGINT DEFAULT 0, last_seen BIGINT, admin BIGINT DEFAULT 0)`,
			`CREATE TABLE IF NOT EXISTS cookies(dev TEXT PRIMARY KEY, data BYTEA, updated BIGINT)`,
			`CREATE TABLE IF NOT EXISTS settings(dev TEXT PRIMARY KEY, data BYTEA)`,
			`CREATE TABLE IF NOT EXISTS pair_codes(code TEXT PRIMARY KEY, expires BIGINT NOT NULL, used BIGINT DEFAULT 0, admin BIGINT DEFAULT 0)`,
			`CREATE TABLE IF NOT EXISTS owner(id INTEGER PRIMARY KEY CHECK(id=1), handle BYTEA NOT NULL, recovery TEXT NOT NULL, created BIGINT)`,
			`CREATE TABLE IF NOT EXISTS passkeys(id BYTEA PRIMARY KEY, cred TEXT NOT NULL, name TEXT, created BIGINT, last_used BIGINT)`,
			`CREATE TABLE IF NOT EXISTS accounts(id TEXT PRIMARY KEY, kind TEXT NOT NULL, provider TEXT, name TEXT, config TEXT, secret BYTEA, status TEXT, last_sync BIGINT, last_error TEXT, created BIGINT)`,
			`CREATE TABLE IF NOT EXISTS synced(account TEXT NOT NULL, key TEXT NOT NULL, value TEXT, PRIMARY KEY(account, key))`,
			`CREATE TABLE IF NOT EXISTS contact_links(hub_id TEXT NOT NULL, source TEXT NOT NULL, entry_id TEXT NOT NULL, created BIGINT, PRIMARY KEY(source, entry_id))`,
			`CREATE INDEX IF NOT EXISTS contact_links_hub ON contact_links(hub_id)`,
			`CREATE TABLE IF NOT EXISTS sms(id BIGSERIAL PRIMARY KEY, device TEXT NOT NULL, dev_msg_id TEXT, number TEXT NOT NULL, key TEXT NOT NULL, dir TEXT NOT NULL, body TEXT, ts BIGINT, status TEXT, error TEXT)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS sms_dev ON sms(device, dev_msg_id) WHERE dev_msg_id IS NOT NULL AND dev_msg_id != ''`,
			`CREATE INDEX IF NOT EXISTS sms_key ON sms(key, ts)`,
			`CREATE TABLE IF NOT EXISTS sms_devices(device TEXT PRIMARY KEY, label TEXT, number TEXT, last_sync BIGINT)`,
			`CREATE TABLE IF NOT EXISTS dev_merges(id TEXT PRIMARY KEY, account TEXT, a_href TEXT, a_body TEXT, a_etag TEXT, b_href TEXT, b_body TEXT, b_etag TEXT, merged_body TEXT, merged_etag TEXT, state TEXT, expires BIGINT)`,
		},
		MySQL: {
			`CREATE TABLE IF NOT EXISTS devices(id VARCHAR(64) PRIMARY KEY, name VARCHAR(255), ` + "`key`" + ` BLOB NOT NULL, created BIGINT, revoked BIGINT DEFAULT 0, last_seen BIGINT, admin BIGINT DEFAULT 0)`,
			`CREATE TABLE IF NOT EXISTS cookies(dev VARCHAR(64) PRIMARY KEY, data LONGBLOB, updated BIGINT)`,
			`CREATE TABLE IF NOT EXISTS settings(dev VARCHAR(64) PRIMARY KEY, data LONGBLOB)`,
			`CREATE TABLE IF NOT EXISTS pair_codes(code VARCHAR(32) PRIMARY KEY, expires BIGINT NOT NULL, used BIGINT DEFAULT 0, admin BIGINT DEFAULT 0)`,
			`CREATE TABLE IF NOT EXISTS owner(id INT PRIMARY KEY, handle BLOB NOT NULL, recovery VARCHAR(128) NOT NULL, created BIGINT)`,
			`CREATE TABLE IF NOT EXISTS passkeys(id VARBINARY(255) PRIMARY KEY, cred TEXT NOT NULL, name VARCHAR(255), created BIGINT, last_used BIGINT)`,
			`CREATE TABLE IF NOT EXISTS accounts(id VARCHAR(32) PRIMARY KEY, kind VARCHAR(16) NOT NULL, provider VARCHAR(64), name VARCHAR(255), config TEXT, secret BLOB, status VARCHAR(16), last_sync BIGINT, last_error TEXT, created BIGINT)`,
			`CREATE TABLE IF NOT EXISTS synced(account VARCHAR(100) NOT NULL, ` + "`key`" + ` VARCHAR(255) NOT NULL, value TEXT, PRIMARY KEY(account, ` + "`key`" + `))`,
			`CREATE TABLE IF NOT EXISTS contact_links(hub_id VARCHAR(255) NOT NULL, source VARCHAR(100) NOT NULL, entry_id VARCHAR(400) NOT NULL, created BIGINT, PRIMARY KEY(source, entry_id), INDEX contact_links_hub(hub_id))`,
			`CREATE TABLE IF NOT EXISTS sms(id BIGINT AUTO_INCREMENT PRIMARY KEY, device VARCHAR(100) NOT NULL, dev_msg_id VARCHAR(190), number VARCHAR(100) NOT NULL, ` + "`key`" + ` VARCHAR(100) NOT NULL, dir VARCHAR(4) NOT NULL, body LONGTEXT, ts BIGINT, status VARCHAR(16), error TEXT, UNIQUE INDEX sms_dev(device, dev_msg_id), INDEX sms_key(` + "`key`" + `, ts))`,
			`CREATE TABLE IF NOT EXISTS sms_devices(device VARCHAR(100) PRIMARY KEY, label VARCHAR(255), number VARCHAR(100), last_sync BIGINT)`,
			`CREATE TABLE IF NOT EXISTS dev_merges(id VARCHAR(32) PRIMARY KEY, account VARCHAR(32), a_href TEXT, a_body LONGTEXT, a_etag TEXT, b_href TEXT, b_body LONGTEXT, b_etag TEXT, merged_body LONGTEXT, merged_etag TEXT, state VARCHAR(16), expires BIGINT)`,
		},
	}},
	{version: 2, name: "self-hosted catalogues: catalogues, releases, federated sources, cached listings, mirrored files", stmts: map[Dialect][]string{
		SQLite: {
			`CREATE TABLE IF NOT EXISTS catalogues(id TEXT PRIMARY KEY, name TEXT, public INTEGER DEFAULT 0, token TEXT, priority INTEGER DEFAULT 100, sync_interval INTEGER DEFAULT 21600, created INTEGER)`,
			`CREATE TABLE IF NOT EXISTS cat_releases(catalogue TEXT NOT NULL, app_id TEXT NOT NULL, version TEXT NOT NULL, data TEXT, created INTEGER, PRIMARY KEY(catalogue, app_id, version))`,
			`CREATE TABLE IF NOT EXISTS cat_sources(id TEXT PRIMARY KEY, catalogue TEXT NOT NULL, name TEXT, url TEXT NOT NULL, token TEXT, priority INTEGER DEFAULT 50, enabled INTEGER DEFAULT 1, mirror INTEGER DEFAULT 0, last_sync INTEGER, last_error TEXT, created INTEGER)`,
			`CREATE TABLE IF NOT EXISTS cat_source_items(source TEXT NOT NULL, app_id TEXT NOT NULL, data TEXT, mirror INTEGER DEFAULT -1, PRIMARY KEY(source, app_id))`,
			`CREATE TABLE IF NOT EXISTS cat_mirror(source TEXT NOT NULL, sha256 TEXT NOT NULL, state TEXT, error TEXT, updated INTEGER, PRIMARY KEY(source, sha256))`,
		},
		Postgres: {
			`CREATE TABLE IF NOT EXISTS catalogues(id TEXT PRIMARY KEY, name TEXT, public BIGINT DEFAULT 0, token TEXT, priority BIGINT DEFAULT 100, sync_interval BIGINT DEFAULT 21600, created BIGINT)`,
			`CREATE TABLE IF NOT EXISTS cat_releases(catalogue TEXT NOT NULL, app_id TEXT NOT NULL, version TEXT NOT NULL, data TEXT, created BIGINT, PRIMARY KEY(catalogue, app_id, version))`,
			`CREATE TABLE IF NOT EXISTS cat_sources(id TEXT PRIMARY KEY, catalogue TEXT NOT NULL, name TEXT, url TEXT NOT NULL, token TEXT, priority BIGINT DEFAULT 50, enabled BIGINT DEFAULT 1, mirror BIGINT DEFAULT 0, last_sync BIGINT, last_error TEXT, created BIGINT)`,
			`CREATE TABLE IF NOT EXISTS cat_source_items(source TEXT NOT NULL, app_id TEXT NOT NULL, data TEXT, mirror BIGINT DEFAULT -1, PRIMARY KEY(source, app_id))`,
			`CREATE TABLE IF NOT EXISTS cat_mirror(source TEXT NOT NULL, sha256 TEXT NOT NULL, state TEXT, error TEXT, updated BIGINT, PRIMARY KEY(source, sha256))`,
		},
		MySQL: {
			`CREATE TABLE IF NOT EXISTS catalogues(id VARCHAR(64) PRIMARY KEY, name VARCHAR(255), public BIGINT DEFAULT 0, token VARCHAR(100), priority BIGINT DEFAULT 100, sync_interval BIGINT DEFAULT 21600, created BIGINT)`,
			`CREATE TABLE IF NOT EXISTS cat_releases(catalogue VARCHAR(64) NOT NULL, app_id VARCHAR(64) NOT NULL, version VARCHAR(100) NOT NULL, data LONGTEXT, created BIGINT, PRIMARY KEY(catalogue, app_id, version))`,
			`CREATE TABLE IF NOT EXISTS cat_sources(id VARCHAR(32) PRIMARY KEY, catalogue VARCHAR(64) NOT NULL, name VARCHAR(255), url TEXT NOT NULL, token TEXT, priority BIGINT DEFAULT 50, enabled BIGINT DEFAULT 1, mirror BIGINT DEFAULT 0, last_sync BIGINT, last_error TEXT, created BIGINT)`,
			`CREATE TABLE IF NOT EXISTS cat_source_items(source VARCHAR(32) NOT NULL, app_id VARCHAR(64) NOT NULL, data LONGTEXT, mirror BIGINT DEFAULT -1, PRIMARY KEY(source, app_id))`,
			`CREATE TABLE IF NOT EXISTS cat_mirror(source VARCHAR(32) NOT NULL, sha256 VARCHAR(64) NOT NULL, state VARCHAR(16), error TEXT, updated BIGINT, PRIMARY KEY(source, sha256))`,
		},
	}},
	{version: 3, name: "server settings: one value per name (weather source, units, default place)", stmts: map[Dialect][]string{
		SQLite:   {`CREATE TABLE IF NOT EXISTS server_settings(name TEXT PRIMARY KEY, value TEXT)`},
		Postgres: {`CREATE TABLE IF NOT EXISTS server_settings(name TEXT PRIMARY KEY, value TEXT)`},
		MySQL:    {`CREATE TABLE IF NOT EXISTS server_settings(name VARCHAR(100) PRIMARY KEY, value LONGTEXT)`},
	}},
	{version: 4, name: "catalogue library: scanned package files and per-catalogue publishing; hosted releases move into it", after: migrateReleases, stmts: map[Dialect][]string{
		SQLite: {
			`CREATE TABLE IF NOT EXISTS cat_library(id TEXT PRIMARY KEY, path TEXT NOT NULL, kind TEXT NOT NULL, sha256 TEXT, size INTEGER, mtime INTEGER, status TEXT, error TEXT, meta TEXT, edits TEXT, legacy TEXT, icon_sha TEXT, added INTEGER, scanned INTEGER, changed INTEGER)`,
			`CREATE TABLE IF NOT EXISTS cat_publish(catalogue TEXT NOT NULL, file_id TEXT NOT NULL, created INTEGER, PRIMARY KEY(catalogue, file_id))`,
		},
		Postgres: {
			`CREATE TABLE IF NOT EXISTS cat_library(id TEXT PRIMARY KEY, path TEXT NOT NULL, kind TEXT NOT NULL, sha256 TEXT, size BIGINT, mtime BIGINT, status TEXT, error TEXT, meta TEXT, edits TEXT, legacy TEXT, icon_sha TEXT, added BIGINT, scanned BIGINT, changed BIGINT)`,
			`CREATE TABLE IF NOT EXISTS cat_publish(catalogue TEXT NOT NULL, file_id TEXT NOT NULL, created BIGINT, PRIMARY KEY(catalogue, file_id))`,
		},
		MySQL: {
			`CREATE TABLE IF NOT EXISTS cat_library(id VARCHAR(32) PRIMARY KEY, path VARCHAR(1024) NOT NULL, kind VARCHAR(16) NOT NULL, sha256 VARCHAR(64), size BIGINT, mtime BIGINT, status VARCHAR(16), error TEXT, meta LONGTEXT, edits LONGTEXT, legacy LONGTEXT, icon_sha VARCHAR(64), added BIGINT, scanned BIGINT, changed BIGINT)`,
			`CREATE TABLE IF NOT EXISTS cat_publish(catalogue VARCHAR(64) NOT NULL, file_id VARCHAR(32) NOT NULL, created BIGINT, PRIMARY KEY(catalogue, file_id))`,
		},
	}},
	{version: 5, name: "first-owner code: one hashed code that registers the first owner while the server is uninitialised", stmts: map[Dialect][]string{
		SQLite:   {`CREATE TABLE IF NOT EXISTS first_owner_code(id INTEGER PRIMARY KEY, salt TEXT NOT NULL, hash TEXT NOT NULL, created INTEGER, from_env INTEGER DEFAULT 0)`},
		Postgres: {`CREATE TABLE IF NOT EXISTS first_owner_code(id BIGINT PRIMARY KEY, salt TEXT NOT NULL, hash TEXT NOT NULL, created BIGINT, from_env BIGINT DEFAULT 0)`},
		MySQL:    {`CREATE TABLE IF NOT EXISTS first_owner_code(id BIGINT PRIMARY KEY, salt VARCHAR(64) NOT NULL, hash VARCHAR(64) NOT NULL, created BIGINT, from_env BIGINT DEFAULT 0)`},
	}},
	{version: 6, name: "chat connections: SIP logins (digest HA1 only), one inbox for every connection, the routing rules and who gets which connection", stmts: map[Dialect][]string{
		SQLite: {
			`CREATE TABLE IF NOT EXISTS sip_users(name TEXT PRIMARY KEY, ha1 TEXT NOT NULL, created INTEGER, inbox_cursor INTEGER DEFAULT 0)`,
			`CREATE TABLE IF NOT EXISTS chat_inbox(id INTEGER PRIMARY KEY AUTOINCREMENT, conn TEXT NOT NULL, peer TEXT NOT NULL, peer_name TEXT, to_login TEXT, body TEXT, ts INTEGER)`,
			`CREATE TABLE IF NOT EXISTS chat_routes(id INTEGER PRIMARY KEY AUTOINCREMENT, position INTEGER NOT NULL, match_kind TEXT NOT NULL, match_value TEXT, conn TEXT NOT NULL, opt TEXT)`,
			`CREATE TABLE IF NOT EXISTS chat_deliver(conn TEXT NOT NULL, login TEXT NOT NULL, PRIMARY KEY(conn, login))`,
		},
		Postgres: {
			`CREATE TABLE IF NOT EXISTS sip_users(name TEXT PRIMARY KEY, ha1 TEXT NOT NULL, created BIGINT, inbox_cursor BIGINT DEFAULT 0)`,
			`CREATE TABLE IF NOT EXISTS chat_inbox(id BIGSERIAL PRIMARY KEY, conn TEXT NOT NULL, peer TEXT NOT NULL, peer_name TEXT, to_login TEXT, body TEXT, ts BIGINT)`,
			`CREATE TABLE IF NOT EXISTS chat_routes(id BIGSERIAL PRIMARY KEY, position BIGINT NOT NULL, match_kind TEXT NOT NULL, match_value TEXT, conn TEXT NOT NULL, opt TEXT)`,
			`CREATE TABLE IF NOT EXISTS chat_deliver(conn TEXT NOT NULL, login TEXT NOT NULL, PRIMARY KEY(conn, login))`,
		},
		MySQL: {
			`CREATE TABLE IF NOT EXISTS sip_users(name VARCHAR(64) PRIMARY KEY, ha1 VARCHAR(64) NOT NULL, created BIGINT, inbox_cursor BIGINT DEFAULT 0)`,
			`CREATE TABLE IF NOT EXISTS chat_inbox(id BIGINT AUTO_INCREMENT PRIMARY KEY, conn VARCHAR(64) NOT NULL, peer VARCHAR(255) NOT NULL, peer_name VARCHAR(255), to_login VARCHAR(64), body LONGTEXT, ts BIGINT)`,
			`CREATE TABLE IF NOT EXISTS chat_routes(id BIGINT AUTO_INCREMENT PRIMARY KEY, position BIGINT NOT NULL, match_kind VARCHAR(16) NOT NULL, match_value VARCHAR(255), conn VARCHAR(64) NOT NULL, opt VARCHAR(255))`,
			`CREATE TABLE IF NOT EXISTS chat_deliver(conn VARCHAR(64) NOT NULL, login VARCHAR(64) NOT NULL, PRIMARY KEY(conn, login))`,
		},
	}},
}

// SchemaVersion is the newest migration this build knows.
func SchemaVersion() int { return migrations[len(migrations)-1].version }

func (s *Store) migrate() error {
	ver := map[Dialect]string{SQLite: "INTEGER", Postgres: "INTEGER", MySQL: "INT"}[s.d]
	if _, err := s.db.DB.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations(version ` + ver + ` PRIMARY KEY, name VARCHAR(255), applied BIGINT)`); err != nil {
		return err
	}
	for _, m := range migrations {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=?`, m.version).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		for _, q := range m.stmts[s.d] {
			if _, err := tx.Tx.Exec(q); err != nil { // DDL is already written for the dialect: no rewriting
				if s.d == SQLite && m.tolerate && strings.HasPrefix(q, "ALTER TABLE") {
					continue // the column is already there (a database from before migrations)
				}
				tx.Rollback()
				return fmt.Errorf("migration %d (%s): %w", m.version, m.name, err)
			}
		}
		if m.after != nil {
			if err := m.after(tx); err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %d (%s): %w", m.version, m.name, err)
			}
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version,name,applied) VALUES(?,?,?)`, m.version, m.name, nowUnix()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
