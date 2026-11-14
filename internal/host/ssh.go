// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Remote login (SSH), for looking into a board: off by default. The first time it is switched on, the owner chooses a user name
// and a password: the board's account (SSHUser, "cloud" on the Pi image, locked until then) is renamed to it, home folder
// included, and the SSH service starts. Off, the account is locked again (its password kept, not readable) and the service
// stops; on again, it is unlocked with the same password. A server set up by hand has no switch: its owner has their own login.

type SSHState struct {
	On          bool   `json:"on"`
	User        string `json:"user,omitempty"` // the name chosen; SSHUser until then
	HasPassword bool   `json:"hasPassword"`    // chosen before: switching on again needs none
}

func (m *Manager) sshFile() string { return filepath.Join(m.StateDir, "ssh.json") }

// SSH says whether remote login is on, and the user to log in as.
func (m *Manager) SSH() SSHState {
	var s SSHState
	if b, err := os.ReadFile(m.sshFile()); err == nil {
		json.Unmarshal(b, &s)
	}
	if s.User == "" {
		s.User = m.SSHUser
	}
	return s
}

func (m *Manager) saveSSH(s SSHState) error {
	b, _ := json.Marshal(s)
	return writeFileAtomic(m.sshFile(), b, 0o600)
}

var userName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// SetSSH switches remote login on or off. password sets (or changes) the account's password; it is needed the first time,
// with the user name (after that the name stays).
func (m *Manager) SetSSH(ctx context.Context, on bool, user, password string) (SSHState, error) {
	if m.SSHUser == "" {
		return SSHState{}, errors.New("SSH can only be managed here on servers installed from a Cloud Companion image")
	}
	// two browsers changing it at once could both rename the account, or set each other's password
	m.smu.Lock()
	defer m.smu.Unlock()
	cur := m.SSH()
	if !on {
		var errs []error
		if out, err := m.host(ctx, "passwd", "-l", cur.User); err != nil {
			errs = append(errs, fmt.Errorf("%v: %s", err, out))
		}
		if out, err := m.host(ctx, "systemctl", "disable", "--now", "ssh"); err != nil {
			errs = append(errs, fmt.Errorf("%v: %s", err, out))
		}
		cur.On = false
		m.saveSSH(cur)
		return m.SSH(), errors.Join(errs...)
	}
	if password == "" && !cur.HasPassword {
		return cur, errors.New("Choose a password.")
	}
	if password != "" && (len(password) < 10 || strings.ContainsAny(password, ":\n\r")) {
		return cur, errors.New("The password must be at least 10 characters, on one line, without a colon.")
	}
	if !cur.HasPassword && user != "" && user != cur.User {
		if !userName.MatchString(user) {
			return cur, errors.New("Usernames can only use lowercase letters, numbers, - and _, and must start with a letter")
		}
		if _, err := m.host(ctx, "id", "-u", user); err == nil {
			return cur, fmt.Errorf("The username %q is already taken. Choose another one", user)
		}
		// the account and its group take the new name; its home folder moves along
		if out, err := m.host(ctx, "usermod", "-l", user, "-d", "/home/"+user, "-m", cur.User); err != nil {
			return cur, fmt.Errorf("Couldn't set the username: %v: %s", err, strings.TrimSpace(string(out)))
		}
		m.host(ctx, "groupmod", "-n", user, cur.User)
		cur.User = user
		m.saveSSH(cur)
	}
	if password != "" {
		// chpasswd reads "user:password" from a file only root can read: the password never shows in a process list
		f, err := os.CreateTemp(m.StateDir, ".chpasswd-*") // its own file (0600): two changes can't swap passwords
		if err != nil {
			return cur, err
		}
		tmp := f.Name()
		_, err = f.WriteString(cur.User + ":" + password + "\n")
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			_, err = m.host(ctx, "sh", "-c", `chpasswd < "$0"`, tmp)
		}
		os.Remove(tmp)
		if err != nil {
			return cur, fmt.Errorf("Couldn't set the password: %w", err)
		}
	}
	if out, err := m.host(ctx, "passwd", "-u", cur.User); err != nil {
		return cur, fmt.Errorf("Couldn't unlock %s: %v: %s", cur.User, err, strings.TrimSpace(string(out)))
	}
	if out, err := m.host(ctx, "systemctl", "enable", "--now", "ssh"); err != nil {
		m.host(ctx, "passwd", "-l", cur.User)
		return cur, fmt.Errorf("Couldn't start SSH: %v: %s", err, strings.TrimSpace(string(out)))
	}
	cur.On, cur.HasPassword = true, true
	if err := m.saveSSH(cur); err != nil {
		return cur, err
	}
	return cur, nil
}
