// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
)

// The first-owner code: a fresh server ("uninitialised": no owner, no passkey and no admin device) makes one code at start and prints it in its log.
// Whoever holds it registers the first owner, either as the web app's first passkey or by pairing the first client device (which is then the admin,
// as every first paired device is). It is valid until it is used, single use, and gone for good as soon as the server is initialised. Only a salted
// SHA-256 of it is stored. PHECLOUD_FIRST_OWNER_CODE lets the operator choose it; that one is never printed.

type queryer interface {
	QueryRow(q string, args ...any) *sql.Row
	Exec(q string, args ...any) (sql.Result, error)
}

// uninitialised reports whether nobody has been registered yet.
func uninitialised(q queryer) bool {
	var n int
	if q.QueryRow(`SELECT (SELECT COUNT(*) FROM owner)+(SELECT COUNT(*) FROM passkeys)+(SELECT COUNT(*) FROM devices WHERE admin=1)`).Scan(&n) != nil {
		return false // when unsure, behave as initialised: no first-owner code works
	}
	return n == 0
}

// Uninitialised reports whether the server has no owner, no passkey and no admin device yet.
func (s *Store) Uninitialised() bool { return uninitialised(s.db) }

// normCode makes the comparison forgiving about case, dashes and spaces (like the recovery code).
func normCode(c string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '-' || r == ' ':
			return -1
		case r >= 'a' && r <= 'z':
			return r - 32
		}
		return r
	}, strings.TrimSpace(c))
}

func firstHash(salt, code string) string {
	h := sha256.Sum256([]byte(salt + "|" + normCode(code)))
	return hex.EncodeToString(h[:])
}

// ValidFirstOwnerEnvCode checks the strength of an operator-chosen code: at least 12 characters (dashes and spaces aside) from letters, digits, '_'
// and '-', with at least 6 different characters.
func ValidFirstOwnerEnvCode(code string) error {
	c := strings.TrimSpace(code)
	for _, r := range c {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == ' ') {
			return fmt.Errorf("only letters, digits, '-' and '_' are allowed")
		}
	}
	n := normCode(c)
	if len(n) < 12 {
		return fmt.Errorf("it must have at least 12 characters (it has %d)", len(n))
	}
	seen := map[rune]bool{}
	for _, r := range n {
		seen[r] = true
	}
	if len(seen) < 6 {
		return fmt.Errorf("it must use at least 6 different characters")
	}
	return nil
}

// setFirstCode replaces the stored first-owner code by a hash of code.
func (s *Store) setFirstCode(code string, fromEnv bool) error {
	sb := make([]byte, 16)
	if _, err := rand.Read(sb); err != nil {
		return err
	}
	salt := hex.EncodeToString(sb)
	e := 0
	if fromEnv {
		e = 1
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM first_owner_code`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO first_owner_code(id,salt,hash,created,from_env) VALUES(1,?,?,?,?)`, salt, firstHash(salt, code), time.Now().Unix(), e); err != nil {
		return err
	}
	return tx.Commit()
}

// ClearFirstCode drops any stored first-owner code.
func (s *Store) ClearFirstCode() { s.db.Exec(`DELETE FROM first_owner_code`) }

// matchFirstCode compares in constant time; it is false once the server is initialised.
func matchFirstCode(q queryer, code string) bool {
	if normCode(code) == "" || !uninitialised(q) {
		return false
	}
	var salt, hash string
	if q.QueryRow(`SELECT salt,hash FROM first_owner_code WHERE id=1`).Scan(&salt, &hash) != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hash), []byte(firstHash(salt, code))) == 1
}

// CheckFirstCode reports whether code is the live first-owner code (without using it up).
func (s *Store) CheckFirstCode(code string) bool { return matchFirstCode(s.db, code) }

// ConsumeFirstCode uses the first-owner code up. The delete decides the race: of two parallel callers with the right code only one sees a row go.
func (s *Store) ConsumeFirstCode(code string) error { return consumeFirst(s.db, code) }

func consumeFirst(q queryer, code string) error {
	if !matchFirstCode(q, code) {
		return ErrBadCode
	}
	res, err := q.Exec(`DELETE FROM first_owner_code WHERE id=1`)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrBadCode
	}
	return nil
}

// NewFirstCode makes and stores a fresh generated first-owner code (XXXX-XXXX-XXXX, 60 bits), invalidating the previous one.
func (s *Store) NewFirstCode() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	c := make([]byte, 12)
	for i := range c {
		c[i] = alphabet[int(b[i])%len(alphabet)]
	}
	code := string(c[:4]) + "-" + string(c[4:8]) + "-" + string(c[8:])
	return code, s.setFirstCode(code, false)
}

// FirstStart is the start-up step. Uninitialised: makes (or, from envCode, adopts) the first-owner code and says so; the generated one is printed to
// out in a box, a fresh one at every start. Initialised: removes any leftover code, and if envCode is set says that it is ignored (never its value).
// webURL is where the web app can be reached.
func FirstStart(st *Store, envCode, webURL string, out io.Writer, log *slog.Logger) error {
	if !st.Uninitialised() {
		st.ClearFirstCode()
		if envCode != "" {
			log.Warn("PHECLOUD_FIRST_OWNER_CODE is ignored: this server already has an owner; remove the setting (more admins: `phecloud paircode --admin`)")
		}
		return nil
	}
	if envCode != "" {
		if err := ValidFirstOwnerEnvCode(envCode); err != nil {
			log.Error("PHECLOUD_FIRST_OWNER_CODE is too weak and is NOT used; a generated code follows", "why", err.Error())
		} else {
			if err := st.setFirstCode(envCode, true); err != nil {
				return err
			}
			log.Info("First start: PHECLOUD_FIRST_OWNER_CODE is active; open " + webURL + " and enter it (valid until used, once)")
			return nil
		}
	}
	code, err := st.NewFirstCode()
	if err != nil {
		return err
	}
	line := func(s string) string { return "| " + s + strings.Repeat(" ", max(0, 74-len(s))) + " |\n" }
	bar := "+" + strings.Repeat("-", 76) + "+\n"
	fmt.Fprint(out, "\n"+bar+
		line("FIRST START: this server has no owner yet.")+
		line("Open "+webURL)+
		line("and enter this code to register the first owner:")+
		line("")+
		line("        "+code)+
		line("")+
		line("Valid until it is used (once). A restart makes a new code, until the")+
		line("first owner exists. A phone may also pair with it and becomes the admin.")+
		bar+"\n")
	return nil
}
