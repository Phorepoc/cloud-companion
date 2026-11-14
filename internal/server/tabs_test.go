// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package server

import (
	"context"
	"log/slog"
	"reflect"
	"testing"

	"github.com/phorepoc/cloud-companion/internal/proto"
)

// The tabs as the person sees them: the page open before any New tab is tab 1, and closing the last tab leaves a blank one,
// never a session without a view (which answered every later action with "no view 1").
func TestTabsNewAndClose(t *testing.T) {
	s := &session{srv: &Server{}, c: &conn{io: &fakeIO{}}, log: slog.Default()}
	act := func(action string, v int) {
		t.Helper()
		raw, _ := proto.Marshal(proto.Tab{T: proto.TTab, Action: action, V: v})
		if err := s.tabAction(context.Background(), raw); err != nil {
			t.Fatalf("%s %d: %v", action, v, err)
		}
	}
	ids := func() []int {
		var out []int
		for id := 1; id <= 64; id++ {
			if s.tabs[id] != nil {
				out = append(out, id)
			}
		}
		return out
	}
	act("new", 0)
	if got := ids(); !reflect.DeepEqual(got, []int{1, 2}) || s.active != 2 {
		t.Fatalf("New tab on a fresh window: tabs %v, active %d; want [1 2], 2", got, s.active)
	}
	act("close", 2)
	if got := ids(); !reflect.DeepEqual(got, []int{1}) || s.active != 1 {
		t.Fatalf("closing the new tab: tabs %v, active %d; want [1], 1", got, s.active)
	}
	act("close", 1)
	if got := ids(); len(got) != 1 || s.active != got[0] || s.active == 1 || s.tab == nil {
		t.Fatalf("closing the last tab: tabs %v, active %d; want one blank tab, shown", got, s.active)
	}
	act("new", 0) // and the browser goes on working
	if len(s.tabs) != 2 {
		t.Fatalf("a new tab after that: %v", ids())
	}
}
