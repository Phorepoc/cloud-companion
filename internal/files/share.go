// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package files

import (
	"errors"
	"sync"
	"time"
)

// Share is a public link to one file: anyone with the link can download it until it expires or the owner removes it.
type Share struct {
	Token   string    `json:"token"`
	Path    string    `json:"path"`
	Created time.Time `json:"created"`
	Expires time.Time `json:"expires,omitempty"` // zero = never
}

// Shares keeps the links; Load and Save persist them (the server stores them as a setting).
type Shares struct {
	mu   sync.Mutex
	list []Share
	Save func([]Share)
}

// NewShares starts from a saved list.
func NewShares(saved []Share, save func([]Share)) *Shares { return &Shares{list: saved, Save: save} }

func (sh *Shares) live() []Share {
	now, out := time.Now(), []Share{}
	for _, s := range sh.list {
		if s.Expires.IsZero() || now.Before(s.Expires) {
			out = append(out, s)
		}
	}
	return out
}

// List returns the links that have not expired.
func (sh *Shares) List() []Share {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return sh.live()
}

// Add makes a link to p for days days (0 = no end).
func (sh *Shares) Add(p string, days int) (Share, error) {
	if days < 0 || days > 3650 {
		return Share{}, errors.New("a link lasts from 1 day to 10 years, or always")
	}
	s := Share{Token: token(), Path: p, Created: time.Now().UTC()}
	if days > 0 {
		s.Expires = s.Created.Add(time.Duration(days) * 24 * time.Hour)
	}
	sh.mu.Lock()
	sh.list = append(sh.live(), s)
	l := append([]Share(nil), sh.list...)
	sh.mu.Unlock()
	if sh.Save != nil {
		sh.Save(l)
	}
	return s, nil
}

// Remove deletes a link.
func (sh *Shares) Remove(tok string) {
	sh.mu.Lock()
	out := []Share{}
	for _, s := range sh.live() {
		if s.Token != tok {
			out = append(out, s)
		}
	}
	sh.list = out
	l := append([]Share(nil), out...)
	sh.mu.Unlock()
	if sh.Save != nil {
		sh.Save(l)
	}
}

// Get returns the path of a live link.
func (sh *Shares) Get(tok string) (string, bool) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	for _, s := range sh.live() {
		if s.Token == tok {
			return s.Path, true
		}
	}
	return "", false
}
