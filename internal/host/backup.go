// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Backups (docs/host-manager.md, "Backups"): restic, in a container, copies every volume of the compose project and the
// manager's state to a destination the owner chose, encrypted with their backup password, on a schedule. A fresh board
// restores all of it from the setup page.

// ResticImage is the restic release the manager runs.
const ResticImage = "restic/restic:0.18.1"

// Destination is where backups go. Kind: usb (a partition by UUID, mounted for the run), dir (a folder on the board, e.g. a
// share mounted by hand), sftp (user@host:/path, with the board's own key), s3 (an S3-compatible bucket), rest (a restic
// REST server).
type Destination struct {
	Kind     string `json:"kind"`
	UUID     string `json:"uuid,omitempty"`     // usb
	Path     string `json:"path,omitempty"`     // usb: folder on the disk; dir; sftp: user@host:/path
	URL      string `json:"url,omitempty"`      // s3: https://endpoint/bucket/prefix; rest: https://user:pass@host:8000/
	KeyID    string `json:"keyId,omitempty"`    // s3
	Secret   string `json:"secret,omitempty"`   // s3
	Password string `json:"password,omitempty"` // the backup password (restic's key)
}

// BackupConfig is backup.json: the destination, when, and how the last run went.
type BackupConfig struct {
	On   bool        `json:"on"`
	Dest Destination `json:"dest"`
	At   string      `json:"at"` // "03:30", the board's time
	Last *BackupRun  `json:"last,omitempty"`
}

type BackupRun struct {
	At       time.Time `json:"at"`
	OK       bool      `json:"ok"`
	Error    string    `json:"error,omitempty"`
	Snapshot string    `json:"snapshot,omitempty"`
}

// Snapshot is one backup in the destination.
type Snapshot struct {
	ID   string    `json:"id"`
	Time time.Time `json:"time"`
	Host string    `json:"host"`
}

var (
	atTime   = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	sftpPath = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+(:[0-9]+)?:/?[^\s]*$`)
	uuidRe   = regexp.MustCompile(`^[A-Za-z0-9-]{4,64}$`)
)

// check says what is wrong with a destination, in words for the page.
func (d Destination) check() error {
	if len(d.Password) < 10 {
		return errors.New("the backup password needs at least 10 characters")
	}
	switch d.Kind {
	case "usb":
		if !uuidRe.MatchString(d.UUID) {
			return errors.New("pick a USB disk")
		}
		if strings.Contains(d.Path, "..") {
			return errors.New("bad folder")
		}
	case "dir":
		if !filepath.IsAbs(d.Path) || strings.Contains(d.Path, "..") {
			return errors.New("the folder must be a full path")
		}
	case "sftp":
		if !sftpPath.MatchString(d.Path) {
			return errors.New("SFTP needs user@host:/folder")
		}
	case "s3":
		if !strings.HasPrefix(d.URL, "https://") && !strings.HasPrefix(d.URL, "http://") || d.KeyID == "" || d.Secret == "" {
			return errors.New("S3 needs the bucket's address and an access key")
		}
	case "rest":
		if !strings.HasPrefix(d.URL, "https://") && !strings.HasPrefix(d.URL, "http://") {
			return errors.New("the REST server needs its address")
		}
	default:
		return errors.New("pick where the backups go")
	}
	return nil
}

// Public is the destination without its secrets, for the pages.
func (d Destination) Public() Destination {
	d.Password, d.Secret = "", ""
	if i := strings.Index(d.URL, "@"); i > 0 && strings.Contains(d.URL[:i], "://") {
		d.URL = d.URL[:strings.Index(d.URL, "://")+3] + "…" + d.URL[i:]
	}
	return d
}

func (m *Manager) backupFile() string { return filepath.Join(m.StateDir, "backup.json") }

// BackupConfig reads backup.json (zero when there is none).
func (m *Manager) BackupConfig() BackupConfig {
	var c BackupConfig
	if b, err := os.ReadFile(m.backupFile()); err == nil {
		json.Unmarshal(b, &c)
	}
	if c.At == "" {
		c.At = "03:30"
	}
	return c
}

func (m *Manager) saveBackup(c BackupConfig) error {
	b, _ := json.MarshalIndent(c, "", "  ")
	return writeFileAtomic(m.backupFile(), b, 0o600)
}

// SSHKey is the board's public key for SFTP destinations (made on first use): the owner adds it to the server's
// authorized_keys.
func (m *Manager) SSHKey() (string, error) {
	dir := filepath.Join(m.StateDir, "ssh")
	if b, err := os.ReadFile(filepath.Join(dir, "id_ed25519.pub")); err == nil {
		return strings.TrimSpace(string(b)), nil
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	blk, err := ssh.MarshalPrivateKey(priv, "cloud backups")
	if err != nil {
		return "", err
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))) + " cloud-backups"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	cfg := "Host *\n  IdentityFile /root/.ssh/id_ed25519\n  StrictHostKeyChecking accept-new\n  UserKnownHostsFile /root/.ssh/known_hosts\n  ServerAliveInterval 60\n"
	if err := writeFileAtomic(filepath.Join(dir, "config"), []byte(cfg), 0o600); err != nil {
		return "", err
	}
	if err := writeFileAtomic(filepath.Join(dir, "id_ed25519"), pem.EncodeToMemory(blk), 0o600); err != nil {
		return "", err
	}
	return line, writeFileAtomic(filepath.Join(dir, "id_ed25519.pub"), []byte(line+"\n"), 0o644)
}

// restic runs restic in a container against the destination, with extra docker run arguments (the volumes), and returns
// its output. A usb destination is mounted for the run.
func (m *Manager) restic(ctx context.Context, d Destination, mounts []string, out io.Writer, args ...string) error {
	run := []string{"run", "--rm", "--hostname", "cloud", "-e", "RESTIC_PASSWORD", "-e", "RESTIC_REPOSITORY"}
	env := []string{"RESTIC_PASSWORD=" + d.Password}
	switch d.Kind {
	case "usb":
		dir := filepath.Join(m.StateDir, "usb", d.UUID)
		if err := m.mountUSB(ctx, d.UUID, dir); err != nil {
			return err
		}
		defer m.host(context.Background(), "umount", dir)
		run = append(run, "-v", dir+":/repo-disk")
		env = append(env, "RESTIC_REPOSITORY="+filepath.Join("/repo-disk", d.Path, "cloud-backups"))
	case "dir":
		os.MkdirAll(d.Path, 0o700)
		run = append(run, "-v", d.Path+":/repo")
		env = append(env, "RESTIC_REPOSITORY=/repo")
	case "sftp":
		if _, err := m.SSHKey(); err != nil {
			return err
		}
		run = append(run, "-v", filepath.Join(m.StateDir, "ssh")+":/root/.ssh")
		env = append(env, "RESTIC_REPOSITORY=sftp:"+d.Path)
	case "s3":
		run = append(run, "-e", "AWS_ACCESS_KEY_ID", "-e", "AWS_SECRET_ACCESS_KEY")
		env = append(env, "RESTIC_REPOSITORY=s3:"+d.URL, "AWS_ACCESS_KEY_ID="+d.KeyID, "AWS_SECRET_ACCESS_KEY="+d.Secret)
	case "rest":
		env = append(env, "RESTIC_REPOSITORY=rest:"+d.URL)
	}
	run = append(append(run, mounts...), ResticImage)
	if er, ok := m.Run.(EnvRunner); ok {
		return er.RunEnv(ctx, m.ComposeDir, env, append(run, args...), out)
	}
	return errors.New("this manager cannot run restic")
}

// EnvRunner runs docker with extra environment variables (restic's password and keys stay out of the command line).
type EnvRunner interface {
	RunEnv(ctx context.Context, dir string, env, args []string, out io.Writer) error
}

func (ExecRunner) RunEnv(ctx context.Context, dir string, env, args []string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, out, out
	cmd.Env = append(os.Environ(), env...)
	return cmd.Run()
}

func (m *Manager) host(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.Host == nil {
		return nil, errors.New("no host commands here")
	}
	return m.Host(ctx, name, args...)
}

func (m *Manager) mountUSB(ctx context.Context, uuid, dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	m.host(ctx, "umount", dir) // left over from a run that was cut short
	if out, err := m.host(ctx, "mount", "UUID="+uuid, dir); err != nil {
		return fmt.Errorf("the USB disk is not there or cannot be mounted: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// USBDisk is a partition on a USB disk that backups can go to.
type USBDisk struct {
	UUID  string `json:"uuid"`
	Label string `json:"label,omitempty"`
	Size  string `json:"size"`
	FS    string `json:"fs"`
	Model string `json:"model,omitempty"`
}

// USBDisks lists the partitions of USB disks with a file system restic can use.
func (m *Manager) USBDisks(ctx context.Context) []USBDisk {
	out, err := m.host(ctx, "lsblk", "-J", "-o", "NAME,UUID,LABEL,SIZE,FSTYPE,TRAN,MODEL,TYPE")
	if err != nil {
		return nil
	}
	type dev struct {
		UUID, Label, Size, FSType, Tran, Model, Type string
		Children                                     []dev
	}
	var l struct{ Blockdevices []dev }
	if json.Unmarshal(out, &l) != nil {
		return nil
	}
	var disks []USBDisk
	for _, d := range l.Blockdevices {
		if d.Tran != "usb" {
			continue
		}
		for _, p := range append([]dev{d}, d.Children...) {
			if p.UUID != "" && slices.Contains([]string{"ext4", "exfat", "vfat", "ntfs", "ntfs3", "btrfs", "xfs"}, p.FSType) {
				disks = append(disks, USBDisk{UUID: p.UUID, Label: p.Label, Size: p.Size, FS: p.FSType, Model: strings.TrimSpace(d.Model)})
			}
		}
	}
	return disks
}

// Snapshots lists the backups at the destination, newest first.
func (m *Manager) Snapshots(ctx context.Context, d Destination) ([]Snapshot, error) {
	var out bytes.Buffer
	if err := m.restic(ctx, d, nil, &out, "snapshots", "--json", "--tag", "cloud"); err != nil {
		return nil, resticError(out.String(), err)
	}
	var snaps []struct {
		ShortID  string    `json:"short_id"`
		Time     time.Time `json:"time"`
		Hostname string    `json:"hostname"`
	}
	if err := json.Unmarshal(jsonPart(out.Bytes()), &snaps); err != nil {
		return nil, fmt.Errorf("restic: %w", err)
	}
	list := make([]Snapshot, 0, len(snaps))
	for i := len(snaps) - 1; i >= 0; i-- {
		list = append(list, Snapshot{ID: snaps[i].ShortID, Time: snaps[i].Time, Host: snaps[i].Hostname})
	}
	return list, nil
}

// jsonPart drops what docker prints before restic's JSON (pull progress on the first run).
func jsonPart(b []byte) []byte {
	if i := bytes.IndexAny(b, "[{"); i >= 0 {
		return b[i:]
	}
	return b
}

// resticError turns restic's output into a sentence for the page.
func resticError(out string, err error) error {
	switch {
	case strings.Contains(out, "wrong password"):
		return errors.New("wrong backup password")
	case strings.Contains(out, "Is there a repository at the following location?") || strings.Contains(out, "repository does not exist"):
		return errors.New("there are no backups there")
	case strings.Contains(out, "Permission denied") || strings.Contains(out, "permission denied"):
		return errors.New("the destination refused the board: check its key or password")
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return fmt.Errorf("%v: %s", err, lines[len(lines)-1])
}

// SetBackup checks the destination (making the repository there when it is new) and saves it. The check is a job: the first
// run pulls restic.
func (m *Manager) SetBackup(on bool, d Destination, at string) (*Job, error) {
	old := m.BackupConfig()
	if d.Password == "" && d.Kind == old.Dest.Kind { // unchanged secrets come back empty from the page
		d.Password = old.Dest.Password
	}
	if d.Secret == "" && d.Kind == "s3" {
		d.Secret = old.Dest.Secret
	}
	if !on {
		old.On = false
		return nil, m.saveBackup(old)
	}
	if err := d.check(); err != nil {
		return nil, err
	}
	if !atTime.MatchString(at) {
		return nil, errors.New("pick a time like 03:30")
	}
	j := m.newJob("backup-setup", "")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		var out bytes.Buffer
		err := m.restic(ctx, d, nil, io.MultiWriter(&out, j), "cat", "config")
		if err != nil && strings.Contains(out.String(), "wrong password") {
			err = errors.New("there are backups there with another password")
		} else if err != nil {
			fmt.Fprintln(j, "no backups there yet: starting them")
			out.Reset()
			if err = m.restic(ctx, d, nil, io.MultiWriter(&out, j), "init"); err != nil {
				err = resticError(out.String(), err)
			}
		}
		if err == nil {
			c := m.BackupConfig()
			c.On, c.Dest, c.At = true, d, at
			err = m.saveBackup(c)
		}
		j.finish(err)
	}()
	return j, nil
}

// stage is what a backup keeps besides the volumes: the manager's state, the compose project's settings and the volumes'
// labels (so compose takes them as its own after a restore).
const stageDir = "stage"

type volumeInfo struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
}

// BackupNow backs up every volume of the compose project and the manager's state. The containers stop for it, so no database
// is copied mid-write, and start again after, whatever happened.
func (m *Manager) BackupNow() (*Job, error) {
	c := m.BackupConfig()
	if !c.On {
		return nil, errors.New("backups are off")
	}
	j := m.newJob("backup", "")
	go func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
		defer cancel()
		snap, err := m.backup(ctx, c.Dest, j)
		c := m.BackupConfig()
		c.Last = &BackupRun{At: time.Now().UTC(), OK: err == nil, Snapshot: snap}
		if err != nil {
			c.Last.Error = err.Error()
		}
		m.saveBackup(c)
		j.finish(err)
	}()
	return j, nil
}

func (m *Manager) backup(ctx context.Context, d Destination, j *Job) (string, error) {
	vols, err := m.volumes(ctx)
	if err != nil {
		return "", err
	}
	stage := filepath.Join(m.StateDir, stageDir)
	if err := m.writeStage(stage, vols); err != nil {
		return "", err
	}
	mounts := []string{"-v", stage + ":/data/host:ro"}
	for _, v := range vols {
		mounts = append(mounts, "-v", v.Name+":/data/volumes/"+v.Name+":ro")
	}
	running, err := m.containers(ctx)
	if err != nil {
		return "", err
	}
	if len(running) > 0 {
		fmt.Fprintln(j, "stopping Cloud Companion for the backup")
		if err := m.Run.Run(ctx, m.ComposeDir, append([]string{"stop", "--time", "60"}, running...), j); err != nil {
			return "", fmt.Errorf("stopping: %w", err)
		}
		defer func() { // when the backup failed; otherwise they started again below
			if len(running) > 0 {
				fmt.Fprintln(j, "starting Cloud Companion again")
				m.Run.Run(context.Background(), m.ComposeDir, append([]string{"start"}, running...), j)
			}
		}()
	}
	var out bytes.Buffer
	if err := m.restic(ctx, d, mounts, io.MultiWriter(&out, j), "backup", "--tag", "cloud", "--host", "cloud", "--json", "/data"); err != nil {
		return "", resticError(out.String(), err)
	}
	snap := ""
	for _, l := range strings.Split(out.String(), "\n") {
		var s struct {
			MessageType string `json:"message_type"`
			SnapshotID  string `json:"snapshot_id"`
		}
		if json.Unmarshal([]byte(l), &s) == nil && s.MessageType == "summary" {
			snap = s.SnapshotID
		}
	}
	if len(snap) > 8 {
		snap = snap[:8]
	}
	// the containers start again before the clean-up: it can take a while and needs no stop
	if len(running) > 0 {
		fmt.Fprintln(j, "starting Cloud Companion again")
		m.Run.Run(ctx, m.ComposeDir, append([]string{"start"}, running...), j)
		running = nil
	}
	out.Reset()
	if err := m.restic(ctx, d, nil, io.MultiWriter(&out, j), "forget", "--tag", "cloud", "--keep-daily", "7", "--keep-weekly", "4", "--keep-monthly", "6", "--prune"); err != nil {
		fmt.Fprintf(j, "clearing old backups failed (the new one is kept): %v\n", resticError(out.String(), err))
	}
	if time.Now().Weekday() == time.Sunday {
		out.Reset()
		if err := m.restic(ctx, d, nil, io.MultiWriter(&out, j), "check"); err != nil {
			return snap, fmt.Errorf("the weekly check of the backups failed: %w", resticError(out.String(), err))
		}
	}
	return snap, nil
}

// volumes lists the compose project's volumes with their labels.
func (m *Manager) volumes(ctx context.Context) ([]volumeInfo, error) {
	var out bytes.Buffer
	if err := m.Run.Run(ctx, m.ComposeDir, []string{"volume", "ls", "-q", "--filter", "label=com.docker.compose.project=" + Project}, &out); err != nil {
		return nil, fmt.Errorf("docker volume ls: %w", err)
	}
	names := strings.Fields(out.String())
	if len(names) == 0 {
		return nil, nil
	}
	out.Reset()
	if err := m.Run.Run(ctx, m.ComposeDir, append([]string{"volume", "inspect", "--format", "{{json .}}"}, names...), &out); err != nil {
		return nil, fmt.Errorf("docker volume inspect: %w", err)
	}
	var vols []volumeInfo
	for _, l := range strings.Split(out.String(), "\n") {
		var v volumeInfo
		if json.Unmarshal([]byte(l), &v) == nil && v.Name != "" {
			vols = append(vols, v)
		}
	}
	return vols, nil
}

// containers lists the running containers of the compose project.
func (m *Manager) containers(ctx context.Context) ([]string, error) {
	var out bytes.Buffer
	if err := m.Run.Run(ctx, m.ComposeDir, []string{"ps", "-q", "--filter", "label=com.docker.compose.project=" + Project}, &out); err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	return strings.Fields(out.String()), nil
}

// stageFiles are the files a backup keeps besides the volumes, relative to the state folder (state:) or the compose folder.
var stageFiles = []string{"state:state.json", "state:recovery.json", ".env", "deploy/hub/.env.phecloud", "deploy/hub/.env.zpush"}

func (m *Manager) writeStage(stage string, vols []volumeInfo) error {
	os.RemoveAll(stage)
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return err
	}
	for _, f := range stageFiles {
		src := filepath.Join(m.ComposeDir, f)
		if p, ok := strings.CutPrefix(f, "state:"); ok {
			src = filepath.Join(m.StateDir, p)
		}
		b, err := os.ReadFile(src)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		dst := filepath.Join(stage, strings.ReplaceAll(strings.TrimPrefix(f, "state:"), "/", "__"))
		if err := writeFileAtomic(dst, b, 0o600); err != nil {
			return err
		}
	}
	b, _ := json.Marshal(vols)
	return writeFileAtomic(filepath.Join(stage, "volumes.json"), b, 0o600)
}

// Restore brings a board back from a backup: the volumes (made again with their compose labels), the manager's state and the
// compose project's settings; then it starts the features as they were. Only on a board that is not set up.
func (m *Manager) Restore(d Destination, snapshot string) (*Job, error) {
	if m.SetUp() {
		return nil, errors.New("this board is set up already")
	}
	if err := d.check(); err != nil {
		return nil, err
	}
	if snapshot == "" {
		snapshot = "latest"
	}
	if snapshot != "latest" && !regexp.MustCompile(`^[0-9a-f]{8,64}$`).MatchString(snapshot) {
		return nil, errors.New("bad snapshot")
	}
	j := m.newJob("restore", "")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Hour)
		defer cancel()
		err := m.restore(ctx, d, snapshot, j)
		if err == nil {
			c := m.BackupConfig()
			c.On, c.Dest = true, d
			m.saveBackup(c) // backups go on to the same place
			if err = m.Load(); err == nil {
				m.mu.Lock()
				err = m.apply(ctx, m.cloneState(), j)
				m.mu.Unlock()
			}
		}
		j.finish(err)
	}()
	return j, nil
}

func (m *Manager) restore(ctx context.Context, d Destination, snapshot string, j *Job) error {
	var out bytes.Buffer
	if err := m.restic(ctx, d, nil, &out, "dump", snapshot, "/data/host/volumes.json"); err != nil {
		return resticError(out.String(), err)
	}
	var vols []volumeInfo
	if err := json.Unmarshal(jsonPart(out.Bytes()), &vols); err != nil {
		return fmt.Errorf("the backup has no volume list: %w", err)
	}
	stage := filepath.Join(m.StateDir, stageDir)
	os.RemoveAll(stage)
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return err
	}
	mounts := []string{"-v", stage + ":/data/host"}
	for _, v := range vols {
		args := []string{"volume", "create"}
		for k, val := range v.Labels {
			args = append(args, "--label", k+"="+val)
		}
		if err := m.Run.Run(ctx, m.ComposeDir, append(args, v.Name), j); err != nil {
			return fmt.Errorf("volume %s: %w", v.Name, err)
		}
		mounts = append(mounts, "-v", v.Name+":/data/volumes/"+v.Name)
	}
	fmt.Fprintf(j, "restoring %d volumes\n", len(vols))
	out.Reset()
	if err := m.restic(ctx, d, mounts, io.MultiWriter(&out, j), "restore", snapshot, "--target", "/", "--include", "/data"); err != nil {
		return resticError(out.String(), err)
	}
	for _, f := range stageFiles {
		b, err := os.ReadFile(filepath.Join(stage, strings.ReplaceAll(strings.TrimPrefix(f, "state:"), "/", "__")))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		dst := filepath.Join(m.ComposeDir, f)
		if p, ok := strings.CutPrefix(f, "state:"); ok {
			dst = filepath.Join(m.StateDir, p)
		}
		os.MkdirAll(filepath.Dir(dst), 0o755)
		if err := writeFileAtomic(dst, b, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// Schedule runs the backup at its time each day until ctx ends (the board's time zone).
func (m *Manager) Schedule(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	last := ""
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			c := m.BackupConfig()
			day := now.Format("2006-01-02")
			if c.On && now.Format("15:04") == c.At && last != day {
				last = day
				m.BackupNow()
			}
		}
	}
}
