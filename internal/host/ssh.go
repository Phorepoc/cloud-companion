// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Remote login (SSH), for looking into a board: off by default. The first time it is switched on, the owner chooses the
// password of the board's user account (SSHUser, "cloud" on the Pi image, locked otherwise); the SSH service starts. Off, the
// account is locked again (its password kept, not readable) and the service stops; on again, it is unlocked with the same
// password. A server set up by hand has no switch: its owner has their own login.

type SSHState struct {
	On          bool   `json:"on"`
	User        string `json:"user,omitempty"`
	HasPassword bool   `json:"hasPassword"` // chosen before: switching on again needs none
}

func (m *Manager) sshFile() string { return filepath.Join(m.StateDir, "ssh.json") }

// SSH says whether remote login is on.
func (m *Manager) SSH() SSHState {
	var s SSHState
	if b, err := os.ReadFile(m.sshFile()); err == nil {
		json.Unmarshal(b, &s)
	}
	s.User = m.SSHUser
	return s
}

// SetSSH switches remote login on or off. password sets (or changes) the account's password; it is needed the first time.
func (m *Manager) SetSSH(ctx context.Context, on bool, password string) (SSHState, error) {
	if m.SSHUser == "" {
		return SSHState{}, errors.New("remote login is switched on the board's own way on a server set up by hand")
	}
	cur := m.SSH()
	if !on {
		var errs []error
		if _, err := m.host(ctx, "passwd", "-l", m.SSHUser); err != nil {
			errs = append(errs, err)
		}
		if out, err := m.host(ctx, "systemctl", "disable", "--now", "ssh"); err != nil {
			errs = append(errs, fmt.Errorf("%v: %s", err, out))
		}
		b, _ := json.Marshal(SSHState{HasPassword: cur.HasPassword})
		writeFileAtomic(m.sshFile(), b, 0o600)
		return m.SSH(), errors.Join(errs...)
	}
	if password == "" && !cur.HasPassword {
		return cur, errors.New("choose a password for remote login")
	}
	if password != "" && (len(password) < 10 || strings.ContainsAny(password, ":\n\r")) {
		return cur, errors.New("the password needs 10 characters or more (and no colon)")
	}
	if password != "" {
		// chpasswd reads "user:password" from a file only root can read: the password never shows in a process list
		tmp := filepath.Join(m.StateDir, ".chpasswd")
		if err := os.WriteFile(tmp, []byte(m.SSHUser+":"+password+"\n"), 0o600); err != nil {
			return cur, err
		}
		_, err := m.host(ctx, "sh", "-c", `chpasswd < "$0"`, tmp)
		os.Remove(tmp)
		if err != nil {
			return cur, fmt.Errorf("setting the password: %w", err)
		}
	}
	if out, err := m.host(ctx, "passwd", "-u", m.SSHUser); err != nil {
		return cur, fmt.Errorf("unlocking %s: %v: %s", m.SSHUser, err, strings.TrimSpace(string(out)))
	}
	if out, err := m.host(ctx, "systemctl", "enable", "--now", "ssh"); err != nil {
		m.host(ctx, "passwd", "-l", m.SSHUser)
		return cur, fmt.Errorf("starting SSH: %v: %s", err, strings.TrimSpace(string(out)))
	}
	b, _ := json.Marshal(SSHState{On: true, HasPassword: true})
	if err := writeFileAtomic(m.sshFile(), b, 0o600); err != nil {
		return cur, err
	}
	return m.SSH(), nil
}
