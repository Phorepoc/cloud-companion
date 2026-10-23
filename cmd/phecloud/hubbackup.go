// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package main

// "phecloud hub backup <dir>" and "phecloud hub restore <backup> [--prefix p-] [--force]".
//
// A backup is one directory holding: the Stalwart data volume and configuration volume (tar.gz), a consistent copy of phecloud's own
// database (SQLite VACUUM INTO, taken while the server runs), the TLS certificate and key, and manifest.json with SHA-256 sums.
// The Stalwart snapshot is consistent because Stalwart is stopped while its two volumes are archived (a few seconds) and started again,
// whatever happens. Everything goes through the `docker` command line (run it on the Docker host, or in the phe-backup image with the
// Docker socket: see docs/hub.md); archives are streamed through stdout/stdin, so no host paths are needed.
//
// Environment: PHE_STALWART (container, default phe-stalwart), PHE_CLOUD (container, default phe-cloud).

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func dockerOut(stdin io.Reader, out io.Writer, args ...string) error {
	cmd := exec.Command("docker", args...)
	cmd.Stdin, cmd.Stdout = stdin, out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker %s: %v: %s", strings.Join(args[:min(len(args), 3)], " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func dockerText(args ...string) (string, error) {
	var b bytes.Buffer
	err := dockerOut(nil, &b, args...)
	return strings.TrimSpace(b.String()), err
}

// archiveVolume streams a volume as tar.gz into path; it returns the SHA-256 of the file.
func archiveVolume(vol, path string) (string, error) {
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if err := dockerOut(nil, io.MultiWriter(f, h), "run", "--rm", "--cpus", "1", "--memory", "256m", "-v", vol+":/v:ro", "busybox:1.37", "tar", "czf", "-", "-C", "/v", "."); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type manifest struct {
	Created        string            `json:"created"`
	StalwartImage  string            `json:"stalwart_image"`
	StalwartData   string            `json:"stalwart_data_volume"`
	StalwartEtc    string            `json:"stalwart_etc_volume"`
	Files          map[string]string `json:"sha256"`
	CloudContainer string            `json:"cloud_container"`
}

func backupMain(args []string) {
	fail := func(err error) {
		fmt.Fprintln(os.Stderr, "backup failed:", err)
		os.Exit(1)
	}
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: phecloud hub backup <dir>")
		os.Exit(2)
	}
	sw, cloud := env("PHE_STALWART", "phe-stalwart"), env("PHE_CLOUD", "phe-cloud")
	dir := filepath.Join(args[0], "hub-backup-"+time.Now().UTC().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fail(err)
	}
	m := manifest{Created: time.Now().UTC().Format(time.RFC3339), Files: map[string]string{}, CloudContainer: cloud}

	// 1. phecloud's own state, consistent while it runs
	tmp := "/tmp/phecloud-backup.db"
	if err := dockerOut(nil, io.Discard, "exec", cloud, "sh", "-c", "rm -f "+tmp); err != nil {
		fail(err)
	}
	external := false
	if err := dockerOut(nil, io.Discard, "exec", cloud, "phecloud", "snapshot", tmp); err != nil {
		if strings.Contains(err.Error(), "exit status 3") { // PHECLOUD_DB is PostgreSQL / MariaDB: not in this backup
			external = true
			fmt.Println("note: phecloud's database is an external server; back it up with pg_dump / mariadb-dump (docs/database.md). The Stalwart data and the certificate are saved below.")
		} else {
			fail(err)
		}
	}
	files := []struct{ src, name string }{{tmp, "phecloud.db"}, {"/data/tls.crt", "tls.crt"}, {"/data/tls.key", "tls.key"}, {"/data/connectors.key", "connectors.key"}}
	if external {
		files = files[1:]
	}
	for _, f := range files {
		out, err := os.OpenFile(filepath.Join(dir, f.name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			fail(err)
		}
		err = dockerOut(nil, out, "exec", cloud, "cat", f.src)
		out.Close()
		if err != nil {
			if f.name != "phecloud.db" { // a server that never ran TLS has no certificate files
				os.Remove(filepath.Join(dir, f.name))
				continue
			}
			fail(err)
		}
	}
	dockerOut(nil, io.Discard, "exec", cloud, "rm", "-f", tmp)

	// 2. Stalwart: stop, archive both volumes, start again (always)
	if m.StalwartImage, _ = dockerText("inspect", sw, "--format", "{{.Config.Image}}"); m.StalwartImage == "" {
		fail(errors.New("container " + sw + " not found"))
	}
	m.StalwartData, _ = dockerText("inspect", sw, "--format", `{{range .Mounts}}{{if eq .Destination "/var/lib/stalwart"}}{{.Name}}{{end}}{{end}}`)
	m.StalwartEtc, _ = dockerText("inspect", sw, "--format", `{{range .Mounts}}{{if eq .Destination "/etc/stalwart"}}{{.Name}}{{end}}{{end}}`)
	if m.StalwartData == "" || m.StalwartEtc == "" {
		fail(errors.New("could not find the Stalwart volumes"))
	}
	wasRunning, _ := dockerText("inspect", sw, "--format", "{{.State.Running}}")
	if wasRunning == "true" {
		if err := dockerOut(nil, io.Discard, "stop", "-t", "30", sw); err != nil {
			fail(err)
		}
	}
	var archErr error
	for _, v := range []struct{ vol, name string }{{m.StalwartData, "stalwart-data.tgz"}, {m.StalwartEtc, "stalwart-etc.tgz"}} {
		if archErr == nil {
			archErr = func() error {
				sum, err := archiveVolume(v.vol, filepath.Join(dir, v.name))
				m.Files[v.name] = sum
				return err
			}()
		}
	}
	if wasRunning == "true" {
		if err := dockerOut(nil, io.Discard, "start", sw); err != nil {
			fmt.Fprintln(os.Stderr, "WARNING: Stalwart did not start again:", err)
		}
	}
	if archErr != nil {
		fail(archErr)
	}
	for _, n := range []string{"phecloud.db", "tls.crt", "tls.key", "connectors.key"} {
		if s, err := fileSum(filepath.Join(dir, n)); err == nil {
			m.Files[n] = s
		}
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0o600); err != nil {
		fail(err)
	}
	fmt.Println("backup written to", dir)
	for n, s := range m.Files {
		fmt.Printf("  %-18s sha256 %s\n", n, s[:16])
	}
}

func restoreMain(args []string) {
	fail := func(err error) {
		fmt.Fprintln(os.Stderr, "restore failed:", err)
		os.Exit(1)
	}
	var dir, prefix string
	force := false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--prefix" && i+1 < len(args):
			prefix = args[i+1]
			i++
		case args[i] == "--force":
			force = true
		case dir == "":
			dir = args[i]
		}
	}
	if dir == "" {
		fmt.Fprintln(os.Stderr, "usage: phecloud hub restore <backup dir> [--prefix <volume name prefix>] [--force]")
		os.Exit(2)
	}
	var m manifest
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil || json.Unmarshal(raw, &m) != nil {
		fail(errors.New("not a hub backup (no readable manifest.json)"))
	}
	for n, want := range m.Files { // verify before touching anything
		got, err := fileSum(filepath.Join(dir, n))
		if err != nil || got != want {
			fail(fmt.Errorf("%s is missing or damaged (checksum mismatch)", n))
		}
	}
	vols := map[string]string{"stalwart-data.tgz": prefix + "stalwart-data", "stalwart-etc.tgz": prefix + "stalwart-etc"}
	cloudVol := prefix + "phecloud-data"
	for _, v := range append([]string{cloudVol}, vols["stalwart-data.tgz"], vols["stalwart-etc.tgz"]) {
		if err := dockerOut(nil, io.Discard, "volume", "create", v); err != nil {
			fail(err)
		}
		out, _ := dockerText("run", "--rm", "-v", v+":/v:ro", "busybox:1.37", "sh", "-c", "ls -A /v | head -1")
		if out != "" && !force {
			fail(fmt.Errorf("volume %s is not empty: restore into a fresh prefix, or pass --force to overwrite", v))
		}
	}
	for tgz, v := range vols {
		f, err := os.Open(filepath.Join(dir, tgz))
		if err != nil {
			fail(err)
		}
		err = dockerOut(f, io.Discard, "run", "--rm", "-i", "-v", v+":/v", "busybox:1.37", "sh", "-c", "rm -rf /v/* /v/.[!.]* 2>/dev/null; tar xzf - -C /v")
		f.Close()
		if err != nil {
			fail(err)
		}
	}
	for _, n := range []string{"phecloud.db", "tls.crt", "tls.key", "connectors.key"} {
		f, err := os.Open(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		name := n
		if n == "phecloud.db" {
			name = "phecloud.db"
		}
		err = dockerOut(f, io.Discard, "run", "--rm", "-i", "-v", cloudVol+":/v", "busybox:1.37", "sh", "-c", "cat > /v/"+name+" && chmod 600 /v/"+name)
		f.Close()
		if err != nil {
			fail(err)
		}
	}
	fmt.Printf("restored into volumes %s, %s, %s (Stalwart image of the backup: %s)\n", vols["stalwart-data.tgz"], vols["stalwart-etc.tgz"], cloudVol, m.StalwartImage)
	fmt.Println("start Stalwart on the two volumes (config at /etc/stalwart, data at /var/lib/stalwart) and phecloud with its data volume mounted at /data")
}
