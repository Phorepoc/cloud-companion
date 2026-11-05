// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package chat

import (
	"context"
	"log/slog"
	"time"

	"github.com/phorepoc/cloud-companion/internal/store"
)

// Running is a connection that belongs to an account and runs until stopped (a SIP service, a Matrix account...).
type Running interface {
	Conn
	Stop()
}

// Starter starts the connection for an account of its kind; status reports how it is doing for the account's row (nil = ok).
type Starter func(a store.Account, secret string, status func(error)) (Running, error)

// Accounts runs a connection for every account of the kinds it knows and adds it to the hub; an account that is removed or changed
// is stopped (and started again with its new settings).
type Accounts struct {
	St     *store.Store
	Hub    *Hub
	Log    *slog.Logger
	Secret func(store.Account) (string, error) // opens the account's password or token
	Kinds  map[string]Starter

	run map[string]running
}

type running struct {
	c   Running
	key string // config + secret it was started with
}

// Sync brings the running connections in line with the accounts.
func (p *Accounts) Sync() {
	if p.run == nil {
		p.run = map[string]running{}
	}
	want := map[string]bool{}
	for _, a := range p.St.Accounts() {
		start := p.Kinds[a.Kind]
		if start == nil {
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
		id := a.ID
		status := func(err error) {
			if err != nil {
				p.St.SetAccountStatus(id, "error", err.Error(), false)
			} else {
				p.St.SetAccountStatus(id, "ok", "", true)
			}
		}
		secret, err := p.Secret(a)
		if err != nil {
			status(err)
			p.run[id] = running{key: key}
			continue
		}
		c, err := start(a, secret, status)
		if err != nil {
			status(err)
			p.run[id] = running{key: key} // not again until the settings change
			continue
		}
		p.Hub.Add(c)
		p.run[id] = running{c: c, key: key}
		p.Log.Info("chat: connected", "account", a.Name, "kind", a.Kind)
	}
	for id := range p.run {
		if !want[id] {
			p.stop(id)
		}
	}
}

func (p *Accounts) stop(id string) {
	r := p.run[id]
	delete(p.run, id)
	if r.c != nil {
		p.Hub.Remove(r.c.ID())
		r.c.Stop()
	}
}

// Run syncs now and every interval until ctx ends, then stops every connection.
func (p *Accounts) Run(ctx context.Context, every time.Duration) {
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
