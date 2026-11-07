// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// updateDocker is Docker for an update: Cloud Companion is healthy unless .env names the broken release.
type updateDocker struct {
	mu     sync.Mutex
	dir    string
	broken string
	calls  []string
}

func (d *updateDocker) Run(ctx context.Context, dir string, args []string, out io.Writer) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, strings.Join(args, " "))
	if args[0] == "inspect" {
		env, _ := os.ReadFile(filepath.Join(d.dir, ".env"))
		if d.broken != "" && strings.Contains(string(env), `PHECLOUD_VERSION="`+d.broken+`"`) {
			io.WriteString(out, "unhealthy\n")
		} else {
			io.WriteString(out, "healthy\n")
		}
	}
	return nil
}

func (d *updateDocker) RunEnv(ctx context.Context, dir string, env, args []string, out io.Writer) error {
	return d.Run(ctx, dir, args, out)
}

// fakeGitHub serves a release list and the board files of the releases that have them.
type fakeGitHub struct {
	srv   *httptest.Server
	files map[string][]byte // path: content
	list  []map[string]any
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	g := &fakeGitHub{files: map[string][]byte{}}
	g.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases" {
			json.NewEncoder(w).Encode(g.list)
			return
		}
		b, ok := g.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(g.srv.Close)
	return g
}

// release adds a release; with board files when compose is not "" (its docker-compose.yml), the manager's program being prog.
func (g *fakeGitHub) release(tag, notes, compose, prog string, corrupt bool) {
	r := map[string]any{"tag_name": tag, "body": notes, "assets": []map[string]string{}}
	if compose != "" {
		var tgz bytes.Buffer
		gz := gzip.NewWriter(&tgz)
		tw := tar.NewWriter(gz)
		for name, body := range map[string]string{"docker-compose.yml": compose, "VERSION": tag + "\n", "deploy/features/x.txt": "x"} {
			tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
			tw.Write([]byte(body))
		}
		tw.Close()
		gz.Close()
		sum := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
		sums := fmt.Sprintf("%s  cloud-board.tar.gz\n%s  phehost-linux-arm64\n", sum(tgz.Bytes()), sum([]byte(prog)))
		g.files["/dl/"+tag+"/cloud-board.tar.gz"] = tgz.Bytes()
		g.files["/dl/"+tag+"/phehost-linux-arm64"] = []byte(prog)
		if corrupt {
			g.files["/dl/"+tag+"/phehost-linux-arm64"] = []byte(prog + "!")
		}
		g.files["/dl/"+tag+"/SHA256SUMS.board"] = []byte(sums)
		var assets []map[string]string
		for _, f := range []string{"cloud-board.tar.gz", "phehost-linux-arm64", "SHA256SUMS.board"} {
			assets = append(assets, map[string]string{"name": f, "browser_download_url": g.srv.URL + "/dl/" + tag + "/" + f})
		}
		r["assets"] = assets
	}
	g.list = append([]map[string]any{r}, g.list...)
}

func newUpdateManager(t *testing.T, g *fakeGitHub) (*Manager, *updateDocker, *atomic.Int32) {
	t.Helper()
	dir := t.TempDir()
	compose, state := filepath.Join(dir, "cloud"), filepath.Join(dir, "state")
	os.MkdirAll(filepath.Join(compose, "deploy/hub"), 0o755)
	os.MkdirAll(state, 0o700)
	os.WriteFile(filepath.Join(compose, "docker-compose.yml"), []byte("old compose"), 0o644)
	os.WriteFile(filepath.Join(compose, "VERSION"), []byte("v0.3.0\n"), 0o644)
	os.WriteFile(filepath.Join(compose, "deploy/hub/.env.zpush"), []byte("written on the board"), 0o600)
	os.WriteFile(filepath.Join(state, "state.json"), []byte(`{"features":{"core":{}}}`), 0o600)
	bin := filepath.Join(dir, "phehost")
	os.WriteFile(bin, []byte("old manager"), 0o755)
	d := &updateDocker{dir: compose}
	restarts := &atomic.Int32{}
	m := &Manager{Cat: realCatalogue(t), Board: Board{Arch: "arm64"}, ComposeDir: compose, StateDir: state, Run: d,
		Releases: g.srv.URL + "/releases", HTTP: g.srv.Client(), Binary: bin, Restart: func() { restarts.Add(1) }, HealthWait: 200 * time.Millisecond}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m, d, restarts
}

func runUpdate(t *testing.T, m *Manager) JobStatus {
	t.Helper()
	if _, err := m.CheckUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	j, err := m.Update()
	if err != nil {
		t.Fatal(err)
	}
	<-j.Done()
	time.Sleep(3500 * time.Millisecond) // the hand-over waits for the page to read the end
	return j.Status()
}

func read(t *testing.T, name string) string {
	b, _ := os.ReadFile(name)
	return string(b)
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"v0.10.0", "v0.9.9", true}, {"v1.0.0", "v1.0.0", false}, {"v0.3.0", "", true}, {"v0.3.0-rc1", "v0.2.0", false}, {"v0.2.0", "v0.3.0", false}} {
		if newer(c.a, c.b) != c.want {
			t.Errorf("newer(%q, %q) != %v", c.a, c.b, c.want)
		}
	}
}

func TestCheckFindsNewestAndBoardFiles(t *testing.T) {
	g := newFakeGitHub(t)
	g.release("v0.3.0", "", "c3", "m3", false)
	g.release("v0.4.0", "## Features\n* **web:** [plain words](https://x/y) ([abc1234](https://x/c))", "c4", "m4", false)
	g.release("v0.5.0", "", "", "", false) // images only
	g.list = append(g.list, map[string]any{"tag_name": "v9.0.0", "draft": true})
	m, _, _ := newUpdateManager(t, g)
	c, err := m.CheckUpdate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.Available == nil || c.Available.Version != "v0.5.0" || c.Board == nil || c.Board.Version != "v0.4.0" {
		t.Fatalf("found: %+v %+v", c.Available, c.Board)
	}
	if n := c.Available.Notes; strings.Contains(n, "](") || strings.Contains(n, "#") {
		t.Errorf("notes not plain: %q", n)
	}
	// up to date
	m.saveUpdate(UpdateConfig{Images: "v0.5.0", Auto: true})
	os.WriteFile(filepath.Join(m.ComposeDir, "VERSION"), []byte("v0.4.0"), 0o644)
	if c, _ := m.CheckUpdate(context.Background()); c.Available != nil {
		t.Errorf("offered while up to date: %+v", c.Available)
	}
}

func TestUpdateInstallsAndHandsOver(t *testing.T) {
	g := newFakeGitHub(t)
	g.release("v0.4.0", "", "new compose", "new manager", false)
	m, d, restarts := newUpdateManager(t, g)
	if st := runUpdate(t, m); st.State != "done" {
		t.Fatalf("%+v", st)
	}
	if read(t, filepath.Join(m.ComposeDir, "docker-compose.yml")) != "new compose" || read(t, m.Binary) != "new manager" {
		t.Error("new files not in place")
	}
	if read(t, filepath.Join(m.ComposeDir, "deploy/hub/.env.zpush")) != "written on the board" {
		t.Error("a file written on the board was lost")
	}
	if m.Version() != "v0.4.0" || !strings.Contains(read(t, filepath.Join(m.ComposeDir, ".env")), `PHECLOUD_VERSION="v0.4.0"`) {
		t.Errorf("runs %s", m.Version())
	}
	if restarts.Load() != 1 {
		t.Error("the new manager did not take over")
	}
	if c := m.UpdateConfig(); c.Available != nil || c.Last == nil || !c.Last.OK || c.Last.From != "v0.3.0" {
		t.Errorf("after: %+v", c)
	}
	if !strings.Contains(strings.Join(d.calls, "\n"), "up -d --pull missing") {
		t.Error("the new images were not started")
	}
}

func TestUpdateGoesBackWhenUnhealthy(t *testing.T) {
	g := newFakeGitHub(t)
	g.release("v0.4.0", "", "new compose", "new manager", false)
	m, d, restarts := newUpdateManager(t, g)
	d.broken = "v0.4.0"
	st := runUpdate(t, m)
	if st.State != "failed" || !strings.Contains(st.Error, "went back to v0.3.0") {
		t.Fatalf("%+v", st)
	}
	if read(t, filepath.Join(m.ComposeDir, "docker-compose.yml")) != "old compose" || read(t, m.Binary) != "old manager" || m.Version() != "v0.3.0" {
		t.Error("not put back")
	}
	if !strings.Contains(read(t, filepath.Join(m.ComposeDir, ".env")), `PHECLOUD_VERSION="v0.3.0"`) {
		t.Error("the old images do not run again")
	}
	if restarts.Load() != 1 {
		t.Error("the old manager was not started again")
	}
	if c := m.UpdateConfig(); c.Last == nil || c.Last.OK || c.Available == nil {
		t.Errorf("a failed update is not kept to try again: %+v", c)
	}
}

func TestUpdateRefusesDamagedDownload(t *testing.T) {
	g := newFakeGitHub(t)
	g.release("v0.4.0", "", "new compose", "new manager", true)
	m, d, restarts := newUpdateManager(t, g)
	st := runUpdate(t, m)
	if st.State != "failed" || !strings.Contains(st.Error, "did not download whole") {
		t.Fatalf("%+v", st)
	}
	if read(t, m.Binary) != "old manager" || restarts.Load() != 0 || len(d.calls) != 0 {
		t.Error("something changed after a damaged download")
	}
}

func TestUpdateRefusesPlainHTTP(t *testing.T) {
	m := &Manager{Releases: "http://example.com/releases"}
	if _, _, err := m.latest(context.Background(), "", ""); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Errorf("%v", err)
	}
}

func TestUntarStaysInside(t *testing.T) {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "../evil", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
	tw.Write([]byte("x"))
	tw.Close()
	gz.Close()
	if err := untar(&b, t.TempDir()); err == nil {
		t.Error("a file outside the folder was taken")
	}
}
