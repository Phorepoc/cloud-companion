// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestSSHSwitch(t *testing.T) {
	var calls []string
	chpasswd := ""
	m := &Manager{StateDir: t.TempDir(), SSHUser: "cloud", Host: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
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
	if _, err := m.SetSSH(ctx, true, ""); err == nil {
		t.Error("on the first time without a password")
	}
	if _, err := m.SetSSH(ctx, true, "short"); err == nil {
		t.Error("a short password taken")
	}
	s, err := m.SetSSH(ctx, true, "my own password")
	if err != nil || !s.On || s.User != "cloud" || !s.HasPassword {
		t.Fatalf("on: %+v %v", s, err)
	}
	all := strings.Join(calls, "\n")
	if chpasswd != "cloud:my own password\n" || strings.Contains(all, "my own password") {
		t.Error("the password went another way than chpasswd's file, or is on a command line")
	}
	if !strings.Contains(all, "passwd -u cloud") || !strings.Contains(all, "systemctl enable --now ssh") {
		t.Errorf("not unlocked and started: %s", all)
	}
	if _, err := os.Stat(m.StateDir + "/.chpasswd"); err == nil {
		t.Error("the password file stayed")
	}
	calls = nil
	if s, _ := m.SetSSH(ctx, false, ""); s.On || !s.HasPassword {
		t.Errorf("off: %+v", s)
	}
	if c := strings.Join(calls, "\n"); !strings.Contains(c, "passwd -l cloud") || !strings.Contains(c, "systemctl disable --now ssh") {
		t.Errorf("not locked and stopped: %s", c)
	}
	// on again: the same password, not asked again
	calls, chpasswd = nil, ""
	if s, err := m.SetSSH(ctx, true, ""); err != nil || !s.On || chpasswd != "" {
		t.Errorf("on again: %+v %v %q", s, err, chpasswd)
	}
	if _, err := (&Manager{StateDir: t.TempDir()}).SetSSH(ctx, true, "my own password"); err == nil {
		t.Error("a switch on a server set up by hand")
	}
}
