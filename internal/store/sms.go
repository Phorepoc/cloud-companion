// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"errors"
	"strings"
	"time"
	"unicode"
)

// SMS through the user's own phones (hub-design.md section 2). Each phone is the connector of its own SIM: it uploads the messages it
// received and sent, with its OWN message id, and polls for messages the web app wants it to send. A message is identified by
// (device, device message id), so a retry or a reconnect never duplicates it. Outbound messages start as `queued` with a hub-assigned
// id; the phone reports sent / delivered / failed for that id.

type SMS struct {
	ID       int64  `json:"id"`
	Device   string `json:"device"`
	DevMsgID string `json:"dev_msg_id"` // the phone's id for it ("" while an outbound message has not been picked up yet)
	Number   string `json:"number"`     // as the phone shows it
	Key      string `json:"key"`        // the number reduced for matching (digits, last 9)
	Dir      string `json:"dir"`        // in | out
	Body     string `json:"body"`
	TS       int64  `json:"ts"`
	Status   string `json:"status"` // received | queued | sent | delivered | failed
	Error    string `json:"error,omitempty"`
}

func (s *Store) initSMS() error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS sms(id INTEGER PRIMARY KEY AUTOINCREMENT, device TEXT NOT NULL, dev_msg_id TEXT, number TEXT NOT NULL, key TEXT NOT NULL, dir TEXT NOT NULL, body TEXT, ts INTEGER, status TEXT, error TEXT)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS sms_dev ON sms(device, dev_msg_id) WHERE dev_msg_id IS NOT NULL AND dev_msg_id != ''`,
		`CREATE INDEX IF NOT EXISTS sms_key ON sms(key, ts)`,
		`CREATE TABLE IF NOT EXISTS sms_devices(device TEXT PRIMARY KEY, label TEXT, number TEXT, last_sync INTEGER)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// NumberKey reduces a phone number for matching: the last 9 digits (+49 171 ... equals 0171 ...); shorter numbers (short codes) stay whole.
func NumberKey(n string) string {
	var b strings.Builder
	for _, r := range n {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if len(d) > 9 {
		d = d[len(d)-9:]
	}
	if d == "" {
		return strings.ToLower(strings.TrimSpace(n)) // alphanumeric sender ids
	}
	return d
}

// SMSUpload records messages from a phone; returns how many were new. Messages already known (same device message id) are ignored.
func (s *Store) SMSUpload(device string, msgs []SMS) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	n := 0
	for _, m := range msgs {
		if m.DevMsgID == "" || m.Number == "" || (m.Dir != "in" && m.Dir != "out") {
			continue
		}
		st := m.Status
		if st == "" {
			st = map[string]string{"in": "received", "out": "sent"}[m.Dir]
		}
		res, err := tx.Exec(`INSERT OR IGNORE INTO sms(device,dev_msg_id,number,key,dir,body,ts,status) VALUES(?,?,?,?,?,?,?,?)`,
			device, m.DevMsgID, m.Number, NumberKey(m.Number), m.Dir, m.Body, m.TS, st)
		if err != nil {
			return 0, err
		}
		if k, _ := res.RowsAffected(); k > 0 {
			n++
		} else if m.Dir == "out" { // a known outbound message: its status may have moved on
			tx.Exec(`UPDATE sms SET status=? WHERE device=? AND dev_msg_id=? AND status NOT IN ('delivered','failed')`, st, device, m.DevMsgID)
		}
	}
	tx.Exec(`INSERT INTO sms_devices(device,last_sync) VALUES(?,?) ON CONFLICT(device) DO UPDATE SET last_sync=excluded.last_sync`, device, time.Now().Unix())
	return n, tx.Commit()
}

// SMSQueue puts an outbound message in a device's queue; returns its hub id.
func (s *Store) SMSQueue(device, number, body string) (int64, error) {
	if strings.TrimSpace(number) == "" || strings.TrimSpace(body) == "" {
		return 0, errors.New("a number and a text are needed")
	}
	res, err := s.db.Exec(`INSERT INTO sms(device,dev_msg_id,number,key,dir,body,ts,status) VALUES(?,NULL,?,?,'out',?,?,'queued')`,
		device, strings.TrimSpace(number), NumberKey(number), body, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SMSPending returns what a device still has to send (queued messages), oldest first.
func (s *Store) SMSPending(device string) []SMS {
	rows, err := s.db.Query(`SELECT id,number,body,ts FROM sms WHERE device=? AND dir='out' AND status='queued' ORDER BY id LIMIT 50`, device)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []SMS{}
	for rows.Next() {
		m := SMS{Device: device, Dir: "out", Status: "queued"}
		if rows.Scan(&m.ID, &m.Number, &m.Body, &m.TS) == nil {
			out = append(out, m)
		}
	}
	return out
}

// SMSAck applies a device's report about an outbound message it was asked to send: its own id for it and the new status.
func (s *Store) SMSAck(device string, hubID int64, devMsgID, status, errText string) error {
	if status != "sent" && status != "delivered" && status != "failed" {
		return errors.New("bad status")
	}
	if devMsgID != "" { // the phone may already have uploaded its own copy of this message (it shows up in its Sent folder): keep that row, drop the queued one
		var existing int64
		if s.db.QueryRow(`SELECT id FROM sms WHERE device=? AND dev_msg_id=? AND id!=?`, device, devMsgID, hubID).Scan(&existing) == nil {
			if _, err := s.db.Exec(`DELETE FROM sms WHERE id=? AND device=? AND dir='out' AND status='queued'`, hubID, device); err != nil {
				return err
			}
			s.db.Exec(`UPDATE sms SET status=?, error=? WHERE id=? AND status NOT IN ('delivered')`, status, errText, existing)
			return nil
		}
	}
	res, err := s.db.Exec(`UPDATE sms SET status=?, error=?, dev_msg_id=CASE WHEN ?!='' THEN ? ELSE dev_msg_id END WHERE id=? AND device=? AND dir='out' AND status NOT IN ('delivered') `,
		status, errText, devMsgID, devMsgID, hubID, device)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("unknown message")
	}
	return nil
}

// SMSDevices lists the devices that sync SMS.
type SMSDevice struct {
	Device   string `json:"device"`
	Name     string `json:"name"`
	LastSync int64  `json:"last_sync"`
}

func (s *Store) SMSDevices() []SMSDevice {
	rows, err := s.db.Query(`SELECT d.device, COALESCE(v.name,''), COALESCE(d.last_sync,0) FROM sms_devices d LEFT JOIN devices v ON v.id=d.device ORDER BY d.last_sync DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []SMSDevice{}
	for rows.Next() {
		var d SMSDevice
		if rows.Scan(&d.Device, &d.Name, &d.LastSync) == nil {
			out = append(out, d)
		}
	}
	return out
}

// SMSConversation is one line of the conversation list: a number (all devices), its last message and the unread-ish count.
type SMSConversation struct {
	Key      string `json:"key"`
	Number   string `json:"number"`
	Last     string `json:"last"`
	LastTS   int64  `json:"last_ts"`
	LastDir  string `json:"last_dir"`
	Count    int    `json:"count"`
	Devices  string `json:"devices"` // comma separated device ids that carry this conversation
	LastFail bool   `json:"last_failed"`
}

func (s *Store) SMSConversations() []SMSConversation {
	rows, err := s.db.Query(`SELECT key, MAX(number), COUNT(*), GROUP_CONCAT(DISTINCT device) FROM sms GROUP BY key`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []SMSConversation{}
	for rows.Next() {
		var c SMSConversation
		if rows.Scan(&c.Key, &c.Number, &c.Count, &c.Devices) == nil {
			out = append(out, c)
		}
	}
	for i := range out {
		var fail string
		s.db.QueryRow(`SELECT body, ts, dir, COALESCE(status,'') FROM sms WHERE key=? ORDER BY ts DESC, id DESC LIMIT 1`, out[i].Key).Scan(&out[i].Last, &out[i].LastTS, &out[i].LastDir, &fail)
		out[i].LastFail = fail == "failed"
	}
	for i := 0; i < len(out); i++ { // newest first
		for j := i + 1; j < len(out); j++ {
			if out[j].LastTS > out[i].LastTS {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// SMSThread returns the messages of a conversation (oldest first).
func (s *Store) SMSThread(key string, limit int) []SMS {
	rows, err := s.db.Query(`SELECT id,device,COALESCE(dev_msg_id,''),number,key,dir,COALESCE(body,''),COALESCE(ts,0),COALESCE(status,''),COALESCE(error,'') FROM (SELECT * FROM sms WHERE key=? ORDER BY ts DESC, id DESC LIMIT ?) ORDER BY ts, id`, key, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []SMS{}
	for rows.Next() {
		var m SMS
		if rows.Scan(&m.ID, &m.Device, &m.DevMsgID, &m.Number, &m.Key, &m.Dir, &m.Body, &m.TS, &m.Status, &m.Error) == nil {
			out = append(out, m)
		}
	}
	return out
}
