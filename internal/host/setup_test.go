// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// boardDocker is a fake Docker whose phe-cloud health the test sets.
type boardDocker struct {
	fakeDocker
	hmu    sync.Mutex
	health string
}

func (d *boardDocker) Run(ctx context.Context, dir string, args []string, out io.Writer) error {
	if args[0] == "inspect" {
		d.hmu.Lock()
		defer d.hmu.Unlock()
		io.WriteString(out, d.health+"\n")
		return nil
	}
	if args[0] == "logs" {
		io.WriteString(out, "panic: the database is locked\n")
		return nil
	}
	return d.fakeDocker.Run(ctx, dir, args, out)
}

func (d *boardDocker) set(h string) { d.hmu.Lock(); d.health = h; d.hmu.Unlock() }

type clock struct{ t time.Time }

func (c *clock) now() time.Time        { return c.t }
func (c *clock) add(d time.Duration)   { c.t = c.t.Add(d) }
func newClient() *http.Client          { j, _ := cookiejar.New(nil); return &http.Client{Jar: j} }
func (c *clock) uptime() time.Duration { return c.t.Sub(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) }

func newSetup(t *testing.T, code string) (*Setup, *boardDocker, *clock, *httptest.Server) {
	t.Helper()
	d := &boardDocker{health: "missing"}
	dir := t.TempDir()
	m := &Manager{Cat: realCatalogue(t), Board: Board{Model: "Raspberry Pi 5", Arch: "arm64", Memory: 8 << 30}, ComposeDir: dir, StateDir: dir, Run: d}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	ck := &clock{time.Date(2026, 1, 1, 0, 1, 0, 0, time.UTC)} // a minute after boot
	s := &Setup{M: m, Uptime: ck.uptime, Now: ck.now, Window: 30 * time.Minute, Unhealthy: 5 * time.Minute, HandOver: 10 * time.Minute}
	if code != "" {
		s.CodeFile = filepath.Join(dir, "cloud-setup.txt")
		os.WriteFile(s.CodeFile, []byte("# from Raspberry Pi Imager\ncode="+code+"\n"), 0o644)
	}
	s.Start()
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return s, d, ck, srv
}

func post(t *testing.T, c *http.Client, srv *httptest.Server, path, body string) int {
	t.Helper()
	res, err := c.Post(srv.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func getState(t *testing.T, c *http.Client, srv *httptest.Server) map[string]any {
	t.Helper()
	res, err := c.Get(srv.URL + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSetupFlow(t *testing.T) {
	s, d, ck, srv := newSetup(t, "PINE-APPLE-42")
	owner, stranger := newClient(), newClient()

	st := getState(t, owner, srv)
	if st["phase"] != PhaseSetup || st["unlocked"] != false || st["needsCode"] != true || st["features"] != nil {
		t.Fatalf("first look: %v", st)
	}
	if post(t, stranger, srv, "/api/setup", `{"passphrase":"long enough please"}`) != 403 {
		t.Error("setup without the code")
	}
	if post(t, stranger, srv, "/api/unlock", `{"secret":"WRONG"}`) != 403 {
		t.Error("a wrong code unlocked")
	}
	if post(t, owner, srv, "/api/unlock", `{"secret":"PINE-APPLE-42"}`) != 200 {
		t.Fatal("the right code did not unlock")
	}
	st = getState(t, owner, srv)
	if st["unlocked"] != true || len(st["features"].([]any)) == 0 {
		t.Fatalf("unlocked: %v", st)
	}
	for _, f := range st["features"].([]any) {
		if id := f.(map[string]any)["id"]; id == "core" || id == "tls" {
			t.Errorf("%s is offered in setup", id)
		}
	}
	if post(t, owner, srv, "/api/setup", `{"passphrase":"correct horse battery","features":["tls"]}`) != 400 {
		t.Error("HTTPS taken in setup: it needs the setup page's port")
	}
	if post(t, owner, srv, "/api/setup", `{"passphrase":"short","features":[]}`) != 400 {
		t.Error("a short passphrase was taken")
	}
	if post(t, owner, srv, "/api/setup", `{"passphrase":"correct horse battery","features":["signal"],"timeZone":"Not/AZone"}`) != 400 {
		t.Error("a bad time zone was taken")
	}
	if post(t, owner, srv, "/api/setup", `{"passphrase":"correct horse battery","features":["signal"],"timeZone":"Europe/Athens"}`) != 202 {
		t.Fatal("setup refused")
	}
	<-s.job.Done()
	if !s.M.SetUp() {
		t.Fatal("not set up")
	}
	// done only when Cloud Companion is healthy
	s.Tick(context.Background())
	if s.Phase() != PhaseSetup {
		t.Fatalf("done before Cloud Companion is healthy: %s", s.Phase())
	}
	d.set("healthy")
	s.Tick(context.Background())
	st = getState(t, owner, srv)
	if st["phase"] != PhaseDone || !strings.Contains(st["open"].(string), ":8080/#/pair?code="+s.M.Value("core", "first-owner")) || s.M.Value("core", "first-owner") == "" {
		t.Fatalf("hand-over: %v", st)
	}
	if getState(t, stranger, srv)["open"] != nil {
		t.Error("the owner's link was shown to someone without the code")
	}
	ck.add(11 * time.Minute)
	s.Tick(context.Background())
	if s.Phase() != PhaseOff {
		t.Fatalf("the page stayed: %s", s.Phase())
	}

	// recovery: unhealthy for more than 5 minutes
	d.set("unhealthy")
	s.Tick(context.Background())
	ck.add(4 * time.Minute)
	s.Tick(context.Background())
	if s.Phase() != PhaseOff {
		t.Fatal("recovery too early")
	}
	ck.add(2 * time.Minute)
	s.Tick(context.Background())
	if s.Phase() != PhaseRecovery {
		t.Fatalf("no recovery: %s", s.Phase())
	}
	if getState(t, owner, srv)["unlocked"] != false {
		t.Error("the setup's unlock still counts in recovery")
	}
	if post(t, owner, srv, "/api/unlock", `{"secret":"PINE-APPLE-42"}`) != 403 {
		t.Error("the setup code unlocked recovery")
	}
	if post(t, owner, srv, "/api/unlock", `{"secret":"correct horse battery"}`) != 200 {
		t.Fatal("the passphrase did not unlock recovery")
	}
	res, _ := owner.Get(srv.URL + "/api/logs")
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(b), "database is locked") {
		t.Errorf("logs: %s", b)
	}
	if post(t, owner, srv, "/api/retry", "") != 202 {
		t.Fatal("retry refused")
	}
	<-s.job.Done()
	d.set("healthy")
	s.Tick(context.Background())
	if s.Phase() != PhaseOff {
		t.Fatalf("still in recovery: %s", s.Phase())
	}
	if post(t, owner, srv, "/api/retry", "") != 403 {
		t.Error("recovery calls work when there is no recovery")
	}
}

func TestSetupWindowAndLockout(t *testing.T) {
	_, _, ck, srv := newSetup(t, "")
	c := newClient()
	if st := getState(t, c, srv); st["windowOpen"] != true || st["needsCode"] != false {
		t.Fatalf("window: %v", st)
	}
	ck.add(31 * time.Minute)
	if post(t, c, srv, "/api/unlock", `{}`) != 403 {
		t.Error("unlocked after the window closed")
	}
	ck.add(-31 * time.Minute)
	if post(t, c, srv, "/api/unlock", `{}`) != 200 {
		t.Fatal("the open window did not unlock")
	}

	_, _, _, srv2 := newSetup(t, "PINE-APPLE-42")
	for i := 0; i < 10; i++ {
		post(t, c, srv2, "/api/unlock", `{"secret":"guess"}`)
	}
	if code := post(t, c, srv2, "/api/unlock", `{"secret":"PINE-APPLE-42"}`); code != 429 {
		t.Errorf("after 10 wrong codes: %d", code)
	}
}

func TestSetupStartsOffWhenSetUp(t *testing.T) {
	s, _, _, _ := newSetup(t, "")
	os.WriteFile(filepath.Join(s.M.StateDir, "state.json"), []byte(`{"features":{"core":{}}}`), 0o600)
	s.Start()
	if s.Phase() != PhaseOff {
		t.Errorf("a set up board starts in %s", s.Phase())
	}
}
