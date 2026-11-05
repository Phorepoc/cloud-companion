// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package store

// The SIP front (docs/sip.md, migration 6). Each phone that talks SIP to Cloud Companion has its own login; only the digest HA1
// (MD5 of "name:realm:password") is kept, so the password itself is shown once and never stored. Messages for a SIP device wait in
// sip_queue until the device is registered and has taken them; each login also remembers the last SMS it was given (sms_cursor).

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"
)

// SIPRealm is the digest realm of the SIP front; it is part of every stored HA1, so it never changes.
const SIPRealm = "cloud-companion"

var sipName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

type SIPUser struct {
	Name    string `json:"name"`
	Created int64  `json:"created"`
}

// SIPHA1 is the digest HA1 for a login.
func SIPHA1(name, password string) string {
	h := md5.Sum([]byte(name + ":" + SIPRealm + ":" + password))
	return hex.EncodeToString(h[:])
}

// AddSIPUser creates a login and returns its password (shown once). A login's SMS start with the messages that arrive after it was made.
func (s *Store) AddSIPUser(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !sipName.MatchString(name) {
		return "", errors.New("a SIP name is 1 to 32 of a-z, 0-9, '.', '_' or '-', starting with a letter or digit")
	}
	if strings.Trim(name, "0123456789+") == "" {
		return "", errors.New("a SIP name cannot be all digits: those addresses are phone numbers")
	}
	b := make([]byte, 12)
	rand.Read(b)
	pw := hex.EncodeToString(b)
	var cur int64
	s.db.QueryRow(`SELECT COALESCE(MAX(id),0) FROM sms`).Scan(&cur)
	if _, err := s.db.Exec(`INSERT INTO sip_users(name,ha1,created,sms_cursor) VALUES(?,?,?,?)`, name, SIPHA1(name, pw), time.Now().Unix(), cur); err != nil {
		return "", errors.New("that SIP name is taken")
	}
	return pw, nil
}

// RemoveSIPUser deletes a login and what was waiting for it.
func (s *Store) RemoveSIPUser(name string) error {
	res, err := s.db.Exec(`DELETE FROM sip_users WHERE name=?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("no such SIP login")
	}
	s.db.Exec(`DELETE FROM sip_queue WHERE to_user=?`, name)
	return nil
}

func (s *Store) SIPUsers() []SIPUser {
	rows, err := s.db.Query(`SELECT name, COALESCE(created,0) FROM sip_users ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []SIPUser{}
	for rows.Next() {
		var u SIPUser
		if rows.Scan(&u.Name, &u.Created) == nil {
			out = append(out, u)
		}
	}
	return out
}

// SIPUserHA1 returns a login's HA1, "" when there is no such login.
func (s *Store) SIPUserHA1(name string) string {
	var h string
	s.db.QueryRow(`SELECT ha1 FROM sip_users WHERE name=?`, name).Scan(&h)
	return h
}

// SIPMessage is a message waiting for a SIP device.
type SIPMessage struct {
	ID       int64
	To       string
	FromURI  string
	FromName string
	Body     string
	TS       int64
}

// SIPQueue keeps a message for a SIP device until it has taken it.
func (s *Store) SIPQueue(to, fromURI, fromName, body string) error {
	_, err := s.db.Exec(`INSERT INTO sip_queue(to_user,from_uri,from_name,body,ts) VALUES(?,?,?,?,?)`, to, fromURI, fromName, body, time.Now().Unix())
	return err
}

// SIPPending returns what is waiting for a device, oldest first.
func (s *Store) SIPPending(to string) []SIPMessage {
	rows, err := s.db.Query(`SELECT id,to_user,from_uri,COALESCE(from_name,''),COALESCE(body,''),COALESCE(ts,0) FROM sip_queue WHERE to_user=? ORDER BY id LIMIT 50`, to)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []SIPMessage{}
	for rows.Next() {
		var m SIPMessage
		if rows.Scan(&m.ID, &m.To, &m.FromURI, &m.FromName, &m.Body, &m.TS) == nil {
			out = append(out, m)
		}
	}
	return out
}

// SIPDelivered drops a message the device has accepted.
func (s *Store) SIPDelivered(id int64) { s.db.Exec(`DELETE FROM sip_queue WHERE id=?`, id) }

// SIPNewSMS returns the received SMS a login has not been given yet (after its cursor), oldest first.
func (s *Store) SIPNewSMS(name string) []SMS {
	var cur int64
	if s.db.QueryRow(`SELECT COALESCE(sms_cursor,0) FROM sip_users WHERE name=?`, name).Scan(&cur) != nil {
		return nil
	}
	rows, err := s.db.Query(`SELECT id,number,COALESCE(body,''),COALESCE(ts,0) FROM sms WHERE id>? AND dir='in' ORDER BY id LIMIT 50`, cur)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []SMS{}
	for rows.Next() {
		m := SMS{Dir: "in"}
		if rows.Scan(&m.ID, &m.Number, &m.Body, &m.TS) == nil {
			out = append(out, m)
		}
	}
	return out
}

// SIPSMSDone moves a login's cursor past an SMS it has been given.
func (s *Store) SIPSMSDone(name string, id int64) {
	s.db.Exec(`UPDATE sip_users SET sms_cursor=? WHERE name=? AND sms_cursor<?`, id, name, id)
}

// SMSRoute picks the phone an SMS to number goes out through: the one that carries that conversation most recently, else the phone
// that synced last. "" when no phone syncs SMS.
func (s *Store) SMSRoute(number string) string {
	var d string
	if s.db.QueryRow(`SELECT device FROM sms WHERE key=? ORDER BY ts DESC, id DESC LIMIT 1`, NumberKey(number)).Scan(&d) == nil && d != "" {
		return d
	}
	s.db.QueryRow(`SELECT device FROM sms_devices ORDER BY last_sync DESC LIMIT 1`).Scan(&d)
	return d
}
