// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
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
	if _, err := os.Stat(m.StateDir + "/.chpasswd"); err == nil {
		t.Error("the password file stayed")
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
