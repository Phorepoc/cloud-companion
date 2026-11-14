// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package host

import (
	"context"
	"encoding/json"
	"errors"
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

func TestSSHSwitch(t *testing.T) {
	var calls []string
	chpasswd := ""
	m := &Manager{StateDir: t.TempDir(), SSHUser: "cloud", Host: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "id" {
			if args[1] == "root" {
				return []byte("0"), nil
			}
			return nil, errors.New("no such user")
		}
		if name == "sh" {
			b, _ := os.ReadFile(args[2])
			chpasswd = string(b)
		}
		return nil, nil
	}}
	ctx := context.Background()
	if s := m.SSH(); s.On || s.HasPassword {
		t.Fatal("on by default")
	}
	if _, err := m.SetSSH(ctx, true, "anna", ""); err == nil {
		t.Error("on the first time without a password")
	}
	if _, err := m.SetSSH(ctx, true, "anna", "short"); err == nil {
		t.Error("a short password taken")
	}
	for _, bad := range []string{"Anna", "1anna", "an na", "root"} {
		if _, err := m.SetSSH(ctx, true, bad, "my own password"); err == nil {
			t.Errorf("user name %q taken", bad)
		}
	}
	s, err := m.SetSSH(ctx, true, "anna", "my own password")
	if err != nil || !s.On || s.User != "anna" || !s.HasPassword {
		t.Fatalf("on: %+v %v", s, err)
	}
	all := strings.Join(calls, "\n")
	if !strings.Contains(all, "usermod -l anna -d /home/anna -m cloud") || !strings.Contains(all, "groupmod -n anna cloud") {
		t.Errorf("not renamed: %s", all)
	}
	if chpasswd != "anna:my own password\n" || strings.Contains(all, "my own password") {
		t.Error("the password went another way than chpasswd's file, or is on a command line")
	}
	if !strings.Contains(all, "passwd -u anna") || !strings.Contains(all, "systemctl enable --now ssh") {
		t.Errorf("not unlocked and started: %s", all)
	}
	if left, _ := filepath.Glob(m.StateDir + "/.chpasswd*"); len(left) > 0 {
		t.Errorf("the password file stayed: %v", left)
	}
	calls = nil
	if s, _ := m.SetSSH(ctx, false, "", ""); s.On || !s.HasPassword {
		t.Errorf("off: %+v", s)
	}
	if c := strings.Join(calls, "\n"); !strings.Contains(c, "passwd -l anna") || !strings.Contains(c, "systemctl disable --now ssh") {
		t.Errorf("not locked and stopped: %s", c)
	}
	// on again: the same password, not asked again
	calls, chpasswd = nil, ""
	if s, err := m.SetSSH(ctx, true, "", ""); err != nil || !s.On || s.User != "anna" || chpasswd != "" {
		t.Errorf("on again: %+v %v %q", s, err, chpasswd)
	}
	if _, err := (&Manager{StateDir: t.TempDir()}).SetSSH(ctx, true, "anna", "my own password"); err == nil {
		t.Error("a switch on a server set up by hand")
	}
}

// switching off when the service will not stop: the reply still says Off (that is what is stored), with what went wrong, so the
// page does not keep showing On
func TestSSHOffThatFailsSaysOff(t *testing.T) {
	m := &Manager{StateDir: t.TempDir(), SSHUser: "cloud", Host: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "id" {
			return nil, errors.New("no such user")
		}
		if name == "systemctl" && args[0] == "disable" {
			return []byte("unit busy"), errors.New("exit status 1")
		}
		return nil, nil
	}}
	if _, err := m.SetSSH(context.Background(), true, "anna", "my own password"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	req, _ := http.NewRequest("PUT", srv.URL+"/v1/ssh", strings.NewReader(`{"on":false}`))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		SSH     SSHState `json:"ssh"`
		Warning string   `json:"warning"`
	}
	json.NewDecoder(res.Body).Decode(&out)
	if res.StatusCode != 200 || out.SSH.On || !strings.Contains(out.Warning, "unit busy") {
		t.Errorf("off that failed: %d %+v", res.StatusCode, out)
	}
}

// two browsers changing remote login at once: one after the other, never both renaming the account
func TestSSHChangesOneAtATime(t *testing.T) {
	var in, most int32
	m := &Manager{StateDir: t.TempDir(), SSHUser: "cloud"}
	m.Host = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "id" {
			return nil, errors.New("no such user")
		}
		if name == "usermod" {
			n := atomic.AddInt32(&in, 1)
			for o := atomic.LoadInt32(&most); n > o && !atomic.CompareAndSwapInt32(&most, o, n); o = atomic.LoadInt32(&most) {
			}
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&in, -1)
		}
		return nil, nil
	}
	var wg sync.WaitGroup
	for _, u := range []string{"anna", "bert"} {
		wg.Add(1)
		go func() { defer wg.Done(); m.SetSSH(context.Background(), true, u, "my own password") }()
	}
	wg.Wait()
	if most > 1 {
		t.Error("two renames ran at once")
	}
	if s := m.SSH(); s.User != "anna" && s.User != "bert" {
		t.Errorf("user: %q", s.User)
	}
}
