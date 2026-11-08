// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Updates come from the project's GitHub releases, over HTTPS only (docs/host-manager.md, "Updates"). A release always has
// images; it has board files (the manager and the compose files, bundle.sh) only when they changed. An update:
//   backs up (when backups are on), puts in the newer board files (the old ones kept), runs the release's images, waits for
//   Cloud Companion to be healthy, and when anything fails puts everything back as it was.
// A new manager takes over by the manager exiting: systemd starts the new one.

// UpdateConfig is kept in StateDir/update.json.
type UpdateConfig struct {
	Images    string         `json:"images,omitempty"` // the release whose images run; "" = the board files' (VERSION)
	Auto      bool           `json:"auto"`             // install during the night, after the backup
	Checked   time.Time      `json:"checked,omitzero"`
	Available *Release       `json:"available,omitempty"`
	Last      *UpdateRun     `json:"last,omitempty"`
	Error     string         `json:"error,omitempty"` // the last check's
	Board     *ReleaseAssets `json:"board,omitempty"` // the newest board files up to Available
}

// Release is what a person is shown of a release: its version and what changed.
type Release struct {
	Version string `json:"version"` // v1.2.3
	Notes   string `json:"notes,omitempty"`
}

// ReleaseAssets are the board files of a release.
type ReleaseAssets struct {
	Version string            `json:"version"`
	URLs    map[string]string `json:"urls"` // file name: download address
}

type UpdateRun struct {
	At    time.Time `json:"at"`
	From  string    `json:"from"`
	To    string    `json:"to"`
	OK    bool      `json:"ok"`
	Error string    `json:"error,omitempty"`
}

// DefaultReleases is the project's releases on GitHub's API.
const DefaultReleases = "https://api.github.com/repos/phorepoc/cloud-companion/releases"

func (m *Manager) updateFile() string { return filepath.Join(m.StateDir, "update.json") }

// UpdateConfig reads update.json; missing means automatic updates are on (a board nobody looks after keeps up).
func (m *Manager) UpdateConfig() UpdateConfig {
	c := UpdateConfig{Auto: true}
	if b, err := os.ReadFile(m.updateFile()); err == nil {
		json.Unmarshal(b, &c)
	}
	return c
}

func (m *Manager) saveUpdate(c UpdateConfig) error {
	b, _ := json.MarshalIndent(c, "", "  ")
	return writeFileAtomic(m.updateFile(), b, 0o600)
}

// editUpdate changes update.json in one step: a check, the automatic switch and an update in progress each write their own
// fields, and none of them puts back an older copy of the others' (which once could switch the images back to the old release).
func (m *Manager) editUpdate(change func(*UpdateConfig)) (UpdateConfig, error) {
	m.umu.Lock()
	defer m.umu.Unlock()
	c := m.UpdateConfig()
	change(&c)
	return c, m.saveUpdate(c)
}

// BoardVersion is the release of the board files in place (bundle.sh's VERSION); "" for a checkout.
func (m *Manager) BoardVersion() string {
	b, _ := os.ReadFile(filepath.Join(m.ComposeDir, "VERSION"))
	return strings.TrimSpace(string(b))
}

// Version is the release the board runs: its images.
func (m *Manager) Version() string {
	if v := m.UpdateConfig().Images; v != "" {
		return v
	}
	return m.BoardVersion()
}

// semver reads vX.Y.Z; ok is false for anything else (pre-releases are not offered).
func semver(v string) (n [3]int, ok bool) {
	p := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(p) != 3 || !strings.HasPrefix(v, "v") {
		return n, false
	}
	for i, s := range p {
		x, err := strconv.Atoi(s)
		if err != nil || x < 0 {
			return n, false
		}
		n[i] = x
	}
	return n, true
}

// newer says whether a is a later release than b ("" is older than anything).
func newer(a, b string) bool {
	x, ok := semver(a)
	if !ok {
		return false
	}
	y, ok := semver(b)
	if !ok {
		return true
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

func (m *Manager) client() *http.Client {
	if m.HTTP != nil {
		return m.HTTP
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

func (m *Manager) releasesURL() string {
	if m.Releases != "" {
		return m.Releases
	}
	return DefaultReleases
}

// get fetches an HTTPS address (GitHub's redirects to its file storage included: they must be HTTPS too).
func (m *Manager) get(ctx context.Context, addr string) (*http.Response, error) {
	if u, err := url.Parse(addr); err != nil || u.Scheme != "https" {
		return nil, fmt.Errorf("not an HTTPS address: %q", addr)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", addr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	c := *m.client()
	c.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if r.URL.Scheme != "https" || len(via) > 5 {
			return errors.New("refused a redirect away from HTTPS")
		}
		return nil
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("%s: %s", addr, res.Status)
	}
	return res, nil
}

// CheckUpdate asks GitHub for the newest release, and keeps what it found in update.json.
func (m *Manager) CheckUpdate(ctx context.Context) (UpdateConfig, error) {
	avail, board, err := m.latest(ctx, m.Version(), m.BoardVersion())
	c, _ := m.editUpdate(func(c *UpdateConfig) {
		c.Checked, c.Error = time.Now().UTC(), ""
		if err != nil {
			c.Error = "Couldn't reach GitHub (" + err.Error() + ")"
		} else {
			c.Available, c.Board = avail, board
		}
	})
	return c, err
}

func (m *Manager) latest(ctx context.Context, current, boardCurrent string) (*Release, *ReleaseAssets, error) {
	res, err := m.get(ctx, m.releasesURL()+"?per_page=30")
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	var rels []struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Body       string `json:"body"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&rels); err != nil {
		return nil, nil, fmt.Errorf("the release list: %w", err)
	}
	var top *Release
	var board *ReleaseAssets
	for _, r := range rels {
		if r.Draft || r.Prerelease {
			continue
		}
		if _, ok := semver(r.Tag); !ok {
			continue
		}
		if top == nil || newer(r.Tag, top.Version) {
			top = &Release{Version: r.Tag, Notes: plainNotes(r.Body)}
		}
		urls := map[string]string{}
		for _, a := range r.Assets {
			urls[a.Name] = a.URL
		}
		if urls["cloud-board.tar.gz"] != "" && urls["SHA256SUMS.board"] != "" && (board == nil || newer(r.Tag, board.Version)) {
			board = &ReleaseAssets{Version: r.Tag, URLs: urls}
		}
	}
	if top == nil || !newer(top.Version, current) {
		return nil, nil, nil // up to date
	}
	if board != nil && !newer(board.Version, boardCurrent) {
		board = nil
	}
	return top, board, nil
}

// plainNotes makes a release's changelog (Markdown) readable as plain text: no headings' hashes, links or commit hashes.
func plainNotes(md string) string {
	var out []string
	for _, l := range strings.Split(md, "\n") {
		l = strings.TrimSpace(l)
		l = strings.TrimLeft(l, "#")
		l = strings.TrimSpace(l)
		// [text](address) -> text
		for {
			i := strings.Index(l, "](")
			j := strings.LastIndex(l[:max(i, 0)], "[")
			k := strings.Index(l[max(i, 0):], ")")
			if i < 0 || j < 0 || k < 0 {
				break
			}
			l = l[:j] + l[j+1:i] + l[i+k+1:]
		}
		l = strings.ReplaceAll(l, "**", "")
		if strings.HasPrefix(l, "* ") {
			l = "• " + l[2:]
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// SetAutoUpdate switches installing updates during the night on or off.
func (m *Manager) SetAutoUpdate(on bool) error {
	_, err := m.editUpdate(func(c *UpdateConfig) { c.Auto = on })
	return err
}

// ErrUpToDate: there is nothing to install.
var ErrUpToDate = errors.New("Cloud Companion is up to date")

// Update installs the release found by the last check, as a job.
func (m *Manager) Update() (*Job, error) {
	c := m.UpdateConfig()
	if c.Available == nil {
		return nil, ErrUpToDate
	}
	if !m.SetUp() {
		return nil, errors.New("Finish setup first")
	}
	j, err := m.soleJob("update")
	if err != nil {
		return nil, err
	}
	go func() {
		m.mu.Lock()
		restart, err := m.update(c, j)
		m.mu.Unlock()
		j.finish(err)
		if restart && m.Restart != nil {
			time.Sleep(3 * time.Second) // the page reads the job's end first
			m.Restart()
		}
	}()
	return j, nil
}

// update runs with m.mu held. restart: a new manager is in place and takes over.
func (m *Manager) update(c UpdateConfig, j *Job) (restart bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	from, to := m.Version(), c.Available.Version
	run := &UpdateRun{At: time.Now().UTC(), From: from, To: to}
	defer func() {
		run.OK = err == nil
		if err != nil {
			run.Error = err.Error()
		}
		m.editUpdate(func(c *UpdateConfig) {
			if err == nil {
				c.Available, c.Board = nil, nil
			}
			c.Last = run
		})
	}()

	if b := m.BackupConfig(); b.On {
		fmt.Fprintln(j, "backing up first")
		snap, err := m.backup(ctx, b.Dest, j)
		b = m.BackupConfig()
		b.Last = &BackupRun{At: time.Now().UTC(), OK: err == nil, Snapshot: snap}
		if err != nil {
			b.Last.Error = err.Error()
			m.saveBackup(b)
			return false, fmt.Errorf("The backup before the update failed, so nothing was changed: %w", err)
		}
		m.saveBackup(b)
	}

	keep := filepath.Join(m.StateDir, "previous")
	newBoard := c.Board != nil
	if newBoard {
		fmt.Fprintf(j, "getting the board files of %s\n", c.Board.Version)
		files, err := m.download(ctx, c.Board, j)
		defer os.RemoveAll(files)
		if err != nil {
			return false, err
		}
		if err := m.keepPrevious(keep); err != nil {
			return false, fmt.Errorf("keeping the current files: %w", err)
		}
		if err := m.putBoardFiles(files); err != nil {
			m.putBack(keep, j)
			return false, err
		}
	}

	prevImages := m.UpdateConfig().Images
	m.editUpdate(func(c *UpdateConfig) { c.Images = to })
	fmt.Fprintf(j, "starting Cloud Companion %s\n", to)
	err = m.apply(ctx, m.cloneState(), j)
	if err == nil {
		err = m.waitHealthy(ctx, j)
	}
	if err != nil {
		fmt.Fprintf(j, "failed: %v; going back to %s\n", err, from)
		m.editUpdate(func(c *UpdateConfig) { c.Images = prevImages })
		if newBoard {
			m.putBack(keep, j)
		}
		if rerr := m.apply(ctx, m.cloneState(), j); rerr != nil {
			fmt.Fprintf(j, "going back failed too: %v\n", rerr)
		}
		if newBoard {
			restart = true // the old manager is back in place: run it
		}
		return restart, fmt.Errorf("Version %s didn't start, so %s was restored: %w", to, from, err)
	}
	fmt.Fprintf(j, "Cloud Companion %s is running\n", to)
	return newBoard, nil
}

// waitHealthy waits for Cloud Companion's health check (up to ten minutes: a Pi takes its time on a first start).
func (m *Manager) waitHealthy(ctx context.Context, j *Job) error {
	deadline := time.Now().Add(m.healthWait())
	for {
		h := m.Health(ctx)
		if h == "healthy" || h == "running" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Cloud Companion is %s", h)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.healthPoll()):
		}
	}
}

func (m *Manager) healthWait() time.Duration {
	if m.HealthWait > 0 {
		return m.HealthWait
	}
	return 10 * time.Minute
}

func (m *Manager) healthPoll() time.Duration {
	if m.HealthWait > 0 {
		return m.HealthWait / 20
	}
	return 5 * time.Second
}

// download fetches the board files into a new folder and checks them against the release's SHA256SUMS.board.
func (m *Manager) download(ctx context.Context, a *ReleaseAssets, j *Job) (string, error) {
	dir, err := os.MkdirTemp(m.StateDir, "update-")
	if err != nil {
		return "", err
	}
	bin := "phehost-linux-" + m.Board.Arch
	for _, f := range []string{"SHA256SUMS.board", "cloud-board.tar.gz", bin} {
		u := a.URLs[f]
		if u == "" {
			return dir, fmt.Errorf("Release %s is missing %s", a.Version, f)
		}
		res, err := m.get(ctx, u)
		if err != nil {
			return dir, err
		}
		out, err := os.Create(filepath.Join(dir, f))
		if err == nil {
			_, err = io.Copy(out, io.LimitReader(res.Body, 512<<20))
			out.Close()
		}
		res.Body.Close()
		if err != nil {
			return dir, fmt.Errorf("%s: %w", f, err)
		}
	}
	sums, err := os.Open(filepath.Join(dir, "SHA256SUMS.board"))
	if err != nil {
		return dir, err
	}
	defer sums.Close()
	want := map[string]string{}
	for sc := bufio.NewScanner(sums); sc.Scan(); {
		if f := strings.Fields(sc.Text()); len(f) == 2 {
			want[strings.TrimPrefix(f[1], "*")] = f[0]
		}
	}
	for _, f := range []string{"cloud-board.tar.gz", bin} {
		got, err := fileSHA256(filepath.Join(dir, f))
		if err != nil || want[f] == "" || got != want[f] {
			return dir, fmt.Errorf("%s didn't download correctly. Try again later", f)
		}
	}
	fmt.Fprintln(j, "board files checked")
	return dir, nil
}

func fileSHA256(name string) (string, error) {
	f, err := os.Open(name)
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

// isBoardFile: what bundle.sh puts in ComposeDir, kept before an update and put back by a failed one.
func isBoardFile(name string) bool {
	if strings.HasPrefix(name, "docker-compose") && strings.HasSuffix(name, ".yml") {
		return true
	}
	return name == "VERSION" || name == "deploy"
}

// keepPrevious copies the board files and the manager in place to StateDir/previous.
func (m *Manager) keepPrevious(keep string) error {
	os.RemoveAll(keep)
	if err := os.MkdirAll(keep, 0o700); err != nil {
		return err
	}
	ents, err := os.ReadDir(m.ComposeDir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if isBoardFile(e.Name()) {
			if err := copyTree(filepath.Join(m.ComposeDir, e.Name()), filepath.Join(keep, "files", e.Name())); err != nil {
				return err
			}
		}
	}
	if m.Binary != "" {
		return copyTree(m.Binary, filepath.Join(keep, "phehost"))
	}
	return nil
}

// putBack puts the kept board files and manager in place again.
func (m *Manager) putBack(keep string, j *Job) {
	fmt.Fprintln(j, "putting the previous files back")
	ents, _ := os.ReadDir(m.ComposeDir)
	for _, e := range ents {
		if isBoardFile(e.Name()) && e.Name() != "deploy" {
			os.Remove(filepath.Join(m.ComposeDir, e.Name()))
		}
	}
	kept, _ := os.ReadDir(filepath.Join(keep, "files"))
	for _, e := range kept {
		// copied over, not swapped: deploy also holds files written on the board (the hub's .env files)
		copyTree(filepath.Join(keep, "files", e.Name()), filepath.Join(m.ComposeDir, e.Name()))
	}
	if m.Binary != "" {
		replaceFile(filepath.Join(keep, "phehost"), m.Binary)
	}
}

// putBoardFiles unpacks the new board files over ComposeDir and puts the new manager in place.
func (m *Manager) putBoardFiles(dir string) error {
	f, err := os.Open(filepath.Join(dir, "cloud-board.tar.gz"))
	if err != nil {
		return err
	}
	defer f.Close()
	if err := untar(f, m.ComposeDir); err != nil {
		return fmt.Errorf("unpacking the board files: %w", err)
	}
	if m.Binary != "" {
		if err := replaceFile(filepath.Join(dir, "phehost-linux-"+m.Board.Arch), m.Binary); err != nil {
			return fmt.Errorf("the new manager: %w", err)
		}
	}
	return nil
}

// untar unpacks a .tar.gz into dir: regular files and folders only, nothing outside dir.
func untar(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(h.Name)
		if name == "." || filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("a file outside the folder: %q", h.Name)
		}
		dst := filepath.Join(dir, name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			tmp := dst + ".new"
			out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o755|0o600)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, io.LimitReader(tr, 256<<20))
			out.Close()
			if err == nil {
				err = os.Rename(tmp, dst)
			}
			if err != nil {
				return err
			}
		}
	}
}

// replaceFile puts a copy of src at dst in one step (a running program's file can be replaced; it keeps the old one open).
func replaceFile(src, dst string) error {
	tmp := dst + ".new"
	if err := copyTree(src, tmp); err != nil {
		return err
	}
	os.Chmod(tmp, 0o755)
	return os.Rename(tmp, dst)
}

// copyTree copies a file or a folder (regular files only).
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		to := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(to, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		return err
	})
}
