// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/scrypt"
)

//go:embed setup.html
var setupPage []byte

// The manager's own page (docs/host-manager.md, "First boot" and "Recovery"). It is on port 80 only while it is needed: until
// the board is set up and Cloud Companion is healthy, and again when Cloud Companion stays unhealthy. Otherwise the port is free
// (Cloud Companion's HTTPS feature uses it).
const (
	PhaseSetup    = "setup"    // not set up yet
	PhaseDone     = "done"     // set up: the page hands the owner over to Cloud Companion for a while, then goes
	PhaseOff      = "off"      // Cloud Companion is the only page
	PhaseRecovery = "recovery" // Cloud Companion has been unhealthy too long
)

// Setup serves the setup and recovery page and decides when it is shown.
type Setup struct {
	M         *Manager
	Listen    string                                                                 // ":80"
	CodeFile  string                                                                 // the setup code on the boot partition ("" or missing: the open window)
	Uptime    func() time.Duration                                                   // since the board started
	Exec      func(ctx context.Context, name string, args ...string) ([]byte, error) // timedatectl, systemctl
	Window    time.Duration                                                          // without a code: setup is open this long after boot (30 min)
	Unhealthy time.Duration                                                          // recovery after this long unhealthy (5 min)
	HandOver  time.Duration                                                          // the done page stays this long (10 min)
	Net       *Network                                                               // nil: the network is not the manager's (a plain server)
	HotspotAt time.Duration                                                          // no network this long after boot: the setup hotspot (90 s)

	mu        sync.Mutex
	phase     string
	tokens    map[string]time.Time
	fails     int
	lockUntil time.Time
	job       *Job
	badSince  time.Time
	doneAt    time.Time
	srv       *http.Server
	net       netState
	restored  bool // set up from a backup: the owner and their devices are back, so no owner code to hand over
	Now       func() time.Time
}

func (s *Setup) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Phase is what the page shows now.
func (s *Setup) Phase() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.phase
}

// Start picks the first phase; Run keeps it up to date and the page's port open or closed.
func (s *Setup) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = map[string]time.Time{}
	if s.M.SetUp() {
		s.phase = PhaseOff
	} else {
		s.phase = PhaseSetup
	}
}

// netState is what the page shows of the network during setup.
type netState struct {
	Online  bool   `json:"online"`
	Hotspot bool   `json:"hotspot"`
	Joining string `json:"joining,omitempty"` // the Wi-Fi being joined
	Error   string `json:"error,omitempty"`   // why the last join or hotspot failed
	WiFi    []WiFi `json:"wifi,omitempty"`
}

// Tick looks at Cloud Companion's health and moves between the phases (every 15 s from Run; the tests call it).
func (s *Setup) Tick(ctx context.Context) {
	if s.Net != nil && s.Phase() == PhaseSetup {
		s.tickNet(ctx)
	}
	h := s.M.Health(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	healthy := h == "healthy" || h == "running"
	switch s.phase {
	case PhaseSetup:
		if s.job != nil && s.job.Status().State == "done" && healthy {
			s.phase, s.doneAt = PhaseDone, now
		}
	case PhaseDone, PhaseOff:
		if s.phase == PhaseDone && now.Sub(s.doneAt) > s.HandOver {
			s.phase = PhaseOff
		}
		if healthy || s.M.Busy() {
			s.badSince = time.Time{}
		} else if s.badSince.IsZero() {
			s.badSince = now
		} else if now.Sub(s.badSince) > s.Unhealthy {
			s.phase, s.tokens = PhaseRecovery, map[string]time.Time{}
		}
	case PhaseRecovery:
		if healthy && (s.job == nil || s.job.Status().State != "running") {
			s.phase, s.badSince = PhaseOff, time.Time{}
		}
	}
}

// tickNet keeps the network state for the page, and opens the setup hotspot when the board has been without a network for
// HotspotAt after boot (and has Wi-Fi). A failed join opens it again.
func (s *Setup) tickNet(ctx context.Context) {
	s.mu.Lock()
	joining := s.net.Joining != ""
	s.mu.Unlock()
	if joining {
		return
	}
	online, hot := s.Net.Online(ctx), s.Net.Hotspot(ctx)
	var errMsg string
	if !online && !hot && s.Uptime() >= s.HotspotAt && s.Net.HasWiFi(ctx) {
		if err := s.Net.StartHotspot(ctx); err != nil {
			errMsg = err.Error()
		} else {
			hot = true
		}
	}
	wifi := s.Net.Scan(ctx)
	s.mu.Lock()
	s.net.Online, s.net.Hotspot, s.net.WiFi = online, hot, wifi
	if errMsg != "" {
		s.net.Error = errMsg
	}
	s.mu.Unlock()
}

// join connects to a Wi-Fi network away from the request: the hotspot, and with it the browser's connection, goes first.
func (s *Setup) join(ssid, password string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	time.Sleep(time.Second) // the answer reaches the browser before the hotspot goes
	err := s.Net.Join(ctx, ssid, password)
	if err != nil {
		s.Net.StartHotspot(ctx)
	}
	s.mu.Lock()
	s.net.Joining = ""
	s.net.Error = ""
	if err != nil {
		s.net.Error = "Could not join " + ssid + ": " + err.Error()
	}
	s.mu.Unlock()
	s.tickNet(ctx)
}

// Run serves the page while the phase needs it, until ctx ends.
func (s *Setup) Run(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		s.serve(s.Phase() != PhaseOff)
		select {
		case <-ctx.Done():
			s.serve(false)
			return
		case <-t.C:
			s.Tick(ctx)
		}
	}
}

func (s *Setup) serve(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case on && s.srv == nil:
		l, err := net.Listen("tcp", s.Listen)
		if err != nil {
			return // tried again on the next tick
		}
		s.srv = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
		go s.srv.Serve(l)
	case !on && s.srv != nil:
		s.srv.Close()
		s.srv = nil
	}
}

func (s *Setup) setupCode() string {
	if s.CodeFile == "" {
		return ""
	}
	b, err := os.ReadFile(s.CodeFile)
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if v, ok := strings.CutPrefix(l, "code="); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// Handler is the page and its calls (/api/...).
func (s *Setup) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:")
		w.Write(setupPage)
	})
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		phase, job, unlocked, restored := s.phase, s.job, s.unlocked(r), s.restored
		s.mu.Unlock()
		out := map[string]any{"phase": phase, "unlocked": unlocked, "board": s.M.Board}
		if phase == PhaseSetup {
			out["needsCode"] = s.setupCode() != ""
			out["windowOpen"] = s.setupCode() == "" && s.Uptime() < s.Window
		}
		if unlocked {
			var fs []FeatureInfo
			for _, f := range s.M.Features() {
				if !f.Core && !f.Later {
					fs = append(fs, f)
				}
			}
			out["features"] = fs
			if phase == PhaseSetup {
				if k, err := s.M.SSHKey(); err == nil {
					out["sshKey"] = k
				}
			}
			if s.Net != nil && phase == PhaseSetup {
				s.mu.Lock()
				out["network"] = s.net
				s.mu.Unlock()
			}
			if job != nil {
				out["job"] = job.Status()
			}
			if phase == PhaseDone {
				out["open"] = s.ownerLink(r, restored)
				out["restored"] = restored
			}
		}
		writeJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("POST /api/unlock", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Secret string }
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.now().Before(s.lockUntil) {
			writeError(w, http.StatusTooManyRequests, errors.New("too many tries: wait a few minutes"))
			return
		}
		ok := false
		switch s.phase {
		case PhaseSetup, PhaseDone:
			if c := s.setupCode(); c != "" {
				ok = subtle.ConstantTimeCompare([]byte(strings.TrimSpace(in.Secret)), []byte(c)) == 1
			} else {
				ok = s.Uptime() < s.Window
			}
		case PhaseRecovery:
			ok = s.checkPassphrase(in.Secret)
		}
		if !ok {
			if s.fails++; s.fails >= 10 {
				s.fails, s.lockUntil = 0, s.now().Add(10*time.Minute)
			}
			writeError(w, http.StatusForbidden, errors.New("that is not it"))
			return
		}
		s.fails = 0
		b := make([]byte, 24)
		rand.Read(b)
		tok := hex.EncodeToString(b)
		s.tokens[tok] = s.now()
		http.SetCookie(w, &http.Cookie{Name: "phehost", Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		writeJSON(w, http.StatusOK, map[string]any{})
	})
	mux.HandleFunc("POST /api/setup", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Passphrase string   `json:"passphrase"`
			Features   []string `json:"features"`
			TimeZone   string   `json:"timeZone"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.phase != PhaseSetup || !s.unlocked(r) {
			writeError(w, http.StatusForbidden, errors.New("not now"))
			return
		}
		if s.job != nil && s.job.Status().State == "running" {
			writeError(w, http.StatusConflict, errors.New("already setting up"))
			return
		}
		if s.Net != nil && !s.net.Online {
			writeError(w, http.StatusConflict, errors.New("connect the board to the internet first"))
			return
		}
		if len([]rune(in.Passphrase)) < 10 {
			writeError(w, http.StatusBadRequest, errors.New("the recovery passphrase needs at least 10 characters"))
			return
		}
		if in.TimeZone != "" && !validZone(in.TimeZone) {
			writeError(w, http.StatusBadRequest, errors.New("unknown time zone"))
			return
		}
		if err := s.savePassphrase(in.Passphrase); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if in.TimeZone != "" && s.Exec != nil {
			if out, err := s.Exec(r.Context(), "timedatectl", "set-timezone", in.TimeZone); err != nil && !errors.Is(err, exec.ErrNotFound) { // none: not a systemd board
				writeError(w, http.StatusInternalServerError, fmt.Errorf("time zone: %v: %s", err, out))
				return
			}
		}
		for _, id := range in.Features {
			if f := s.M.Cat.Get(id); f != nil && f.Later {
				writeError(w, http.StatusBadRequest, fmt.Errorf("%s is switched on after setup, in Settings", f.Title))
				return
			}
		}
		j, err := s.M.Choose(in.Features, nil)
		if err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
		s.job = j
		writeJSON(w, http.StatusAccepted, j.Status())
	})
	// a fresh board restored from a backup instead of set up
	mux.HandleFunc("POST /api/restore/snapshots", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Dest Destination `json:"dest"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if !s.restoreAllowed(w, r) {
			return
		}
		if err := in.Dest.check(); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		snaps, err := s.M.Snapshots(r.Context(), in.Dest)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"snapshots": snaps})
	})
	mux.HandleFunc("GET /api/restore/usb", func(w http.ResponseWriter, r *http.Request) {
		if !s.restoreAllowed(w, r) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"disks": s.M.USBDisks(r.Context())})
	})
	mux.HandleFunc("POST /api/restore", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Dest     Destination `json:"dest"`
			Snapshot string      `json:"snapshot"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if !s.restoreAllowed(w, r) {
			return
		}
		j, err := s.M.Restore(in.Dest, in.Snapshot)
		if err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
		s.mu.Lock()
		s.job, s.restored = j, true
		s.mu.Unlock()
		writeJSON(w, http.StatusAccepted, j.Status())
	})
	mux.HandleFunc("POST /api/wifi", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			SSID     string `json:"ssid"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil || in.SSID == "" {
			writeError(w, http.StatusBadRequest, errors.New("pick a network"))
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.Net == nil || s.phase != PhaseSetup || !s.unlocked(r) {
			writeError(w, http.StatusForbidden, errors.New("not now"))
			return
		}
		if s.net.Joining != "" {
			writeError(w, http.StatusConflict, errors.New("already joining "+s.net.Joining))
			return
		}
		s.net.Joining, s.net.Error = in.SSID, ""
		go s.join(in.SSID, in.Password)
		writeJSON(w, http.StatusAccepted, map[string]any{})
	})
	// phones look for a sign-in page on the hotspot: every other address shows this page
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		hot := s.net.Hotspot
		s.mu.Unlock()
		if !hot {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "http://10.42.0.1/", http.StatusFound)
	})
	// recovery
	mux.HandleFunc("POST /api/retry", func(w http.ResponseWriter, r *http.Request) {
		if !s.recoveryUnlocked(w, r) {
			return
		}
		j := s.M.Apply()
		s.mu.Lock()
		s.job = j
		s.mu.Unlock()
		writeJSON(w, http.StatusAccepted, j.Status())
	})
	mux.HandleFunc("GET /api/logs", func(w http.ResponseWriter, r *http.Request) {
		if !s.recoveryUnlocked(w, r) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		writeJSON(w, http.StatusOK, map[string]any{"log": s.M.Logs(ctx, "phe-cloud", 200)})
	})
	mux.HandleFunc("POST /api/reboot", func(w http.ResponseWriter, r *http.Request) {
		if !s.recoveryUnlocked(w, r) {
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{})
		if s.Exec != nil {
			go s.Exec(context.Background(), "systemctl", "reboot")
		}
	})
	return mux
}

// restoreAllowed: setting up (not yet set up), unlocked, online, and no setup or restore running.
func (s *Setup) restoreAllowed(w http.ResponseWriter, r *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.phase != PhaseSetup || !s.unlocked(r):
		writeError(w, http.StatusForbidden, errors.New("not now"))
	case s.Net != nil && !s.net.Online:
		writeError(w, http.StatusConflict, errors.New("connect the board to the internet first"))
	case s.job != nil && s.job.Status().State == "running":
		writeError(w, http.StatusConflict, errors.New("already setting up"))
	default:
		return true
	}
	return false
}

func (s *Setup) recoveryUnlocked(w http.ResponseWriter, r *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase != PhaseRecovery || !s.unlocked(r) {
		writeError(w, http.StatusForbidden, errors.New("not now"))
		return false
	}
	return true
}

// unlocked: the request carries a token from /api/unlock (setup: 2 hours; recovery: 30 minutes). s.mu is held.
func (s *Setup) unlocked(r *http.Request) bool {
	c, err := r.Cookie("phehost")
	if err != nil {
		return false
	}
	t, ok := s.tokens[c.Value]
	ttl := 2 * time.Hour
	if s.phase == PhaseRecovery {
		ttl = 30 * time.Minute
	}
	return ok && s.now().Sub(t) < ttl
}

// ownerLink is Cloud Companion's pairing page with the first owner's code, at the address the browser used for this page.
func (s *Setup) ownerLink(r *http.Request, restored bool) string {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if restored {
		return "http://" + host + ":8080/"
	}
	return "http://" + host + ":8080/#/pair?code=" + s.M.Value("core", "first-owner")
}

var zoneName = regexp.MustCompile(`^[A-Za-z_]+(/[A-Za-z0-9_+-]+){0,2}$`)

func validZone(z string) bool {
	if z == "UTC" {
		return true
	}
	if !zoneName.MatchString(z) {
		return false
	}
	_, err := time.LoadLocation(z)
	return err == nil
}

type passphrase struct {
	Salt []byte `json:"salt"`
	Hash []byte `json:"hash"`
}

func (s *Setup) savePassphrase(p string) error {
	salt := make([]byte, 16)
	rand.Read(salt)
	h, err := scrypt.Key([]byte(p), salt, 1<<15, 8, 1, 32)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(passphrase{salt, h})
	return writeFileAtomic(filepath.Join(s.M.StateDir, "recovery.json"), b, 0o600)
}

func (s *Setup) checkPassphrase(p string) bool {
	b, err := os.ReadFile(filepath.Join(s.M.StateDir, "recovery.json"))
	if err != nil {
		return false
	}
	var rp passphrase
	if json.Unmarshal(b, &rp) != nil {
		return false
	}
	h, err := scrypt.Key([]byte(p), rp.Salt, 1<<15, 8, 1, 32)
	return err == nil && subtle.ConstantTimeCompare(h, rp.Hash) == 1
}
