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

// ---- explicit merges of two entries ON ONE DEVICE/SERVICE (with an undo window) ----

type DevMerge struct {
	ID         string `json:"id"`
	Account    string `json:"account"`
	AHref      string `json:"a_href"`
	ABody      string `json:"-"`
	AETag      string `json:"-"`
	BHref      string `json:"b_href"`
	BBody      string `json:"-"`
	BETag      string `json:"-"`
	MergedBody string `json:"-"`
	MergedETag string `json:"-"`
	State      string `json:"state"` // preview | applied | undone
	Expires    int64  `json:"expires"`
}

func (s *Store) DevMergeSave(m DevMerge) DevMerge {
	if m.ID == "" {
		b := make([]byte, 6)
		rand.Read(b)
		m.ID = hex.EncodeToString(b)
	}
	s.db.Exec(`INSERT INTO dev_merges(id,account,a_href,a_body,a_etag,b_href,b_body,b_etag,merged_body,merged_etag,state,expires) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
	  ON CONFLICT(id) DO UPDATE SET state=excluded.state, merged_etag=excluded.merged_etag, expires=excluded.expires`,
		m.ID, m.Account, m.AHref, m.ABody, m.AETag, m.BHref, m.BBody, m.BETag, m.MergedBody, m.MergedETag, m.State, m.Expires)
	return m
}

func (s *Store) DevMergeGet(id string) (DevMerge, bool) {
	var m DevMerge
	err := s.db.QueryRow(`SELECT id,account,a_href,a_body,a_etag,b_href,b_body,b_etag,merged_body,COALESCE(merged_etag,''),state,expires FROM dev_merges WHERE id=?`, id).
		Scan(&m.ID, &m.Account, &m.AHref, &m.ABody, &m.AETag, &m.BHref, &m.BBody, &m.BETag, &m.MergedBody, &m.MergedETag, &m.State, &m.Expires)
	return m, err == nil
}

// SetAccountSecret replaces the sealed credential of an account (an OAuth access token that was refreshed).
func (s *Store) SetAccountSecret(id string, sealed []byte) {
	s.db.Exec(`UPDATE accounts SET secret=? WHERE id=?`, sealed, id)
}

// UnlinkEntry drops one link (an entry deleted at its source).
func (s *Store) UnlinkEntry(source, entryID string) {
	s.db.Exec(`DELETE FROM contact_links WHERE source=? AND entry_id=?`, source, entryID)
}
