// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Chat connections (docs/messaging.md, migration 6). Every service a message can go through (SMS on your phones, your SIP devices,
// later Matrix, XMPP, IRC...) is a connection. What arrives on any of them lands in chat_inbox; each SIP login takes the inbox rows
// meant for it in order (inbox_cursor). The routing rules (chat_routes) say which connection an address goes to, and chat_deliver
// which logins get which connection's messages (no rows for a connection: every login).
//
// SIP logins keep only the digest HA1 (MD5 of "name:realm:password"): the password is shown once and never stored.

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

// AddSIPUser creates a login and returns its password (shown once). A new login gets what arrives after it was made.
func (s *Store) AddSIPUser(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !sipName.MatchString(name) {
		return "", errors.New("a SIP name is 1 to 32 of a-z, 0-9, '.', '_' or '-', starting with a letter or digit")
	}
	if strings.Trim(name, "0123456789") == "" {
		return "", errors.New("a SIP name cannot be all digits: those addresses are phone numbers")
	}
	b := make([]byte, 12)
	rand.Read(b)
	pw := hex.EncodeToString(b)
	var cur int64
	s.db.QueryRow(`SELECT COALESCE(MAX(id),0) FROM chat_inbox`).Scan(&cur)
	if _, err := s.db.Exec(`INSERT INTO sip_users(name,ha1,created,inbox_cursor) VALUES(?,?,?,?)`, name, SIPHA1(name, pw), time.Now().Unix(), cur); err != nil {
		return "", errors.New("that SIP name is taken")
	}
	return pw, nil
}

// RemoveSIPUser deletes a login, what was waiting only for it and its delivery choices.
func (s *Store) RemoveSIPUser(name string) error {
	res, err := s.db.Exec(`DELETE FROM sip_users WHERE name=?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("no such SIP login")
	}
	s.db.Exec(`DELETE FROM chat_inbox WHERE to_login=?`, name)
	s.db.Exec(`DELETE FROM chat_deliver WHERE login=?`, name)
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

// ---- the inbox

// ChatMessage is a message that arrived on a connection. Peer is the other side as that connection names it (a number, a login,
// a Matrix id...); ToLogin is set when it is for one login only (a message between your own devices).
type ChatMessage struct {
	ID       int64  `json:"id"`
	Conn     string `json:"conn"`
	Peer     string `json:"peer"`
	PeerName string `json:"peer_name"`
	ToLogin  string `json:"to_login,omitempty"`
	Body     string `json:"body"`
	TS       int64  `json:"ts"`
}

// ChatIn records an incoming message.
func (s *Store) ChatIn(m ChatMessage) error {
	if m.TS == 0 {
		m.TS = time.Now().Unix()
	}
	var to any
	if m.ToLogin != "" {
		to = m.ToLogin
	}
	_, err := s.db.Exec(`INSERT INTO chat_inbox(conn,peer,peer_name,to_login,body,ts) VALUES(?,?,?,?,?,?)`, m.Conn, m.Peer, m.PeerName, to, m.Body, m.TS)
	return err
}

// ChatFor returns what a login has not taken yet, oldest first: messages for it alone, and messages of connections it gets.
func (s *Store) ChatFor(login string) []ChatMessage {
	var cur int64
	if s.db.QueryRow(`SELECT COALESCE(inbox_cursor,0) FROM sip_users WHERE name=?`, login).Scan(&cur) != nil {
		return nil
	}
	rows, err := s.db.Query(`SELECT id,conn,peer,COALESCE(peer_name,''),COALESCE(to_login,''),COALESCE(body,''),COALESCE(ts,0) FROM chat_inbox i
		WHERE id>? AND (to_login=? OR (to_login IS NULL AND (
			NOT EXISTS (SELECT 1 FROM chat_deliver d WHERE d.conn=i.conn) OR EXISTS (SELECT 1 FROM chat_deliver d WHERE d.conn=i.conn AND d.login=?))))
		ORDER BY id LIMIT 50`, cur, login, login)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		var m ChatMessage
		if rows.Scan(&m.ID, &m.Conn, &m.Peer, &m.PeerName, &m.ToLogin, &m.Body, &m.TS) == nil {
			out = append(out, m)
		}
	}
	return out
}

// ChatTaken moves a login past a message it has accepted.
func (s *Store) ChatTaken(login string, id int64) {
	s.db.Exec(`UPDATE sip_users SET inbox_cursor=? WHERE name=? AND inbox_cursor<?`, id, login, id)
}

// ---- routing rules

// ChatRoute sends the addresses it matches to a connection. Kind "number" matches phone numbers, "prefix" addresses that start with
// Value (the prefix is taken off before the connection sees the address). Opt is the connection's own choice for this rule (for
// SMS: the phone to send through; empty = the phone that carries the conversation).
type ChatRoute struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Conn  string `json:"conn"`
	Opt   string `json:"opt"`
}

func (s *Store) ChatRoutes() []ChatRoute {
	rows, err := s.db.Query(`SELECT match_kind, COALESCE(match_value,''), conn, COALESCE(opt,'') FROM chat_routes ORDER BY position, id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []ChatRoute{}
	for rows.Next() {
		var r ChatRoute
		if rows.Scan(&r.Kind, &r.Value, &r.Conn, &r.Opt) == nil {
			out = append(out, r)
		}
	}
	return out
}

// SetChatRoutes replaces the rules (in order: the first that matches wins).
func (s *Store) SetChatRoutes(rs []ChatRoute) error {
	for _, r := range rs {
		if r.Conn == "" || (r.Kind != "number" && r.Kind != "prefix") || (r.Kind == "prefix" && strings.TrimSpace(r.Value) == "") {
			return errors.New("a rule needs a connection, and a prefix rule its prefix")
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM chat_routes`); err != nil {
		return err
	}
	for i, r := range rs {
		if _, err := tx.Exec(`INSERT INTO chat_routes(position,match_kind,match_value,conn,opt) VALUES(?,?,?,?,?)`, i, r.Kind, strings.TrimSpace(r.Value), r.Conn, r.Opt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ChatDeliver returns, per connection, the logins that get its messages (a connection that is not listed goes to every login).
func (s *Store) ChatDeliver() map[string][]string {
	out := map[string][]string{}
	rows, err := s.db.Query(`SELECT conn, login FROM chat_deliver ORDER BY conn, login`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c, l string
		if rows.Scan(&c, &l) == nil {
			out[c] = append(out[c], l)
		}
	}
	return out
}

// SetChatDeliver sets who gets a connection's messages; no logins = every login.
func (s *Store) SetChatDeliver(conn string, logins []string) error {
	for _, l := range logins { // before the transaction: SQLite has one connection, and the transaction holds it
		if s.SIPUserHA1(l) == "" {
			return errors.New("no SIP login " + l)
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM chat_deliver WHERE conn=?`, conn); err != nil {
		return err
	}
	for _, l := range logins {
		if _, err := tx.Exec(`INSERT INTO chat_deliver(conn,login) VALUES(?,?)`, conn, l); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- SMS as a connection

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

// SMSReceivedAfter returns received SMS with an id above after, oldest first (the SMS connection copies them into the inbox).
func (s *Store) SMSReceivedAfter(after int64) []SMS {
	rows, err := s.db.Query(`SELECT id,device,number,COALESCE(body,''),COALESCE(ts,0) FROM sms WHERE id>? AND dir='in' ORDER BY id LIMIT 200`, after)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []SMS{}
	for rows.Next() {
		m := SMS{Dir: "in"}
		if rows.Scan(&m.ID, &m.Device, &m.Number, &m.Body, &m.TS) == nil {
			out = append(out, m)
		}
	}
	return out
}

// SMSMaxID is the newest SMS id (where the SMS connection starts on a fresh server: old messages are not replayed).
func (s *Store) SMSMaxID() int64 {
	var n int64
	s.db.QueryRow(`SELECT COALESCE(MAX(id),0) FROM sms`).Scan(&n)
	return n
}
