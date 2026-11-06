// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package fakes holds stand-ins for the outside services Cloud Companion talks to (Nextcloud, Immich, Matrix, XMPP, IRC, Signal, SIP),
// for the browser suites (web/test): each answers just what the suites send, the way the real service does, so CI downloads and starts
// nothing but the app and this program. The real services stay in the integration scripts (testdata/*/test.sh), run by hand when one
// of them updates.
package fakes

import (
	"context"
	"sort"
	"sync"
)

// A Fake is one service: Run serves it until ctx ends.
type Fake struct {
	Name string
	Run  func(ctx context.Context) error
}

var (
	mu  sync.Mutex
	all = map[string]Fake{}
)

// Register adds a fake (from its package's init).
func Register(f Fake) {
	mu.Lock()
	defer mu.Unlock()
	all[f.Name] = f
}

// All lists the fakes by name.
func All() []Fake {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Fake, 0, len(all))
	for _, f := range all {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
