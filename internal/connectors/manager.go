// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/phorepoc/phecloud/internal/davhub"
	"github.com/phorepoc/phecloud/internal/proto"
	"github.com/phorepoc/phecloud/internal/store"
)

// Manager runs the accounts: test, sync (now or every interval), relay outgoing mail.
type Manager struct {
	St       *store.Store
	Hub      *davhub.Client
	V        *Vault
	MaxMails int // per account and run (default 300)
	mu       sync.Mutex
	busy     map[string]bool
}

func NewManager(st *store.Store, hub *davhub.Client, v *Vault) *Manager {
	return &Manager{St: st, Hub: hub, V: v, MaxMails: 300, busy: map[string]bool{}}
}

// Test checks a configuration that is not stored yet (the Connections page's Test button); detail says what was found.
func (m *Manager) Test(ctx context.Context, kind, cfgJSON, secret string) (string, error) {
	switch kind {
	case "imap":
		var c MailCfg
		if err := json.Unmarshal([]byte(cfgJSON), &c); err != nil {
			return "", err
		}
		fl, err := TestIMAP(c.IMAP, secret)
		if err != nil {
			return "", fmt.Errorf("IMAP: %w", err)
		}
		d := fmt.Sprintf("IMAP login works, %d folders", len(fl))
		if c.SMTP.Host != "" {
			sc := c.SMTP
			if sc.User == "" {
				sc.User = c.IMAP.User
			}
			if err := TestSMTP(sc, secret); err != nil {
				return d, fmt.Errorf("SMTP: %w", err)
			}
			d += "; SMTP login works"
		}
		return d, nil
	case "carddav", "caldav":
		var c DAVCfg
		if err := json.Unmarshal([]byte(cfgJSON), &c); err != nil {
			return "", err
		}
		n, err := TestDAV(ctx, c, secret)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("login works, %d entries", n), nil
	}
	return "", errors.New("unknown kind " + kind)
}

// Add stores an account (the secret is sealed first).
func (m *Manager) Add(kind, provider, name, cfgJSON, secret string) (store.Account, error) {
	if strings.TrimSpace(name) == "" {
		return store.Account{}, errors.New("give the account a name")
	}
	if kind != "imap" && kind != "carddav" && kind != "caldav" {
		return store.Account{}, errors.New("unknown kind " + kind)
	}
	if !json.Valid([]byte(cfgJSON)) {
		return store.Account{}, errors.New("bad configuration")
	}
	return m.St.AddAccount(store.Account{Kind: kind, Provider: provider, Name: strings.TrimSpace(name), Config: cfgJSON, Secret: m.V.Seal([]byte(secret))})
}

// secret is what the connector logs in with: the password, or for an OAuth account a fresh access token (refreshed and re-sealed when it is about to expire).
func (m *Manager) secret(a store.Account) (string, error) {
	b, err := m.V.Open(a.Secret)
	if err != nil {
		return "", err
	}
	var oc struct {
		OAuth string `json:"oauth"`
	}
	json.Unmarshal([]byte(a.Config), &oc)
	if oc.OAuth == "" {
		return string(b), nil
	}
	var tok Token
	if err := json.Unmarshal(b, &tok); err != nil {
		return "", errors.New("the stored OAuth token is damaged: sign in again")
	}
	if tok.Expiry < time.Now().Unix()+90 {
		p := Providers()[oc.OAuth]
		if p == nil {
			return "", errors.New("the " + oc.OAuth + " sign-in is no longer configured on this server")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := p.Refresh(ctx, &tok); err != nil {
			return "", fmt.Errorf("could not renew the sign-in (%v): sign in again", err)
		}
		nb, _ := json.Marshal(tok)
		m.St.SetAccountSecret(a.ID, m.V.Seal(nb))
	}
	return Cred(tok.Access), nil
}

// AddOAuth creates the accounts a completed sign-in asked for (kinds: mail, contacts, calendar); each keeps its own sealed copy of the tokens.
func (m *Manager) AddOAuth(p *Provider, kinds []string, name string, tok Token) ([]store.Account, error) {
	if tok.Email == "" {
		return nil, errors.New("the provider did not tell which address signed in")
	}
	raw, _ := json.Marshal(tok)
	var out []store.Account
	for _, k := range kinds {
		var kind string
		var cfg any
		var label string
		switch k {
		case "mail":
			imap, smtp := p.IMAP, p.SMTP
			imap.User, smtp.User = tok.Email, tok.Email
			kind, label = "imap", "mail"
			cfg = MailCfg{IMAP: imap, SMTP: smtp, Folders: []string{"INBOX"}, Months: 3, From: tok.Email, OAuth: p.ID}
		case "contacts":
			if p.CardDAVURL == nil {
				continue
			}
			kind, label = "carddav", "contacts"
			cfg = DAVCfg{URL: p.CardDAVURL(tok.Email), User: tok.Email, OAuth: p.ID}
		case "calendar":
			if p.CalDAVURL == nil {
				continue
			}
			kind, label = "caldav", "calendar"
			cfg = DAVCfg{URL: p.CalDAVURL(tok.Email), User: tok.Email, OAuth: p.ID}
		default:
			continue
		}
		cj, _ := json.Marshal(cfg)
		nm := fmt.Sprintf("%s %s (%s)", p.Label, label, tok.Email)
		if name != "" {
			nm = fmt.Sprintf("%s %s", name, label)
		}
		a, err := m.St.AddAccount(store.Account{Kind: kind, Provider: p.ID, Name: nm, Config: string(cj), Secret: m.V.Seal(raw)})
		if err != nil {
			return out, err
		}
		out = append(out, a)
	}
	if len(out) == 0 {
		return nil, errors.New("nothing to connect")
	}
	return out, nil
}

// SyncNow syncs one account and records the outcome; it returns a one-line summary.
func (m *Manager) SyncNow(ctx context.Context, id string) (string, error) {
	a, ok := m.St.Account(id)
	if !ok {
		return "", errors.New("no such account")
	}
	m.mu.Lock()
	if m.busy[id] {
		m.mu.Unlock()
		return "", errors.New("already syncing")
	}
	m.busy[id] = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.busy, id); m.mu.Unlock() }()
	m.St.SetAccountStatus(id, "syncing", "", false)
	sum, err := m.sync(ctx, a)
	if err != nil {
		m.St.SetAccountStatus(id, "error", err.Error(), false)
		return sum, err
	}
	m.St.SetAccountStatus(id, "ok", "", true)
	return sum, nil
}

func (m *Manager) sync(ctx context.Context, a store.Account) (string, error) {
	if m.Hub == nil {
		return "", errors.New("the hub is not configured")
	}
	pass, err := m.secret(a)
	if err != nil {
		return "", errors.New("the stored secret cannot be opened (key changed?)")
	}
	switch a.Kind {
	case "imap":
		var c MailCfg
		if err := json.Unmarshal([]byte(a.Config), &c); err != nil {
			return "", err
		}
		n, err := SyncIMAP(ctx, a.ID, a.Name, c, pass, m.Hub, m.St, m.MaxMails)
		return fmt.Sprintf("%d new messages", n), err
	case "carddav", "caldav":
		var c DAVCfg
		if err := json.Unmarshal([]byte(a.Config), &c); err != nil {
			return "", err
		}
		kind := map[string]string{"carddav": "card", "caldav": "cal"}[a.Kind]
		ch, rm, err := SyncDAV(ctx, a.ID, kind, c, pass, m.Hub, m.St)
		return fmt.Sprintf("%d changed, %d removed", ch, rm), err
	}
	return "", errors.New("unknown kind")
}

// Remove deletes the account, its sync memory and its links (what was already copied into the hub stays there).
func (m *Manager) Remove(id string) {
	m.St.RemoveAccount(id)
	m.St.UnlinkSource(id)
}

// Loop syncs every account each interval until ctx ends.
func (m *Manager) Loop(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		for _, a := range m.St.Accounts() {
			c, cancel := context.WithTimeout(ctx, 5*time.Minute)
			m.SyncNow(c, a.ID)
			cancel()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Send relays a message through an imap account and files the copy in its Sent folder.
func (m *Manager) Send(accountID string, to []string, subject, body string) error {
	a, ok := m.St.Account(accountID)
	if !ok || a.Kind != "imap" {
		return errors.New("no such mail account")
	}
	var c MailCfg
	if err := json.Unmarshal([]byte(a.Config), &c); err != nil {
		return err
	}
	if c.SMTP.Host == "" || c.From == "" {
		return errors.New("this account has no SMTP server or sender address configured")
	}
	pass, err := m.secret(a)
	if err != nil {
		return err
	}
	return Relay(c, pass, c.From, to, BuildMessage(c.From, to, subject, body))
}

// MailAccounts lists the accounts that can send (an IMAP account with an SMTP server and a sender address).
func (m *Manager) MailAccounts() []proto.MailAccount {
	var out []proto.MailAccount
	for _, a := range m.St.Accounts() {
		if a.Kind != "imap" {
			continue
		}
		var c MailCfg
		if json.Unmarshal([]byte(a.Config), &c) == nil && c.SMTP.Host != "" && c.From != "" {
			out = append(out, proto.MailAccount{ID: a.ID, Name: a.Name, From: c.From})
		}
	}
	return out
}

// SendAs is Send under the name the server's MailSender interface uses.
func (m *Manager) SendAs(account string, to []string, subject, body string) error {
	return m.Send(account, to, subject, body)
}

// DAV returns the configuration and password of a CardDAV account (for writes that go back to the device).
func (m *Manager) DAV(accountID string) (DAVCfg, string, error) {
	a, ok := m.St.Account(accountID)
	if !ok || a.Kind != "carddav" {
		return DAVCfg{}, "", errors.New("no such contacts account")
	}
	var c DAVCfg
	if err := json.Unmarshal([]byte(a.Config), &c); err != nil {
		return c, "", err
	}
	p, err := m.secret(a)
	return c, p, err
}
