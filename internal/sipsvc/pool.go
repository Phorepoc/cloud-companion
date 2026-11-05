// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package sipsvc

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
)

// Pool runs a Service for every SIP service account and adds it to the hub as a connection; an account that is removed or changed
// is stopped (and started again with its new settings).
type Pool struct {
	St     *store.Store
	Hub    *chat.Hub
	Log    *slog.Logger
	Secret func(store.Account) (string, error) // opens the account's password

	run map[string]running
}

type running struct {
	svc *Service
	key string // config + secret it was started with
}

// Sync brings the running services in line with the accounts.
func (p *Pool) Sync() {
	if p.run == nil {
		p.run = map[string]running{}
	}
	want := map[string]bool{}
	for _, a := range p.St.Accounts() {
		if a.Kind != "sip" {
			continue
		}
		want[a.ID] = true
		key := a.Config + "\x00" + string(a.Secret)
		if r, ok := p.run[a.ID]; ok {
			if r.key == key {
				continue
			}
			p.stop(a.ID)
		}
		var c Cfg
		if err := json.Unmarshal([]byte(a.Config), &c); err != nil {
			p.St.SetAccountStatus(a.ID, "error", "bad configuration", false)
			continue
		}
		pass, err := p.Secret(a)
		if err != nil {
			p.St.SetAccountStatus(a.ID, "error", "the stored password cannot be opened (key changed?)", false)
			continue
		}
		id := a.ID
		s := &Service{Acc: id, Name: a.Name, Cfg: c, Pass: pass, Hub: p.Hub, Log: p.Log, Status: func(err error) {
			if err != nil {
				p.St.SetAccountStatus(id, "error", err.Error(), false)
			} else {
				p.St.SetAccountStatus(id, "ok", "", true)
			}
		}}
		if err := s.Start(); err != nil {
			p.St.SetAccountStatus(id, "error", err.Error(), false)
			p.run[id] = running{key: key} // not again until the settings change
			continue
		}
		p.Hub.Add(s)
		p.run[id] = running{svc: s, key: key}
		p.Log.Info("sip service: connected", "account", a.Name, "listen", s.Cfg.Listen)
	}
	for id := range p.run {
		if !want[id] {
			p.stop(id)
		}
	}
}

func (p *Pool) stop(id string) {
	r := p.run[id]
	delete(p.run, id)
	if r.svc != nil {
		p.Hub.Remove(r.svc.ID())
		r.svc.Stop()
	}
}

// Run syncs now and every interval until ctx ends, then stops every service.
func (p *Pool) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		p.Sync()
		select {
		case <-ctx.Done():
			for id := range p.run {
				p.stop(id)
			}
			return
		case <-t.C:
		}
	}
}
