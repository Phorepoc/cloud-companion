// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// fakeExt answers the commands as the extension does; it records them in order.
type fakeExt struct {
	f    *Firefox
	mu   sync.Mutex
	cmds []string
	tab  int
}

func (e *fakeExt) write(_ context.Context, b []byte) error {
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	c := m["c"].(string)
	e.mu.Lock()
	v := "true"
	if c == "pOpen" {
		e.tab++
		v = fmt.Sprint(e.tab)
	}
	if c == "pNav" {
		c += " " + fmt.Sprint(m["url"])
	}
	e.cmds = append(e.cmds, c)
	e.mu.Unlock()
	go e.f.Handle([]byte(fmt.Sprintf(`{"t":"r","r":%v,"v":%s}`, m["r"], v)))
	return nil
}

func (e *fakeExt) log() string { e.mu.Lock(); defer e.mu.Unlock(); return strings.Join(e.cmds, ",") }

func TestFirefoxOldConnectionLeavesNewOne(t *testing.T) {
	f := NewFirefox(0)
	old, cur := &fakeExt{f: f}, &fakeExt{f: f}
	n1 := f.Attach(old.write)
	f.Attach(cur.write)
	f.Detach(n1) // the old socket's cleanup, after the new one connected
	if _, err := f.New(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cur.log() != "pOpen" || old.log() != "" {
		t.Fatalf("new %q old %q", cur.log(), old.log())
	}
}

func TestFirefoxViewOpensAgainAfterRestart(t *testing.T) {
	ctx := context.Background()
	f := NewFirefox(0)
	a := &fakeExt{f: f}
	n := f.Attach(a.write)
	s, err := f.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Open(ctx, "https://example.com/", 360, 1); err != nil {
		t.Fatal(err)
	}
	f.Detach(n) // Firefox restarted
	if err := s.ScrollTo(ctx, 10); err == nil {
		t.Fatal("no Firefox: the command must fail")
	}
	b := &fakeExt{f: f}
	f.Attach(b.write)
	if err := s.ScrollTo(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if got, want := b.log(), "pOpen,pSite,pView,pNav https://example.com/,pSettle,pEval"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if s.(*ffSession).tab != 1 {
		t.Fatal("the view must be the new tab")
	}
}
