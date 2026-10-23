// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package connectors pulls outside accounts into the hub. The hub (Stalwart) is only the local cache: mail is IMPORTED from the
// accounts' IMAP servers (never delivered over SMTP, no MX, no DKIM), outgoing mail is relayed through the matching account's own
// SMTP server and the copy is appended to that account's Sent folder; contacts and calendars are pulled from CardDAV / CalDAV.
package connectors

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// ServerCfg is one IMAP or SMTP endpoint.
type ServerCfg struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	TLS  string `json:"tls"` // "tls" (implicit), "starttls", "plain" (tests, localhost only)
	User string `json:"user"`
}

// MailCfg is the config of an "imap" account.
type MailCfg struct {
	IMAP    ServerCfg `json:"imap"`
	SMTP    ServerCfg `json:"smtp"`
	Folders []string  `json:"folders"` // default INBOX
	Months  int       `json:"months"`  // how far back to pull (default 3)
	From    string    `json:"from"`    // the address mail from this account is sent as
}

// Sink is the local cache the mail goes into (davhub.Client).
type Sink interface {
	MailFolderID(name string) (string, error)
	MailImport(folderID string, raw []byte, seen bool, received time.Time) (string, error)
}

// Seen remembers what was pulled already (store.Store).
type Seen interface {
	SyncedGet(account, key string) (string, bool)
	SyncedSet(account, key, value string)
}

func (s ServerCfg) addr() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.Port)) }

func dialIMAP(c ServerCfg, pass string) (*imapclient.Client, error) {
	opts := &imapclient.Options{TLSConfig: &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}}
	var cl *imapclient.Client
	var err error
	switch c.TLS {
	case "tls":
		cl, err = imapclient.DialTLS(c.addr(), opts)
	case "starttls":
		cl, err = imapclient.DialStartTLS(c.addr(), opts)
	case "plain":
		cl, err = imapclient.DialInsecure(c.addr(), opts)
	default:
		return nil, fmt.Errorf("unknown TLS mode %q", c.TLS)
	}
	if err != nil {
		return nil, err
	}
	if err := cl.Login(c.User, pass).Wait(); err != nil {
		cl.Close()
		return nil, fmt.Errorf("login refused: %w", err)
	}
	return cl, nil
}

// TestIMAP logs in and lists the folders (what the Connections page's Test button runs).
func TestIMAP(c ServerCfg, pass string) ([]string, error) {
	cl, err := dialIMAP(c, pass)
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	l, err := cl.List("", "*", nil).Collect()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range l {
		out = append(out, m.Mailbox)
	}
	return out, nil
}

// SyncIMAP pulls the recent messages of the configured folders into the sink; messages already pulled (by folder, UIDVALIDITY and UID)
// are skipped. At most maxMsgs are imported per run, so a big mailbox fills in over several runs. It returns how many were imported.
func SyncIMAP(ctx context.Context, account, accName string, c MailCfg, pass string, sink Sink, seen Seen, maxMsgs int) (int, error) {
	if c.Months <= 0 {
		c.Months = 3
	}
	if len(c.Folders) == 0 {
		c.Folders = []string{"INBOX"}
	}
	cl, err := dialIMAP(c.IMAP, pass)
	if err != nil {
		return 0, err
	}
	defer cl.Close()
	since := time.Now().AddDate(0, -c.Months, 0)
	total := 0
	for _, folder := range c.Folders {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		sel, err := cl.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
		if err != nil {
			return total, fmt.Errorf("folder %s: %w", folder, err)
		}
		sd, err := cl.UIDSearch(&imap.SearchCriteria{Since: since}, nil).Wait()
		if err != nil {
			return total, err
		}
		var todo []imap.UID
		for _, u := range sd.AllUIDs() {
			if _, done := seen.SyncedGet(account, fmt.Sprintf("imap:%s:%d:%d", folder, sel.UIDValidity, u)); !done {
				todo = append(todo, u)
			}
		}
		if len(todo) == 0 {
			continue
		}
		hubFolder, err := sink.MailFolderID(accName + " / " + folder)
		if err != nil {
			return total, err
		}
		for i := 0; i < len(todo) && total < maxMsgs; i += 10 {
			j := i + 10
			if j > len(todo) {
				j = len(todo)
			}
			var set imap.UIDSet
			for _, u := range todo[i:j] {
				set.AddNum(u)
			}
			msgs, err := cl.Fetch(set, &imap.FetchOptions{UID: true, Flags: true, InternalDate: true, BodySection: []*imap.FetchItemBodySection{{Peek: true}}}).Collect()
			if err != nil {
				return total, err
			}
			for _, m := range msgs {
				if total >= maxMsgs {
					break
				}
				var raw []byte
				for _, b := range m.BodySection {
					raw = b.Bytes
				}
				if len(raw) == 0 {
					continue
				}
				isSeen := false
				for _, f := range m.Flags {
					if f == imap.FlagSeen {
						isSeen = true
					}
				}
				if _, err := sink.MailImport(hubFolder, raw, isSeen, m.InternalDate); err != nil {
					return total, err
				}
				seen.SyncedSet(account, fmt.Sprintf("imap:%s:%d:%d", folder, sel.UIDValidity, m.UID), "1")
				total++
			}
		}
	}
	return total, nil
}

// ---- outgoing: relayed through the account's own SMTP server, the copy appended to its Sent folder ----

func smtpClient(c ServerCfg) (*smtp.Client, error) {
	switch c.TLS {
	case "tls":
		conn, err := tls.Dial("tcp", c.addr(), &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12})
		if err != nil {
			return nil, err
		}
		return smtp.NewClient(conn, c.Host)
	case "starttls", "plain":
		conn, err := net.DialTimeout("tcp", c.addr(), 20*time.Second)
		if err != nil {
			return nil, err
		}
		cl, err := smtp.NewClient(conn, c.Host)
		if err != nil {
			return nil, err
		}
		if c.TLS == "starttls" {
			if err := cl.StartTLS(&tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}); err != nil {
				cl.Close()
				return nil, err
			}
		}
		return cl, nil
	}
	return nil, fmt.Errorf("unknown TLS mode %q", c.TLS)
}

// TestSMTP connects and authenticates (no mail is sent).
func TestSMTP(c ServerCfg, pass string) error {
	cl, err := smtpClient(c)
	if err != nil {
		return err
	}
	defer cl.Close()
	if ok, _ := cl.Extension("AUTH"); ok {
		user := c.User
		if err := cl.Auth(smtp.PlainAuth("", user, pass, c.Host)); err != nil {
			return fmt.Errorf("login refused: %w", err)
		}
	}
	cl.Quit() // the server may close without a clean reply; the login already worked
	return nil
}

// Relay sends the message through the account's SMTP server and appends a copy to the account's Sent folder.
func Relay(c MailCfg, pass, from string, to []string, raw []byte) error {
	if len(to) == 0 {
		return errors.New("no recipient")
	}
	cl, err := smtpClient(c.SMTP)
	if err != nil {
		return err
	}
	defer cl.Close()
	if ok, _ := cl.Extension("AUTH"); ok {
		user := c.SMTP.User
		if user == "" {
			user = c.IMAP.User
		}
		if err := cl.Auth(smtp.PlainAuth("", user, pass, c.SMTP.Host)); err != nil {
			return fmt.Errorf("SMTP login refused: %w", err)
		}
	}
	if err := cl.Mail(from); err != nil {
		return err
	}
	for _, r := range to {
		if err := cl.Rcpt(r); err != nil {
			return fmt.Errorf("recipient %s refused: %w", r, err)
		}
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	cl.Quit()
	return appendSent(c, pass, raw)
}

func appendSent(c MailCfg, pass string, raw []byte) error {
	cl, err := dialIMAP(c.IMAP, pass)
	if err != nil {
		return fmt.Errorf("sent, but could not file the copy: %w", err)
	}
	defer cl.Close()
	sent := "Sent"
	if l, err := cl.List("", "*", nil).Collect(); err == nil {
		for _, m := range l {
			for _, a := range m.Attrs {
				if a == imap.MailboxAttrSent {
					sent = m.Mailbox
				}
			}
		}
	}
	cmd := cl.Append(sent, int64(len(raw)), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagSeen}, Time: time.Now()})
	if _, err := cmd.Write(raw); err != nil {
		return err
	}
	if err := cmd.Close(); err != nil {
		return err
	}
	if _, err := cmd.Wait(); err != nil {
		return fmt.Errorf("sent, but could not file the copy in %s: %w", sent, err)
	}
	return nil
}

// BuildMessage makes a plain-text RFC 5322 message for Relay (the hub's composer gives subject and body).
func BuildMessage(from string, to []string, subject, body string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n",
		from, strings.Join(to, ", "), strings.NewReplacer("\r", " ", "\n", " ").Replace(subject), time.Now().Format(time.RFC1123Z))
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n"))
	return b.Bytes()
}
