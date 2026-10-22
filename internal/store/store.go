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
		`CREATE TABLE IF NOT EXISTS pair_codes(code TEXT PRIMARY KEY, expires INTEGER NOT NULL, used INTEGER DEFAULT 0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			return nil, err
		}
	}
	return &Store{db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// NewPairCode makes a single-use short code (8 chars, 10 minutes).
func (s *Store) NewPairCode(ttl time.Duration) (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	c := make([]byte, 8)
	for i := range c {
		c[i] = alphabet[int(b[i])%len(alphabet)]
	}
	code := string(c[:4]) + "-" + string(c[4:])
	_, err := s.db.Exec(`INSERT INTO pair_codes(code,expires) VALUES(?,?)`, code, time.Now().Add(ttl).Unix())
	return code, err
}

var ErrBadCode = errors.New("invalid or expired pairing code")

// Pair consumes a code and creates a device with a fresh 32-byte key.
func (s *Store) Pair(code, name string) (id string, key []byte, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE pair_codes SET used=1 WHERE code=? AND used=0 AND expires>?`, code, time.Now().Unix())
	if err != nil {
		return "", nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return "", nil, ErrBadCode
	}
	ib := make([]byte, 8)
	key = make([]byte, 32)
	rand.Read(ib)
	rand.Read(key)
	id = hex.EncodeToString(ib)
	if _, err = tx.Exec(`INSERT INTO devices(id,name,key,created) VALUES(?,?,?,?)`, id, name, key, time.Now().Unix()); err != nil {
		return "", nil, err
	}
	return id, key, tx.Commit()
}

var ErrUnknown = errors.New("unknown or revoked device")

func (s *Store) Key(id string) ([]byte, error) {
	var key []byte
	var revoked int
	err := s.db.QueryRow(`SELECT key,revoked FROM devices WHERE id=?`, id).Scan(&key, &revoked)
	if err != nil || revoked != 0 {
		return nil, ErrUnknown
	}
	s.db.Exec(`UPDATE devices SET last_seen=? WHERE id=?`, time.Now().Unix(), id)
	return key, nil
}

func (s *Store) Revoke(id string) error {
	_, err := s.db.Exec(`UPDATE devices SET revoked=1 WHERE id=?`, id)
	return err
}

type Device struct {
	ID, Name      string
	Created, Seen int64
	Revoked       bool
}

func (s *Store) List() ([]Device, error) {
	rows, err := s.db.Query(`SELECT id,COALESCE(name,''),created,COALESCE(last_seen,0),revoked FROM devices ORDER BY created`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		var r int
		if err := rows.Scan(&d.ID, &d.Name, &d.Created, &d.Seen, &r); err != nil {
			return nil, err
		}
		d.Revoked = r != 0
		out = append(out, d)
	}
	return out, rows.Err()
}
