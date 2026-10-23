// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package connectors

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

type lit struct{ *bytes.Reader }

func (l lit) Size() int64 { return int64(l.Reader.Size()) }

func msg(id, subj string) []byte {
	return []byte(fmt.Sprintf("From: a@example.org\r\nTo: me@example.org\r\nSubject: %s\r\nMessage-ID: <%s@example.org>\r\nDate: %s\r\n\r\nbody of %s\r\n", subj, id, time.Now().Format(time.RFC1123Z), subj))
}

func startIMAP(t *testing.T) (ServerCfg, *imapmemserver.User) {
	t.Helper()
	mem := imapmemserver.New()
	u := imapmemserver.NewUser("me", "secret")
	u.Create("INBOX", nil)
	u.Create("Sent", &imap.CreateOptions{SpecialUse: []imap.MailboxAttr{imap.MailboxAttrSent}})
	mem.AddUser(u)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		InsecureAuth: true,
	})
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	a := ln.Addr().(*net.TCPAddr)
	return ServerCfg{Host: "127.0.0.1", Port: a.Port, TLS: "plain", User: "me"}, u
}

type fakeSink struct {
	mu      sync.Mutex
	folders map[string]string
	mails   map[string][][]byte
	seen    map[string]bool
}

func (f *fakeSink) MailFolderID(n string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.folders == nil {
		f.folders, f.mails = map[string]string{}, map[string][][]byte{}
	}
	if _, ok := f.folders[n]; !ok {
		f.folders[n] = fmt.Sprintf("f%d", len(f.folders)+1)
	}
	return f.folders[n], nil
}
func (f *fakeSink) MailImport(id string, raw []byte, seen bool, at time.Time) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mails[id] = append(f.mails[id], raw)
	return "e", nil
}

type memSeen struct{ m map[string]string }

func (s *memSeen) SyncedGet(a, k string) (string, bool) { v, ok := s.m[a+"|"+k]; return v, ok }
func (s *memSeen) SyncedSet(a, k, v string)             { s.m[a+"|"+k] = v }

func TestIMAPSync(t *testing.T) {
	cfg, u := startIMAP(t)
	for i := 1; i <= 3; i++ {
		raw := msg(fmt.Sprint(i), fmt.Sprintf("hello %d", i))
		if _, err := u.Append("INBOX", lit{bytes.NewReader(raw)}, &imap.AppendOptions{Time: time.Now().Add(-time.Duration(i) * time.Hour), Flags: []imap.Flag{imap.FlagSeen}}); err != nil {
			t.Fatal(err)
		}
	}
	old := msg("old", "ancient")
	u.Append("INBOX", lit{bytes.NewReader(old)}, &imap.AppendOptions{Time: time.Now().AddDate(-1, -2, 0)})

	folders, err := TestIMAP(cfg, "secret")
	if err != nil || len(folders) < 2 {
		t.Fatalf("test: %v %v", folders, err)
	}
	if _, err := TestIMAP(cfg, "wrong"); err == nil {
		t.Fatal("a wrong password was accepted")
	}

	sink, seen := &fakeSink{}, &memSeen{map[string]string{}}
	mc := MailCfg{IMAP: cfg, Months: 3}
	n, err := SyncIMAP(context.Background(), "acc1", "Work", mc, "secret", sink, seen, 100)
	if err != nil || n != 3 {
		t.Fatalf("first sync: %d %v", n, err)
	}
	if got := len(sink.mails[sink.folders["Work / INBOX"]]); got != 3 {
		t.Fatalf("cache holds %d, want 3 (the old one is outside the window)", got)
	}
	for _, m := range sink.mails[sink.folders["Work / INBOX"]] {
		if strings.Contains(string(m), "ancient") {
			t.Fatal("a message older than the window was pulled")
		}
	}
	// a second run pulls nothing again; a new message arrives -> exactly one
	if n, _ := SyncIMAP(context.Background(), "acc1", "Work", mc, "secret", sink, seen, 100); n != 0 {
		t.Fatalf("second sync pulled %d again", n)
	}
	u.Append("INBOX", lit{bytes.NewReader(msg("4", "fresh"))}, &imap.AppendOptions{Time: time.Now()})
	if n, _ := SyncIMAP(context.Background(), "acc1", "Work", mc, "secret", sink, seen, 100); n != 1 {
		t.Fatalf("third sync pulled %d, want 1", n)
	}
	// the per-run cap bounds a big mailbox
	for i := 10; i < 15; i++ {
		u.Append("INBOX", lit{bytes.NewReader(msg(fmt.Sprint(i), "bulk"))}, &imap.AppendOptions{Time: time.Now()})
	}
	if n, _ := SyncIMAP(context.Background(), "acc1", "Work", mc, "secret", sink, seen, 2); n != 2 {
		t.Fatalf("capped sync pulled %d, want 2", n)
	}
}

type smtpBackend struct {
	mu   sync.Mutex
	from string
	to   []string
	data []byte
}

func (b *smtpBackend) NewSession(*smtp.Conn) (smtp.Session, error) { return &smtpSession{b: b}, nil }

type smtpSession struct{ b *smtpBackend }

func (s *smtpSession) AuthMechanisms() []string { return []string{sasl.Plain} }
func (s *smtpSession) Auth(string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(identity, user, pass string) error {
		if user != "me" || pass != "secret" {
			return fmt.Errorf("bad credentials")
		}
		return nil
	}), nil
}
func (s *smtpSession) Mail(from string, _ *smtp.MailOptions) error { s.b.from = from; return nil }
func (s *smtpSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.b.to = append(s.b.to, to)
	return nil
}
func (s *smtpSession) Data(r io.Reader) error {
	b, _ := io.ReadAll(r)
	s.b.mu.Lock()
	s.b.data = b
	s.b.mu.Unlock()
	return nil
}
func (s *smtpSession) Reset()        {}
func (s *smtpSession) Logout() error { return nil }

func TestRelay(t *testing.T) {
	icfg, u := startIMAP(t)
	be := &smtpBackend{}
	srv := smtp.NewServer(be)
	srv.AllowInsecureAuth = true
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	defer srv.Close()
	sc := ServerCfg{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, TLS: "plain", User: "me"}
	mc := MailCfg{IMAP: icfg, SMTP: sc, From: "me@example.org"}
	if err := TestSMTP(sc, "secret"); err != nil {
		t.Fatal("smtp test:", err)
	}
	if TestSMTP(sc, "nope") == nil {
		t.Fatal("a wrong SMTP password was accepted")
	}
	raw := BuildMessage("me@example.org", []string{"you@example.net"}, "Relayed", "line one line two")
	if err := Relay(mc, "secret", "me@example.org", []string{"you@example.net"}, raw); err != nil {
		t.Fatal(err)
	}
	if be.from != "me@example.org" || len(be.to) != 1 || be.to[0] != "you@example.net" || !strings.Contains(string(be.data), "Subject: Relayed") {
		t.Fatalf("the SMTP server got %q %v %q", be.from, be.to, be.data)
	}
	// the copy is in the account's Sent folder
	cl, err := dialIMAP(icfg, "secret")
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	st, err := cl.Select("Sent", nil).Wait()
	if err != nil || st.NumMessages != 1 {
		t.Fatalf("Sent holds %v messages (%v)", st, err)
	}
	_ = u
}
