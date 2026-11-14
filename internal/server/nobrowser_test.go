// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package server

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/phorepoc/cloud-companion/internal/browser"
)

// A server whose browser could not start (no display, a 2 GB board) runs on: a page gets the reason, nothing panics, and the
// tab stays usable for when it is tried again.
func TestNoBrowser(t *testing.T) {
	why := errors.New("starting the browser: no display")
	s := &session{srv: &Server{Browser: browser.Unavailable{Err: why}}, c: &conn{io: &fakeIO{}}, log: slog.Default()}
	tb := &tab{id: 1}
	for i := 0; i < 2; i++ {
		if err := s.startBrowser(context.Background(), tb); !errors.Is(err, why) {
			t.Fatalf("try %d: %v", i, err)
		}
		if tb.br != nil {
			t.Fatal("a tab holds a browser that never started")
		}
	}
}
