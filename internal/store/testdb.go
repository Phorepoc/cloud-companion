// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// OpenTest opens a fresh store for a test. Without PHECLOUD_TEST_DB it is a SQLite file in the test's temp dir. With PHECLOUD_TEST_DB set to a
// postgres:// or mysql:// URL of a server where the user may CREATE DATABASE, every test gets its own new database (dropped afterwards), so the
// whole Go suite runs unchanged against PostgreSQL and MariaDB (web/test/db_test.sh).
func OpenTest(t testing.TB) *Store {
	t.Helper()
	base := os.Getenv("PHECLOUD_TEST_DB")
	if base == "" {
		st, err := Open(filepath.Join(t.TempDir(), "t.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
		return st
	}
	d, driver, ddsn, err := parseDSN(base)
	if err != nil || d == SQLite {
		t.Fatalf("PHECLOUD_TEST_DB must be a postgres:// or mysql:// URL: %v", err)
	}
	r := make([]byte, 5)
	rand.Read(r)
	name := "phe_t_" + hex.EncodeToString(r)
	admin, err := sql.Open(driver, ddsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(base)
	u.Path = "/" + name
	st, err := Open(u.String())
	if err != nil {
		admin.Exec("DROP DATABASE " + name)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		st.Close()
		if d == Postgres {
			admin.Exec("DROP DATABASE " + name + " WITH (FORCE)")
		} else {
			admin.Exec("DROP DATABASE " + name)
		}
		admin.Close()
	})
	_ = strings.TrimSpace
	return st
}
