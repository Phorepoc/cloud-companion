// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// The single owner of the server: a WebAuthn user handle, any number of passkeys, and one recovery code (stored as a SHA-256 hash;
// the code itself has 80 bits of randomness and is shown once).

func (s *Store) initPasskeys() error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS owner(id INTEGER PRIMARY KEY CHECK(id=1), handle BLOB NOT NULL, recovery TEXT NOT NULL, created INTEGER)`,
		`CREATE TABLE IF NOT EXISTS passkeys(id BLOB PRIMARY KEY, cred TEXT NOT NULL, name TEXT, created INTEGER, last_used INTEGER)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// OwnerExists reports whether an owner passkey has been registered.
func (s *Store) OwnerExists() bool {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM owner`).Scan(&n)
	return n > 0
}

// OwnerHandle returns the WebAuthn user handle of the owner, creating it (and the recovery code hash) when absent.
func (s *Store) OwnerHandle() ([]byte, error) {
	var h []byte
	if err := s.db.QueryRow(`SELECT handle FROM owner WHERE id=1`).Scan(&h); err != nil {
		return nil, err
	}
	return h, nil
}

// NewHandle is a fresh 32-byte user handle for a registration in progress.
func NewHandle() []byte {
	h := make([]byte, 32)
	rand.Read(h)
	return h
}

const recAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// NewRecoveryCode makes a code like ABCD-EFGH-JKLM-NPQR (4 groups of 4, 20 symbols of 5 bits = 100 bits).
func NewRecoveryCode() string {
	b := make([]byte, 16)
	rand.Read(b)
	out := make([]byte, 0, 19)
	for i, x := range b {
		if i > 0 && i%4 == 0 {
			out = append(out, '-')
		}
		out = append(out, recAlphabet[int(x)%len(recAlphabet)])
	}
	return string(out)
}

func recHash(code string) string {
	c := make([]byte, 0, len(code))
	for i := 0; i < len(code); i++ {
		ch := code[i]
		if ch == '-' || ch == ' ' {
			continue
		}
		if ch >= 'a' && ch <= 'z' {
			ch -= 32
		}
		c = append(c, ch)
	}
	h := sha256.Sum256(c)
	return hex.EncodeToString(h[:])
}

// ConsumeAdminCode uses up an unexpired admin pairing code (the proof that the person at the server console allows a new owner passkey).
func (s *Store) ConsumeAdminCode(code string) error {
	res, err := s.db.Exec(`UPDATE pair_codes SET used=1 WHERE code=? AND used=0 AND expires>? AND admin=1`, code, time.Now().Unix())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrBadCode
	}
	return nil
}

// CheckAdminCode reports whether a code could be consumed (without consuming it).
func (s *Store) CheckAdminCode(code string) bool {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM pair_codes WHERE code=? AND used=0 AND expires>? AND admin=1`, code, time.Now().Unix()).Scan(&n)
	return n > 0
}

// AddPasskey stores a credential; the first one creates the owner (handle, recovery code) and returns the recovery code, later ones return "".
func (s *Store) AddPasskey(handle []byte, cred *webauthn.Credential, name string) (recovery string, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var n int
	tx.QueryRow(`SELECT COUNT(*) FROM owner`).Scan(&n)
	if n == 0 {
		recovery = NewRecoveryCode()
		if _, err = tx.Exec(`INSERT INTO owner(id,handle,recovery,created) VALUES(1,?,?,?)`, handle, recHash(recovery), time.Now().Unix()); err != nil {
			return "", err
		}
	}
	b, _ := json.Marshal(cred)
	if _, err = tx.Exec(`INSERT INTO passkeys(id,cred,name,created) VALUES(?,?,?,?)`, cred.ID, string(b), name, time.Now().Unix()); err != nil {
		return "", err
	}
	return recovery, tx.Commit()
}

// Passkeys returns the owner's credentials.
func (s *Store) Passkeys() []webauthn.Credential {
	rows, err := s.db.Query(`SELECT cred FROM passkeys ORDER BY created`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []webauthn.Credential
	for rows.Next() {
		var j string
		var c webauthn.Credential
		if rows.Scan(&j) == nil && json.Unmarshal([]byte(j), &c) == nil {
			out = append(out, c)
		}
	}
	return out
}

// PasskeyUsed stores the updated sign counter after a login.
func (s *Store) PasskeyUsed(cred *webauthn.Credential) {
	b, _ := json.Marshal(cred)
	s.db.Exec(`UPDATE passkeys SET cred=?, last_used=? WHERE id=?`, string(b), time.Now().Unix(), cred.ID)
}

// UseRecoveryCode checks the code and replaces it by a new one (returned); the old one stops working.
func (s *Store) UseRecoveryCode(code string) (newCode string, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var have string
	if err := tx.QueryRow(`SELECT recovery FROM owner WHERE id=1`).Scan(&have); err != nil {
		return "", ErrBadCode
	}
	if subtle.ConstantTimeCompare([]byte(have), []byte(recHash(code))) != 1 {
		return "", ErrBadCode
	}
	newCode = NewRecoveryCode()
	if _, err := tx.Exec(`UPDATE owner SET recovery=? WHERE id=1`, recHash(newCode)); err != nil {
		return "", err
	}
	return newCode, tx.Commit()
}

// NewDevice creates a device (a web browser that signed in) without a pairing code.
func (s *Store) NewDevice(name string, admin bool) (id string, key []byte, err error) {
	ib := make([]byte, 8)
	key = make([]byte, 32)
	rand.Read(ib)
	rand.Read(key)
	id = hex.EncodeToString(ib)
	a := 0
	if admin {
		a = 1
	}
	_, err = s.db.Exec(`INSERT INTO devices(id,name,key,created,admin) VALUES(?,?,?,?,?)`, id, name, key, time.Now().Unix(), a)
	return id, key, err
}

var ErrNoOwner = errors.New("no owner")
