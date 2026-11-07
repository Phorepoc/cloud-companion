// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// resticDocker is a fake Docker with two running containers and two volumes, and a restic that answers from its own state.
type resticDocker struct {
	mu       sync.Mutex
	calls    [][]string
	envs     [][]string
	failRun  bool
	password string // the repository's; "" = none there yet
	volumes  map[string]map[string]string
}

func (d *resticDocker) Run(ctx context.Context, dir string, args []string, out io.Writer) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, args)
	switch {
	case args[0] == "volume" && args[1] == "ls":
		io.WriteString(out, "cloud_phecloud-data\ncloud_signal-data\n")
	case args[0] == "volume" && args[1] == "inspect":
		for _, n := range args[4:] {
			b, _ := json.Marshal(volumeInfo{Name: n, Labels: map[string]string{"com.docker.compose.project": "cloud", "com.docker.compose.volume": strings.TrimPrefix(n, "cloud_")}})
			out.Write(append(b, '\n'))
		}
	case args[0] == "volume" && args[1] == "create":
		labels := map[string]string{}
		for i, a := range args {
			if a == "--label" {
				k, v, _ := strings.Cut(args[i+1], "=")
				labels[k] = v
			}
		}
		d.volumes[args[len(args)-1]] = labels
	case args[0] == "ps":
		io.WriteString(out, "c1\nc2\n")
	}
	return nil
}

func (d *resticDocker) RunEnv(ctx context.Context, dir string, env, args []string, out io.Writer) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, args)
	d.envs = append(d.envs, env)
	pass := ""
	for _, e := range env {
		if p, ok := strings.CutPrefix(e, "RESTIC_PASSWORD="); ok {
			pass = p
		}
	}
	i := slices.Index(args, ResticImage)
	cmd := args[i+1:]
	if d.password == "" && cmd[0] != "init" {
		io.WriteString(out, "Fatal: repository does not exist: unable to open config file\nIs there a repository at the following location?\n")
		return errors.New("exit status 10")
	}
	if d.password != "" && pass != d.password {
		io.WriteString(out, "Fatal: wrong password or no key found\n")
		return errors.New("exit status 12")
	}
	switch cmd[0] {
	case "init":
		d.password = pass
	case "backup":
		if d.failRun {
			io.WriteString(out, "Fatal: unable to save snapshot: disk full\n")
			return errors.New("exit status 1")
		}
		io.WriteString(out, `{"message_type":"status","percent_done":0.5}`+"\n"+`{"message_type":"summary","snapshot_id":"4f2a9c1e77aa"}`+"\n")
	case "snapshots":
		io.WriteString(out, `[{"short_id":"11111111","time":"2026-11-06T03:30:00Z","hostname":"cloud"},{"short_id":"4f2a9c1e","time":"2026-11-07T03:30:00Z","hostname":"cloud"}]`)
	case "dump":
		b, _ := json.Marshal([]volumeInfo{{Name: "cloud_phecloud-data", Labels: map[string]string{"com.docker.compose.project": "cloud", "com.docker.compose.volume": "phecloud-data"}}})
		out.Write(b)
	}
	return nil
}

func (d *resticDocker) did(prefix ...string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.calls {
		if len(c) >= len(prefix) && slices.Equal(c[:len(prefix)], prefix) {
			return true
		}
	}
	return false
}

func newBackupManager(t *testing.T) (*Manager, *resticDocker) {
	t.Helper()
	d := &resticDocker{volumes: map[string]map[string]string{}}
	dir := t.TempDir()
	m := &Manager{Cat: realCatalogue(t), ComposeDir: filepath.Join(dir, "cloud"), StateDir: filepath.Join(dir, "state"), Run: d}
	os.MkdirAll(m.ComposeDir, 0o755)
	os.MkdirAll(m.StateDir, 0o700)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m, d
}

func TestDestinationCheck(t *testing.T) {
	ok := "long enough!"
	for _, c := range []struct {
		d    Destination
		fail bool
	}{
		{Destination{Kind: "rest", URL: "https://h:8000/x", Password: ok}, false},
		{Destination{Kind: "rest", URL: "https://h:8000/x", Password: "short"}, true},
		{Destination{Kind: "sftp", Path: "me@nas.local:/backups/cloud", Password: ok}, false},
		{Destination{Kind: "sftp", Path: "nas.local:/x; rm -rf /", Password: ok}, true},
		{Destination{Kind: "s3", URL: "https://s3.example.com/b", KeyID: "k", Secret: "s", Password: ok}, false},
		{Destination{Kind: "s3", URL: "https://s3.example.com/b", Password: ok}, true},
		{Destination{Kind: "usb", UUID: "1234-ABCD", Path: "cloud", Password: ok}, false},
		{Destination{Kind: "usb", UUID: "1234-ABCD", Path: "../../etc", Password: ok}, true},
		{Destination{Kind: "dir", Path: "relative", Password: ok}, true},
		{Destination{Kind: "ftp", Password: ok}, true},
	} {
		if err := c.d.check(); (err != nil) != c.fail {
			t.Errorf("%+v: %v", c.d, err)
		}
	}
}

func TestPublicHidesSecrets(t *testing.T) {
	p := Destination{Kind: "rest", URL: "https://me:hunter2@h:8000/x", Password: "p", Secret: "s"}.Public()
	if p.Password != "" || p.Secret != "" || strings.Contains(p.URL, "hunter2") || !strings.HasSuffix(p.URL, "@h:8000/x") {
		t.Errorf("%+v", p)
	}
}

func TestBackupSetupAndRun(t *testing.T) {
	m, d := newBackupManager(t)
	os.WriteFile(filepath.Join(m.StateDir, "state.json"), []byte(`{"features":{"core":{"first-owner":"x"}}}`), 0o600)
	os.WriteFile(filepath.Join(m.ComposeDir, ".env"), []byte("A=1\n"), 0o600)
	dest := Destination{Kind: "rest", URL: "http://rest:8000/cloud", Password: "a long backup password"}

	if _, err := m.BackupNow(); err == nil {
		t.Error("a backup ran while backups are off")
	}
	if _, err := m.SetBackup(true, dest, "3:30"); err == nil {
		t.Error("a bad time was taken")
	}
	j, err := m.SetBackup(true, dest, "03:30")
	if err != nil {
		t.Fatal(err)
	}
	<-j.Done()
	if st := j.Status(); st.State != "done" || d.password != dest.Password {
		t.Fatalf("setting up: %+v", st)
	}
	for _, c := range d.calls {
		if strings.Contains(strings.Join(c, " "), dest.Password) {
			t.Fatal("the password is on docker's command line")
		}
	}
	// a page that sends no password keeps the saved one
	j, err = m.SetBackup(true, Destination{Kind: "rest", URL: dest.URL}, "04:00")
	if err != nil {
		t.Fatal(err)
	}
	<-j.Done()
	if c := m.BackupConfig(); c.At != "04:00" || c.Dest.Password != dest.Password {
		t.Fatalf("kept: %+v", c)
	}

	j, err = m.BackupNow()
	if err != nil {
		t.Fatal(err)
	}
	<-j.Done()
	if st := j.Status(); st.State != "done" {
		t.Fatalf("backup: %+v", st)
	}
	if !d.did("stop", "--time", "60", "c1", "c2") || !d.did("start", "c1", "c2") {
		t.Error("Cloud Companion was not stopped for the backup and started again")
	}
	var backupArgs []string
	for _, c := range d.calls {
		if slices.Contains(c, "backup") {
			backupArgs = c
		}
	}
	for _, want := range []string{"cloud_phecloud-data:/data/volumes/cloud_phecloud-data:ro", "cloud_signal-data:/data/volumes/cloud_signal-data:ro"} {
		if !slices.Contains(backupArgs, want) {
			t.Errorf("volume not backed up: %s in %v", want, backupArgs)
		}
	}
	if c := m.BackupConfig(); c.Last == nil || !c.Last.OK || c.Last.Snapshot != "4f2a9c1e" {
		t.Errorf("last run: %+v", c.Last)
	}
	stage := filepath.Join(m.StateDir, stageDir)
	for _, f := range []string{"state.json", ".env", "volumes.json"} {
		if _, err := os.Stat(filepath.Join(stage, f)); err != nil {
			t.Errorf("not kept: %s", f)
		}
	}

	// a failed backup still starts Cloud Companion again, and says why
	d.failRun = true
	d.calls = nil
	j, _ = m.BackupNow()
	<-j.Done()
	if !d.did("start", "c1", "c2") {
		t.Error("Cloud Companion stayed stopped after a failed backup")
	}
	if c := m.BackupConfig(); c.Last.OK || !strings.Contains(c.Last.Error, "disk full") {
		t.Errorf("failed run: %+v", c.Last)
	}
}

func TestSnapshotsAndRestore(t *testing.T) {
	m, d := newBackupManager(t)
	dest := Destination{Kind: "rest", URL: "http://rest:8000/cloud", Password: "a long backup password"}
	if _, err := m.Snapshots(context.Background(), dest); err == nil || err.Error() != "there are no backups there" {
		t.Errorf("empty destination: %v", err)
	}
	d.password = "another password"
	if _, err := m.Snapshots(context.Background(), dest); err == nil || err.Error() != "that is not the backup password" {
		t.Errorf("wrong password: %v", err)
	}
	d.password = dest.Password
	snaps, err := m.Snapshots(context.Background(), dest)
	if err != nil || len(snaps) != 2 || snaps[0].ID != "4f2a9c1e" {
		t.Fatalf("newest first: %+v %v", snaps, err)
	}
	if _, err := m.Restore(dest, "x;y"); err == nil {
		t.Error("a bad snapshot id was taken")
	}
	// the restore writes the staged files back from what restic restored into the stage folder
	j, err := m.Restore(dest, "4f2a9c1e")
	if err != nil {
		t.Fatal(err)
	}
	<-j.Done()
	if st := j.Status(); st.State != "done" {
		t.Fatalf("restore: %+v", st)
	}
	if l := d.volumes["cloud_phecloud-data"]; l["com.docker.compose.project"] != "cloud" || l["com.docker.compose.volume"] != "phecloud-data" {
		t.Errorf("volume made again without compose's labels: %v", l)
	}
	if c := m.BackupConfig(); !c.On || c.Dest.URL != dest.URL {
		t.Errorf("backups do not go on to the same place: %+v", c)
	}
	os.WriteFile(filepath.Join(m.StateDir, "state.json"), []byte(`{"features":{"core":{}}}`), 0o600)
	if _, err := m.Restore(dest, "latest"); err == nil {
		t.Error("restored over a board that is set up")
	}
}

// A board runs the images of the release its files came from: VERSION (bundle.sh) reaches compose as PHECLOUD_VERSION.
func TestEnvFileReleaseVersion(t *testing.T) {
	m, _ := newBackupManager(t)
	if strings.Contains(string(m.envFile(State{}, nil)), "PHECLOUD_VERSION") {
		t.Error("a version without VERSION")
	}
	os.WriteFile(filepath.Join(m.ComposeDir, "VERSION"), []byte("v0.4.0\n"), 0o644)
	if !strings.Contains(string(m.envFile(State{}, nil)), `PHECLOUD_VERSION="v0.4.0"`) {
		t.Errorf("not passed on:\n%s", m.envFile(State{}, nil))
	}
}
