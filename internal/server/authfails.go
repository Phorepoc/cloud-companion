// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net"
	"sync"
	"time"
)

// authFails counts failed sign-ins and pairings per client address: ten in a minute block that address for the rest of the minute (guessing a pairing code or a device
// key over the phone port gets no faster than that). Addresses of the same host with different ports count together.
type authFails struct {
	mu sync.Mutex
	m  map[string][]time.Time
}

const (
	authFailLimit  = 10
	authFailWindow = time.Minute
)

func hostOf(remote string) string {
	if h, _, err := net.SplitHostPort(remote); err == nil {
		return h
	}
	return remote
}

func (f *authFails) prune(h string, now time.Time) []time.Time {
	var keep []time.Time
	for _, t := range f.m[h] {
		if now.Sub(t) < authFailWindow {
			keep = append(keep, t)
		}
	}
	if len(keep) == 0 {
		delete(f.m, h)
	} else {
		f.m[h] = keep
	}
	return keep
}

func (f *authFails) blocked(remote string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.prune(hostOf(remote), time.Now())) >= authFailLimit
}

func (f *authFails) add(remote string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = map[string][]time.Time{}
	}
	h := hostOf(remote)
	f.prune(h, time.Now())
	f.m[h] = append(f.m[h], time.Now())
}
