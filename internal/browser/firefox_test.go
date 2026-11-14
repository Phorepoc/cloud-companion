// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
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
	msgs []map[string]any
	rep  map[string]string // command -> reply value (JSON), instead of true
}

func (e *fakeExt) write(_ context.Context, b []byte) error {
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	c := m["c"].(string)
	e.mu.Lock()
	e.msgs = append(e.msgs, m)
	v := "true"
	if r, ok := e.rep[c]; ok {
		v = r
	}
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

func (e *fakeExt) sent(c string) []map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []map[string]any
	for _, m := range e.msgs {
		if m["c"] == c {
			out = append(out, m)
		}
	}
	return out
}

func opened(t *testing.T, rep map[string]string) (*Firefox, *fakeExt, Session) {
	t.Helper()
	f := NewFirefox(0)
	e := &fakeExt{f: f, rep: rep}
	f.Attach(e.write)
	s, err := f.NewFor(context.Background(), "dev1")
	if err != nil {
		t.Fatal(err)
	}
	return f, e, s
}

func TestFirefoxViewOpensInTheDevicesContainer(t *testing.T) {
	_, e, _ := opened(t, nil)
	if o := e.sent("pOpen"); len(o) != 1 || o[0]["dev"] != "dev1" {
		t.Fatalf("pOpen %v", o)
	}
}

func TestFirefoxCookiesAreTheContainers(t *testing.T) {
	ctx := context.Background()
	jar := `[{"name":"who","value":"alice","domain":"site","path":"/","expires":-1,"size":8,"httpOnly":false,"secure":true,"session":true,"sameSite":""}]`
	_, e, s := opened(t, map[string]string{"pCookies": jar})
	got, err := s.Cookies(ctx)
	if err != nil || string(got) != jar {
		t.Fatalf("got %s %v", got, err)
	}
	if err := s.SetCookies(ctx, []byte(jar)); err != nil {
		t.Fatal(err)
	}
	set := e.sent("pSetCookies")
	if len(set) != 1 {
		t.Fatalf("pSetCookies %v", set)
	}
	var want any
	_ = json.Unmarshal([]byte(jar), &want)
	if !reflect.DeepEqual(set[0]["cookies"], want) {
		t.Fatalf("sent %v", set[0]["cookies"])
	}
}

func TestFirefoxTapIsTrustedAndFallsBack(t *testing.T) {
	ctx := context.Background()
	_, e, s := opened(t, nil)
	if err := s.Tap(ctx, 10, 20, -1); err != nil {
		t.Fatal(err)
	}
	if len(e.sent("pClick")) != 1 || len(e.sent("pEval")) != 0 {
		t.Fatalf("trusted tap: %s", e.log())
	}
	_, e, s = opened(t, map[string]string{"pClick": "false"}) // no privileged path in this Firefox
	if err := s.Tap(ctx, 10, 20, -1); err != nil {
		t.Fatal(err)
	}
	if len(e.sent("pEval")) != 1 {
		t.Fatalf("fallback: %s", e.log())
	}
}

func TestFirefoxCertificateContinueAnyway(t *testing.T) {
	ctx := context.Background()
	bad := `{"cert":{"url":"https://bad/","text":"SSL_ERROR_BAD_CERT_DOMAIN"}}`
	for _, ok := range []bool{true, false} {
		_, e, s := opened(t, map[string]string{"pNav": bad})
		var asked Dialog
		s.SetHandlers(Handlers{Dialog: func(d Dialog) Answer { asked = d; return Answer{OK: ok} }})
		if err := s.Navigate(ctx, "https://bad/", ""); err != nil {
			t.Fatal(err)
		}
		if asked.Kind != "cert" || !strings.Contains(asked.Message, "https://bad/") {
			t.Fatalf("asked %+v", asked)
		}
		if got := len(e.sent("pCert")); (got == 1) != ok {
			t.Fatalf("ok=%v pCert=%d: %s", ok, got, e.log())
		}
	}
}

func TestFirefoxDownloadReachesThePhone(t *testing.T) {
	f, _, s := opened(t, nil)
	got := make(chan Download, 1)
	s.SetHandlers(Handlers{Download: func(d Download) { got <- d }})
	tab := s.(*ffSession).tab
	if !f.Handle([]byte(fmt.Sprintf(`{"t":"pdl","tab":%d,"url":"data:,hi","name":"hello.txt"}`, tab))) {
		t.Fatal("not taken")
	}
	if d := <-got; d.Name != "hello.txt" || d.URL != "data:,hi" {
		t.Fatalf("%+v", d)
	}
}
