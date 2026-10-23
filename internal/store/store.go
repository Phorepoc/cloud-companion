// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package store keeps paired devices and one-shot pairing codes in SQLite.
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS devices(id TEXT PRIMARY KEY, name TEXT, key BLOB NOT NULL, created INTEGER, revoked INTEGER DEFAULT 0, last_seen INTEGER)`,
		`CREATE TABLE IF NOT EXISTS cookies(dev TEXT PRIMARY KEY, data BLOB, updated INTEGER)`,
		`CREATE TABLE IF NOT EXISTS settings(dev TEXT PRIMARY KEY, data BLOB)`,
		`CREATE TABLE IF NOT EXISTS pair_codes(code TEXT PRIMARY KEY, expires INTEGER NOT NULL, used INTEGER DEFAULT 0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			return nil, err
		}
	}
	db.Exec(`ALTER TABLE devices ADD COLUMN admin INTEGER DEFAULT 0`) // errors (column exists) are fine
	db.Exec(`ALTER TABLE pair_codes ADD COLUMN admin INTEGER DEFAULT 0`)
	st := &Store{db}
	if err := st.initPasskeys(); err != nil {
		return nil, err
	}
	if err := st.initConnectors(); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Store) Close() error { return s.db.Close() }

// NewPairCode makes a single-use short code (8 chars, 10 minutes).
func (s *Store) NewPairCode(ttl time.Duration, admin bool) (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	c := make([]byte, 8)
	for i := range c {
		c[i] = alphabet[int(b[i])%len(alphabet)]
	}
	code := string(c[:4]) + "-" + string(c[4:])
	a := 0
	if admin {
		a = 1
	}
	_, err := s.db.Exec(`INSERT INTO pair_codes(code,expires,admin) VALUES(?,?,?)`, code, time.Now().Add(ttl).Unix(), a)
	return code, err
}

var ErrBadCode = errors.New("invalid or expired pairing code")

// Pair consumes a code and creates a device with a fresh 32-byte key.
func (s *Store) Pair(code, name string) (id string, key []byte, admin bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", nil, false, err
	}
	defer tx.Rollback()
	var codeAdmin int
	if err := tx.QueryRow(`SELECT admin FROM pair_codes WHERE code=? AND used=0 AND expires>?`, code, time.Now().Unix()).Scan(&codeAdmin); err != nil {
		return "", nil, false, ErrBadCode
	}
	if _, err := tx.Exec(`UPDATE pair_codes SET used=1 WHERE code=?`, code); err != nil {
		return "", nil, false, err
	}
	var n int
	tx.QueryRow(`SELECT COUNT(*) FROM devices`).Scan(&n)
	admin = codeAdmin != 0 || n == 0 // the first paired device is the admin
	ib := make([]byte, 8)
	key = make([]byte, 32)
	rand.Read(ib)
	rand.Read(key)
	id = hex.EncodeToString(ib)
	a := 0
	if admin {
		a = 1
	}
	if _, err = tx.Exec(`INSERT INTO devices(id,name,key,created,admin) VALUES(?,?,?,?,?)`, id, name, key, time.Now().Unix(), a); err != nil {
		return "", nil, false, err
	}
	return id, key, admin, tx.Commit()
}

var ErrUnknown = errors.New("unknown or revoked device")

// Key returns the device key and whether the device is an admin (may list and revoke devices).
func (s *Store) Key(id string) ([]byte, bool, error) {
	var key []byte
	var revoked, admin int
	err := s.db.QueryRow(`SELECT key,revoked,admin FROM devices WHERE id=?`, id).Scan(&key, &revoked, &admin)
	if err != nil || revoked != 0 {
		return nil, false, ErrUnknown
	}
	s.db.Exec(`UPDATE devices SET last_seen=? WHERE id=?`, time.Now().Unix(), id)
	return key, admin != 0, nil
}

func (s *Store) Revoke(id string) error {
	_, err := s.db.Exec(`UPDATE devices SET revoked=1 WHERE id=?`, id)
	return err
}

type Device struct {
	ID, Name      string
	Created, Seen int64
	Revoked       bool
	Admin         bool
}

func (s *Store) List() ([]Device, error) {
	rows, err := s.db.Query(`SELECT id,COALESCE(name,''),created,COALESCE(last_seen,0),revoked,admin FROM devices ORDER BY created`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		var r, a int
		if err := rows.Scan(&d.ID, &d.Name, &d.Created, &d.Seen, &r, &a); err != nil {
			return nil, err
		}
		d.Revoked, d.Admin = r != 0, a != 0
		out = append(out, d)
	}
	return out, rows.Err()
}

// SaveCookies / LoadCookies keep one opaque cookie jar (JSON) per device.
func (s *Store) SaveCookies(dev string, data []byte) error {
	_, err := s.db.Exec(`INSERT INTO cookies(dev,data,updated) VALUES(?,?,?) ON CONFLICT(dev) DO UPDATE SET data=excluded.data,updated=excluded.updated`, dev, data, time.Now().Unix())
	return err
}

func (s *Store) LoadCookies(dev string) []byte {
	var b []byte
	s.db.QueryRow(`SELECT data FROM cookies WHERE dev=?`, dev).Scan(&b)
	return b
}

// SaveSettings / LoadSettings keep the per-device byte budget (JSON) so it survives reconnects.
func (s *Store) SaveSettings(dev string, data []byte) error {
	_, err := s.db.Exec(`INSERT INTO settings(dev,data) VALUES(?,?) ON CONFLICT(dev) DO UPDATE SET data=excluded.data`, dev, data)
	return err
}

func (s *Store) LoadSettings(dev string) []byte {
	var b []byte
	s.db.QueryRow(`SELECT data FROM settings WHERE dev=?`, dev).Scan(&b)
	return b
}

// DeviceName returns the name a device was paired with.
func (s *Store) DeviceName(id string) string {
	var n string
	s.db.QueryRow(`SELECT COALESCE(name,'') FROM devices WHERE id=?`, id).Scan(&n)
	return n
}

// Snapshot writes a consistent copy of the database to path (SQLite VACUUM INTO; safe while the server runs).
func (s *Store) Snapshot(path string) error {
	_, err := s.db.Exec(`VACUUM INTO ?`, path)
	return err
}
