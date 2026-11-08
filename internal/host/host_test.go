// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

func realCatalogue(t *testing.T) *Catalogue {
	t.Helper()
	c, err := LoadCatalogue(os.DirFS("../../deploy/features"), ".")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCatalogueShipped(t *testing.T) {
	c := realCatalogue(t)
	if c.Get("core") == nil || !c.Get("core").Core {
		t.Fatal("core missing or not always on")
	}
	for _, id := range c.IDs() {
		for _, x := range c.Get(id).Files {
			if _, err := os.Stat(filepath.Join("../..", x)); err != nil {
				t.Errorf("%s: compose file %s: %v", id, x, err)
			}
		}
	}
	ids, err := c.Closure([]string{"hub-legacy"})
	if err != nil || !slices.Equal(ids, []string{"core", "hub", "hub-legacy"}) {
		t.Errorf("closure = %v, %v; want core, hub, hub-legacy", ids, err)
	}
}

func TestCatalogueErrors(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"unknown requirement": {"a.yaml": "id: a\ntitle: A\nrequires: [b]\n"},
		"cycle":               {"a.yaml": "id: a\ntitle: A\nrequires: [b]\n", "b.yaml": "id: b\ntitle: B\nrequires: [a]\n"},
		"twice":               {"a.yaml": "id: a\ntitle: A\n", "b.yaml": "id: a\ntitle: B\n"},
		"bad size":            {"a.yaml": "id: a\ntitle: A\nneeds: { memory: lots }\n"},
	} {
		fsys := fstest.MapFS{}
		for n, s := range files {
			fsys[n] = &fstest.MapFile{Data: []byte(s)}
		}
		if _, err := LoadCatalogue(fsys, "."); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

func TestFits(t *testing.T) {
	c := realCatalogue(t)
	pi4 := Board{Arch: "arm64", Memory: 2 << 30}
	if ok, _ := c.Get("android").Fits(pi4); ok {
		t.Error("Android apps fit a 2 GB arm64 board")
	}
	if ok, why := c.Get("signal").Fits(pi4); !ok {
		t.Errorf("Signal does not fit a 2 GB Pi 4: %s", why)
	}
}

func TestComposeArgs(t *testing.T) {
	c := realCatalogue(t)
	ids, _ := c.Closure([]string{"postgres", "signal"})
	got := strings.Join(ComposeArgs(c, ids), " ")
	want := "compose -p cloud -f docker-compose.yml -f docker-compose.board.yml -f docker-compose.db.yml --profile db-postgres --profile signal up -d --pull missing --remove-orphans --wait"
	if got != want {
		t.Errorf("args\n got %s\nwant %s", got, want)
	}
}

// fakeDocker records the compose command lines and fails when told to.
type fakeDocker struct {
	mu   sync.Mutex
	runs [][]string
	cfg  []string // the services of the last config
	up   []string // the services running (all ever started: compose up leaves the switched-off ones)
	fail func(args []string) bool
}

func (f *fakeDocker) Run(ctx context.Context, dir string, args []string, out io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if slices.Contains(args, "config") { // the services of the profiles named
		svc := map[string]string{"db-postgres": "postgres", "db-mariadb": "mariadb", "signal": "signal-cli", "hub": "stalwart zpush",
			"hub-legacy": "legacytls", "asterisk": "asterisk", "tls": "caddy", "android": "redroid dockerproxy android"}
		io.WriteString(out, "phecloud\n")
		f.cfg = []string{"phecloud"}
		for i, a := range args {
			if a == "--profile" {
				io.WriteString(out, strings.ReplaceAll(svc[args[i+1]], " ", "\n")+"\n")
				f.cfg = append(f.cfg, strings.Fields(svc[args[i+1]])...)
			}
		}
		return nil
	}
	if args[0] == "ps" { // the project's containers: what the last up started
		for _, svc := range f.up {
			io.WriteString(out, "id-"+svc+" "+svc+"\n")
		}
		return nil
	}
	f.runs = append(f.runs, args)
	if args[0] == "rm" {
		return nil
	}
	// a failed up --wait leaves what it started
	for _, x := range servicesOf(args) {
		if !slices.Contains(f.up, x) {
			f.up = append(f.up, x)
		}
	}
	if f.fail != nil && f.fail(args) {
		io.WriteString(out, "Error: container failed to start\n")
		return errors.New("exit status 1")
	}
	io.WriteString(out, "Container started\n")
	return nil
}

func newTestManager(t *testing.T, d *fakeDocker) (*Manager, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	m := &Manager{Cat: realCatalogue(t), Board: Board{Arch: "arm64", Memory: 8 << 30}, ComposeDir: dir, StateDir: dir, Run: d}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(m.Handler())
	t.Cleanup(srv.Close)
	return m, srv
}

func call(t *testing.T, srv *httptest.Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func waitJob(t *testing.T, srv *httptest.Server, id any) map[string]any {
	t.Helper()
	_, st := call(t, srv, "GET", "/v1/jobs/"+id.(string)+"?wait=1", "")
	return st
}

func featureOn(t *testing.T, srv *httptest.Server, id string) bool {
	t.Helper()
	_, out := call(t, srv, "GET", "/v1/features", "")
	for _, f := range out["features"].([]any) {
		if m := f.(map[string]any); m["id"] == id {
			return m["on"].(bool)
		}
	}
	t.Fatalf("no feature %s", id)
	return false
}

func TestEnableDisable(t *testing.T) {
	d := &fakeDocker{}
	m, srv := newTestManager(t, d)

	code, j := call(t, srv, "POST", "/v1/features/postgres", "")
	if code != http.StatusAccepted {
		t.Fatalf("enable: %d %v", code, j)
	}
	if st := waitJob(t, srv, j["id"]); st["state"] != "done" {
		t.Fatalf("job: %v", st)
	}
	if !featureOn(t, srv, "postgres") || !featureOn(t, srv, "core") {
		t.Error("postgres (and core) not on")
	}
	env, _ := os.ReadFile(filepath.Join(m.ComposeDir, ".env"))
	if !strings.Contains(string(env), `PHECLOUD_DB="postgres://phecloud:`) || strings.Contains(string(env), "${") {
		t.Errorf(".env without the database address:\n%s", env)
	}
	// the generated password is kept: switching another feature on does not change it
	pw := m.state.Features["postgres"]["password"]
	_, j = call(t, srv, "POST", "/v1/features/signal", "")
	waitJob(t, srv, j["id"])
	if pw == "" || m.state.Features["postgres"]["password"] != pw {
		t.Error("the database password changed")
	}
	// and it is never shown
	_, out := call(t, srv, "GET", "/v1/features", "")
	if b, _ := json.Marshal(out); strings.Contains(string(b), pw) {
		t.Error("the password is in the feature list")
	}

	if code, _ := call(t, srv, "POST", "/v1/features/mariadb", ""); code != http.StatusConflict {
		t.Errorf("MariaDB beside PostgreSQL: %d", code)
	}
	if code, _ := call(t, srv, "DELETE", "/v1/features/core", ""); code != http.StatusConflict {
		t.Errorf("core switched off: %d", code)
	}
	_, j = call(t, srv, "POST", "/v1/features/hub-legacy", "")
	waitJob(t, srv, j["id"])
	if code, _ := call(t, srv, "DELETE", "/v1/features/hub", ""); code != http.StatusConflict {
		t.Errorf("hub switched off under hub-legacy: %d", code)
	}
	_, j = call(t, srv, "DELETE", "/v1/features/signal", "")
	waitJob(t, srv, j["id"])
	if featureOn(t, srv, "signal") {
		t.Error("signal still on")
	}
	up, rm := strings.Join(d.runs[len(d.runs)-2], " "), d.runs[len(d.runs)-1]
	if strings.Contains(up, "--profile signal") || !strings.Contains(up, "--remove-orphans") {
		t.Errorf("up after switching Signal off: %s", up)
	}
	// Signal's container is stopped and removed (with the others that are off); the ones that are on stay
	if !slices.Equal(rm, []string{"rm", "--force", "id-signal-cli"}) {
		t.Errorf("rm after switching Signal off: %v", rm)
	}
	if code, _ := call(t, srv, "POST", "/v1/features/nope", ""); code != http.StatusNotFound {
		t.Errorf("unknown feature: %d", code)
	}

	// what was chosen survives a restart of the manager
	m2 := &Manager{Cat: m.Cat, ComposeDir: m.ComposeDir, StateDir: m.StateDir, Run: d}
	if err := m2.Load(); err != nil || m2.state.Features["postgres"]["password"] != pw || m2.state.Features["signal"] != nil {
		t.Errorf("state after restart: %v %v", m2.state, err)
	}
}

func TestFailedChangeGoesBack(t *testing.T) {
	d := &fakeDocker{fail: func(args []string) bool { return slices.Contains(args, "signal") }}
	m, srv := newTestManager(t, d)
	_, j := call(t, srv, "POST", "/v1/features/signal", "")
	st := waitJob(t, srv, j["id"])
	if st["state"] != "failed" || !strings.Contains(strings.Join(toStrings(st["log"]), "\n"), "container failed to start") {
		t.Fatalf("job: %v", st)
	}
	if featureOn(t, srv, "signal") || m.state.Features["signal"] != nil {
		t.Error("signal is on after its start failed")
	}
	if up := strings.Join(d.runs[len(d.runs)-2], " "); strings.Contains(up, "--profile signal") || !slices.Contains(d.runs[len(d.runs)-1], "id-signal-cli") {
		t.Errorf("not put back: %s / %v", up, d.runs[len(d.runs)-1])
	}
	if _, err := os.Stat(filepath.Join(m.StateDir, "state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the failed state was saved")
	}
}

// servicesOf is what an up of these profiles starts
func servicesOf(args []string) []string {
	svc := map[string]string{"db-postgres": "postgres", "db-mariadb": "mariadb", "signal": "signal-cli", "hub": "stalwart zpush",
		"hub-legacy": "legacytls", "asterisk": "asterisk", "tls": "caddy", "android": "redroid dockerproxy android"}
	out := []string{"phecloud"}
	for i, a := range args {
		if a == "--profile" {
			out = append(out, strings.Fields(svc[args[i+1]])...)
		}
	}
	return out
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestRootStorage(t *testing.T) {
	line := func(src string) string {
		return "22 1 179:2 / / rw,noatime shared:1 - ext4 " + src + " rw\n25 22 0:5 / /dev rw shared:2 - devtmpfs udev rw\n"
	}
	for src, want := range map[string]string{"/dev/mmcblk0p2": "sd", "/dev/nvme0n1p2": "nvme", "/dev/sda2": "usb", "overlay": ""} {
		if got := rootStorage(line(src)); got != want {
			t.Errorf("%s: %q, want %q", src, got, want)
		}
	}
}

// A job that has not written anything yet still has a log the pages can read: [] in JSON, not null (a null blanked the
// Backups page).
func TestNewJobLogIsEmptyList(t *testing.T) {
	m := &Manager{}
	m.Load()
	b, _ := json.Marshal(m.newJob("backup", "").Status())
	if !strings.Contains(string(b), `"log":[]`) {
		t.Errorf("%s", b)
	}
}

// Two features turned on close together (two browsers, or one right after the other) both stay on: the second change is
// worked out once the first is done, not from the state before it.
func TestChangesCloseTogetherBothStand(t *testing.T) {
	gate := make(chan struct{})
	var once sync.Once
	d := &fakeDocker{fail: func(args []string) bool {
		if slices.Contains(args, "up") {
			once.Do(func() { <-gate }) // the first change is still applying when the second comes
		}
		return false
	}}
	m, _ := newTestManager(t, d)
	j1, err := m.Choose([]string{"signal"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	j2, err := m.Choose([]string{"asterisk"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	close(gate)
	<-j1.Done()
	<-j2.Done()
	if j1.Status().State != "done" || j2.Status().State != "done" {
		t.Fatalf("jobs: %+v %+v", j1.Status(), j2.Status())
	}
	for _, id := range []string{"signal", "asterisk"} {
		if m.Value(id, "") == "" && !slices.Contains(m.enabled(m.cloneState()), id) {
			t.Errorf("%s was undone by the other change", id)
		}
	}
}
