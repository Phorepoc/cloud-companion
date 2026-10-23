// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Connected outside accounts. Stalwart is only the local cache: these rows say what is pulled from where. The credentials are
// sealed (AES-GCM, internal/connectors) before they reach this table; the store never sees them in the clear.

type Account struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`     // imap | carddav | caldav
	Provider  string `json:"provider"` // generic | gmail | outlook | icloud | nextcloud ...
	Name      string `json:"name"`
	Config    string `json:"config"` // JSON, no secrets (host, port, user, folders, months, smtp host ...)
	Secret    []byte `json:"-"`      // sealed password / token
	Status    string `json:"status"` // new | ok | error | syncing
	LastSync  int64  `json:"last_sync"`
	LastError string `json:"last_error"`
	Created   int64  `json:"created"`
}

func (s *Store) initConnectors() error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS accounts(id TEXT PRIMARY KEY, kind TEXT NOT NULL, provider TEXT, name TEXT, config TEXT, secret BLOB, status TEXT, last_sync INTEGER, last_error TEXT, created INTEGER)`,
		`CREATE TABLE IF NOT EXISTS synced(account TEXT NOT NULL, key TEXT NOT NULL, value TEXT, PRIMARY KEY(account, key))`,
		`CREATE TABLE IF NOT EXISTS contact_links(hub_id TEXT NOT NULL, source TEXT NOT NULL, entry_id TEXT NOT NULL, created INTEGER, PRIMARY KEY(source, entry_id))`,
		`CREATE INDEX IF NOT EXISTS contact_links_hub ON contact_links(hub_id)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) AddAccount(a Account) (Account, error) {
	b := make([]byte, 6)
	rand.Read(b)
	a.ID = hex.EncodeToString(b)
	a.Created = time.Now().Unix()
	if a.Status == "" {
		a.Status = "new"
	}
	_, err := s.db.Exec(`INSERT INTO accounts(id,kind,provider,name,config,secret,status,last_sync,last_error,created) VALUES(?,?,?,?,?,?,?,0,'',?)`,
		a.ID, a.Kind, a.Provider, a.Name, a.Config, a.Secret, a.Status, a.Created)
	return a, err
}

func (s *Store) Accounts() []Account {
	rows, err := s.db.Query(`SELECT id,kind,COALESCE(provider,''),COALESCE(name,''),COALESCE(config,''),secret,COALESCE(status,''),COALESCE(last_sync,0),COALESCE(last_error,''),COALESCE(created,0) FROM accounts ORDER BY created`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		var a Account
		if rows.Scan(&a.ID, &a.Kind, &a.Provider, &a.Name, &a.Config, &a.Secret, &a.Status, &a.LastSync, &a.LastError, &a.Created) == nil {
			out = append(out, a)
		}
	}
	return out
}

func (s *Store) Account(id string) (Account, bool) {
	for _, a := range s.Accounts() {
		if a.ID == id {
			return a, true
		}
	}
	return Account{}, false
}

func (s *Store) SetAccountStatus(id, status, lastErr string, synced bool) {
	if synced {
		s.db.Exec(`UPDATE accounts SET status=?, last_error=?, last_sync=? WHERE id=?`, status, lastErr, time.Now().Unix(), id)
	} else {
		s.db.Exec(`UPDATE accounts SET status=?, last_error=? WHERE id=?`, status, lastErr, id)
	}
}

func (s *Store) RemoveAccount(id string) {
	s.db.Exec(`DELETE FROM accounts WHERE id=?`, id)
	s.db.Exec(`DELETE FROM synced WHERE account=?`, id)
}

func (s *Store) SyncedGet(account, key string) (string, bool) {
	var v string
	if err := s.db.QueryRow(`SELECT COALESCE(value,'') FROM synced WHERE account=? AND key=?`, account, key).Scan(&v); err != nil {
		return "", false
	}
	return v, true
}

func (s *Store) SyncedSet(account, key, value string) {
	s.db.Exec(`INSERT INTO synced(account,key,value) VALUES(?,?,?) ON CONFLICT(account,key) DO UPDATE SET value=excluded.value`, account, key, value)
}

func (s *Store) SyncedDelete(account, key string) {
	s.db.Exec(`DELETE FROM synced WHERE account=? AND key=?`, account, key)
}

// SyncedKeys lists the keys with a prefix (what a DAV sync saw last time, to notice deletions).
func (s *Store) SyncedKeys(account, prefix string) map[string]string {
	out := map[string]string{}
	rows, err := s.db.Query(`SELECT key, COALESCE(value,'') FROM synced WHERE account=? AND key LIKE ?`, account, prefix+"%")
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			out[k] = v
		}
	}
	return out
}

// ---- contact links: hub contact <-> (source, entry id) ----

type Link struct {
	HubID   string `json:"hub_id"`
	Source  string `json:"source"`   // an account id, or a device id
	EntryID string `json:"entry_id"` // the entry's id in that source
}

func (s *Store) LinkContact(hubID, source, entryID string) error {
	_, err := s.db.Exec(`INSERT INTO contact_links(hub_id,source,entry_id,created) VALUES(?,?,?,?) ON CONFLICT(source,entry_id) DO UPDATE SET hub_id=excluded.hub_id`,
		hubID, source, entryID, time.Now().Unix())
	return err
}

func (s *Store) Links(hubID string) []Link {
	q, args := `SELECT hub_id,source,entry_id FROM contact_links ORDER BY created`, []any{}
	if hubID != "" {
		q, args = `SELECT hub_id,source,entry_id FROM contact_links WHERE hub_id=? ORDER BY created`, []any{hubID}
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []Link{}
	for rows.Next() {
		var l Link
		if rows.Scan(&l.HubID, &l.Source, &l.EntryID) == nil {
			out = append(out, l)
		}
	}
	return out
}

// MoveLinks re-points every link of one hub contact to another (after a merge).
func (s *Store) MoveLinks(from, to string) {
	s.db.Exec(`UPDATE contact_links SET hub_id=? WHERE hub_id=?`, to, from)
}

// UnlinkSource drops the links of a removed account.
func (s *Store) UnlinkSource(source string) {
	s.db.Exec(`DELETE FROM contact_links WHERE source=?`, source)
}

// LinkTarget returns the hub contact a source entry is linked to.
func (s *Store) LinkTarget(source, entryID string) (string, bool) {
	var h string
	if err := s.db.QueryRow(`SELECT hub_id FROM contact_links WHERE source=? AND entry_id=?`, source, entryID).Scan(&h); err != nil {
		return "", false
	}
	return h, true
}
