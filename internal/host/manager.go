// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Project is the compose project every feature runs in.
const Project = "cloud"

// Runner runs docker (docker compose ...); the tests use a fake one.
type Runner interface {
	Run(ctx context.Context, dir string, args []string, out io.Writer) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, dir string, args []string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, out, out
	return cmd.Run()
}

// State is what the owner chose, kept in state.json: the features switched on and their settings.
type State struct {
	Features map[string]map[string]string `json:"features"`
}

// Manager switches features on and off. One change runs at a time; each is a job whose progress can be read.
type Manager struct {
	Cat        *Catalogue
	Board      Board
	ComposeDir string // docker-compose.yml and the files the features name; the manager writes .env here
	StateDir   string
	Run        Runner
	Host       func(ctx context.Context, name string, args ...string) ([]byte, error) // mount, lsblk (backups to a USB disk)

	SSHUser string // the board's account remote login unlocks (ssh.go); "" = no switch (a server set up by hand)

	// updates (update.go)
	Releases   string        // GitHub's release list; "" = DefaultReleases
	HTTP       *http.Client  // nil = a plain one
	Binary     string        // the manager's own program, replaced by an update; "" = never replaced (a checkout)
	Restart    func()        // hands over to a new manager (exits: systemd starts it)
	HealthWait time.Duration // how long a new release may take to be healthy; 0 = ten minutes

	mu    sync.Mutex // the state and the compose project
	jmu   sync.Mutex
	jobs  map[string]*Job
	jseq  int
	state State
}

func (m *Manager) Load() error {
	m.jobs = map[string]*Job{}
	m.state = State{Features: map[string]map[string]string{}}
	b, err := os.ReadFile(filepath.Join(m.StateDir, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &m.state); err != nil {
		return fmt.Errorf("state.json: %w", err)
	}
	if m.state.Features == nil {
		m.state.Features = map[string]map[string]string{}
	}
	return nil
}

func (m *Manager) save(s State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(m.StateDir, "state.json"), b, 0o600)
}

// FeatureInfo is a feature as the Features page shows it.
type FeatureInfo struct {
	*Feature
	On       bool              `json:"on"`
	Fits     bool              `json:"fits"`
	Why      string            `json:"why,omitempty"`
	Values   map[string]string `json:"values,omitempty"` // its settings, secrets left out
	Required []string          `json:"requiredBy,omitempty"`
}

func (m *Manager) Features() []FeatureInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	on, _ := m.Cat.Closure(m.enabled(m.state))
	var out []FeatureInfo
	for _, id := range m.Cat.IDs() {
		f := m.Cat.Get(id)
		fi := FeatureInfo{Feature: f, On: slices.Contains(on, id)}
		fi.Fits, fi.Why = f.Fits(m.Board)
		for _, s := range f.Settings {
			if v, ok := m.state.Features[id][s.Key]; ok && !s.Secret {
				if fi.Values == nil {
					fi.Values = map[string]string{}
				}
				fi.Values[s.Key] = v
			}
		}
		for _, o := range on {
			if slices.Contains(m.Cat.Get(o).Requires, id) {
				fi.Required = append(fi.Required, o)
			}
		}
		out = append(out, fi)
	}
	return out
}

func (m *Manager) enabled(s State) []string {
	ids := make([]string, 0, len(s.Features))
	for id := range s.Features {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Enable switches a feature on (and what it requires) with its settings; the work runs as a job.
func (m *Manager) Enable(id string, values map[string]string) (*Job, error) {
	return m.Choose([]string{id}, map[string]map[string]string{id: values})
}

// Choose switches features on (and what they require) with their settings, in one change: the setup's choices.
func (m *Manager) Choose(want []string, values map[string]map[string]string) (*Job, error) {
	for _, id := range want {
		if m.Cat.Get(id) == nil {
			return nil, ErrNotFound
		}
	}
	m.mu.Lock()
	next := m.cloneState()
	m.mu.Unlock()
	ids, err := m.Cat.Closure(append(m.enabled(next), want...))
	if err != nil {
		return nil, err
	}
	for _, x := range ids {
		g := m.Cat.Get(x)
		if ok, why := g.Fits(m.Board); !ok {
			return nil, fmt.Errorf("%s: %s", g.Title, why)
		}
		for _, c := range g.Conflicts {
			if slices.Contains(ids, c) {
				return nil, fmt.Errorf("%s can't run at the same time as %s", g.Title, m.Cat.Get(c).Title)
			}
		}
		if next.Features[x] == nil {
			next.Features[x] = map[string]string{}
		}
		vals := next.Features[x]
		for _, s := range g.Settings {
			if v, ok := values[x][s.Key]; ok && !s.Generate {
				vals[s.Key] = v
			}
			if s.Generate && vals[s.Key] == "" {
				vals[s.Key] = randomSecret()
			}
			if s.Required && vals[s.Key] == "" {
				return nil, fmt.Errorf("%s: %s is required", g.Title, s.Label)
			}
		}
	}
	feature := ""
	if len(want) == 1 {
		feature = want[0]
	}
	return m.start("enable", feature, next), nil
}

// Value is a setting of a feature that is on ("" when there is none).
func (m *Manager) Value(id, key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.Features[id][key]
}

// SetUp says whether the board has been set up (features were chosen and saved).
func (m *Manager) SetUp() bool {
	_, err := os.Stat(filepath.Join(m.StateDir, "state.json"))
	return err == nil
}

// Health is the state of Cloud Companion's container as Docker sees it: healthy, starting, unhealthy, running (no health
// check), exited and the like when it is not running, or missing.
func (m *Manager) Health(ctx context.Context) string {
	var out bytes.Buffer
	if err := m.Run.Run(ctx, m.ComposeDir, []string{"inspect", "-f", `{{if ne .State.Status "running"}}{{.State.Status}}{{else if .State.Health}}{{.State.Health.Status}}{{else}}running{{end}}`, "phe-cloud"}, &out); err != nil {
		return "missing"
	}
	return strings.TrimSpace(out.String())
}

// Logs is the end of a container's log.
func (m *Manager) Logs(ctx context.Context, container string, lines int) string {
	var out bytes.Buffer
	m.Run.Run(ctx, m.ComposeDir, []string{"logs", "--tail", strconv.Itoa(lines), container}, &out)
	return out.String()
}

// Busy says whether a change is running.
func (m *Manager) Busy() bool {
	m.jmu.Lock()
	defer m.jmu.Unlock()
	for _, j := range m.jobs {
		if j.Status().State == "running" {
			return true
		}
	}
	return false
}

// Disable switches a feature off; its data (volumes) stays.
func (m *Manager) Disable(id string) (*Job, error) {
	f := m.Cat.Get(id)
	if f == nil {
		return nil, ErrNotFound
	}
	if f.Core {
		return nil, fmt.Errorf("%s is always on", f.Title)
	}
	m.mu.Lock()
	next := m.cloneState()
	m.mu.Unlock()
	on, _ := m.Cat.Closure(m.enabled(next))
	for _, o := range on {
		if o != id && slices.Contains(m.Cat.Get(o).Requires, id) && next.Features[o] != nil {
			return nil, fmt.Errorf("%s needs %s. Turn it off first", m.Cat.Get(o).Title, f.Title)
		}
	}
	delete(next.Features, id)
	return m.start("disable", id, next), nil
}

// Apply brings the compose project to the saved state (at start, and after an update).
func (m *Manager) Apply() *Job {
	m.mu.Lock()
	s := m.cloneState()
	m.mu.Unlock()
	return m.start("apply", "", s)
}

func (m *Manager) cloneState() State {
	s := State{Features: map[string]map[string]string{}}
	for id, v := range m.state.Features {
		c := map[string]string{}
		for k, x := range v {
			c[k] = x
		}
		s.Features[id] = c
	}
	return s
}

// start runs a change as a job: the new state is applied; when that fails the old one is put back.
func (m *Manager) start(kind, feature string, next State) *Job {
	j := m.newJob(kind, feature)
	go func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		prev := m.cloneState()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		err := m.apply(ctx, next, j)
		if err == nil {
			err = m.save(next)
		}
		if err != nil {
			fmt.Fprintf(j, "failed: %v; going back to how it was\n", err)
			if rerr := m.apply(ctx, prev, j); rerr != nil {
				fmt.Fprintf(j, "going back failed too: %v\n", rerr)
			}
			j.finish(err)
			return
		}
		m.state = next
		j.finish(nil)
	}()
	return j
}

// apply writes .env and runs the compose project with the profiles of the state's features.
func (m *Manager) apply(ctx context.Context, s State, j *Job) error {
	ids, err := m.Cat.Closure(m.enabled(s))
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(m.ComposeDir, ".env"), m.envFile(s, ids), 0o600); err != nil {
		return err
	}
	args := ComposeArgs(m.Cat, ids)
	fmt.Fprintf(j, "docker %s\n", strings.Join(args, " "))
	if err := m.Run.Run(ctx, m.ComposeDir, args, j); err != nil {
		return err
	}
	// a feature switched off: its services are not orphans to compose (only their profile is off), so their containers go here
	on, err := m.services(ctx, ids)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	ps := []string{"ps", "-a", "--filter", "label=com.docker.compose.project=" + Project, "--format", `{{.ID}} {{.Label "com.docker.compose.service"}}`}
	if err := m.Run.Run(ctx, m.ComposeDir, ps, &out); err != nil {
		return fmt.Errorf("docker ps: %w", err)
	}
	var off, names []string
	for _, l := range strings.Split(out.String(), "\n") {
		if f := strings.Fields(l); len(f) == 2 && !slices.Contains(on, f[1]) {
			off, names = append(off, f[0]), append(names, f[1])
		}
	}
	if len(off) == 0 {
		return nil
	}
	fmt.Fprintf(j, "removing %s\n", strings.Join(names, ", "))
	return m.Run.Run(ctx, m.ComposeDir, append([]string{"rm", "--force"}, off...), j)
}

// services lists the compose services of the features.
func (m *Manager) services(ctx context.Context, ids []string) ([]string, error) {
	var out bytes.Buffer
	if err := m.Run.Run(ctx, m.ComposeDir, append(projectArgs(m.Cat, ids), "config", "--services"), &out); err != nil {
		return nil, fmt.Errorf("compose config: %w: %s", err, strings.TrimSpace(out.String()))
	}
	return strings.Fields(out.String()), nil
}

// ComposeArgs is the docker command line for the features (pull and start what changed, remove what is no longer used).
func ComposeArgs(c *Catalogue, ids []string) []string {
	return append(projectArgs(c, ids), "up", "-d", "--pull", "missing", "--remove-orphans", "--wait")
}

// projectArgs is "compose" with the project, the files and the profiles of the features.
func projectArgs(c *Catalogue, ids []string) []string {
	args := []string{"compose", "-p", Project, "-f", "docker-compose.yml"}
	var files, profiles []string
	for _, id := range ids {
		f := c.Get(id)
		for _, x := range f.Files {
			if !slices.Contains(files, x) {
				files = append(files, x)
			}
		}
		for _, p := range f.Profiles {
			if !slices.Contains(profiles, p) {
				profiles = append(profiles, p)
			}
		}
	}
	for _, x := range files {
		args = append(args, "-f", x)
	}
	for _, p := range profiles {
		args = append(args, "--profile", p)
	}
	return args
}

func (m *Manager) envFile(s State, ids []string) []byte {
	vars := map[string]string{}
	for _, id := range ids {
		f := m.Cat.Get(id)
		for _, st := range f.Settings {
			vars[st.Env] = s.Features[id][st.Key]
		}
	}
	for _, id := range ids {
		for k, v := range m.Cat.Get(id).Env {
			vars[k] = os.Expand(v, func(n string) string { return vars[n] })
		}
	}
	// the release the board runs (update.go): its images, not the compose files' fallback
	if v := m.Version(); v != "" {
		vars["PHECLOUD_VERSION"] = v
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b bytes.Buffer
	b.WriteString("# written by the host manager (phehost): change features in Cloud Companion, not here\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, strconv.Quote(vars[k]))
	}
	return b.Bytes()
}

var ErrNotFound = errors.New("no such feature")

func randomSecret() string {
	b := make([]byte, 24)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func writeFileAtomic(name string, b []byte, mode os.FileMode) error {
	tmp := name + ".tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, name)
}

// Job is one change in progress: its output lines and how it ended.
type Job struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Feature string `json:"feature,omitempty"`

	mu    sync.Mutex
	lines []string
	part  string
	done  bool
	err   error
	ch    chan struct{} // closed when done
}

func (m *Manager) newJob(kind, feature string) *Job {
	m.jmu.Lock()
	defer m.jmu.Unlock()
	m.jseq++
	j := &Job{ID: strconv.Itoa(m.jseq), Kind: kind, Feature: feature, ch: make(chan struct{})}
	m.jobs[j.ID] = j
	return j
}

func (m *Manager) Job(id string) *Job {
	m.jmu.Lock()
	defer m.jmu.Unlock()
	return m.jobs[id]
}

func (j *Job) Write(p []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	s := j.part + string(p)
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			break
		}
		j.lines = append(j.lines, strings.TrimRight(s[:i], "\r"))
		s = s[i+1:]
	}
	j.part = s
	return len(p), nil
}

func (j *Job) finish(err error) {
	j.mu.Lock()
	if j.part != "" {
		j.lines, j.part = append(j.lines, j.part), ""
	}
	j.done, j.err = true, err
	j.mu.Unlock()
	close(j.ch)
}

// Done is closed when the job has ended.
func (j *Job) Done() <-chan struct{} { return j.ch }

// JobStatus is a job as the API returns it.
type JobStatus struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Feature string   `json:"feature,omitempty"`
	State   string   `json:"state"` // running, done, failed
	Error   string   `json:"error,omitempty"`
	Log     []string `json:"log"`
}

func (j *Job) Status() JobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	st := JobStatus{ID: j.ID, Kind: j.Kind, Feature: j.Feature, State: "running", Log: append([]string{}, j.lines...)} // [] not null: the pages spread it
	if j.done {
		st.State = "done"
		if j.err != nil {
			st.State, st.Error = "failed", j.err.Error()
		}
	}
	return st
}
