// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package store

// One store, three databases. PHECLOUD_DB is a SQLite file path (the default), a postgres:// URL (PostgreSQL) or a mysql:// URL (MariaDB / MySQL).
// The SQL in this package is written once, in the dialect SQLite and PostgreSQL share (`?` placeholders, INSERT OR IGNORE,
// ON CONFLICT ... DO UPDATE ... excluded.x, GROUP_CONCAT); DB and Tx rewrite a statement for the dialect in use just before it runs:
//   PostgreSQL: ?  ->  $n, INSERT OR IGNORE -> ... ON CONFLICT DO NOTHING, GROUP_CONCAT(DISTINCT x) -> string_agg(DISTINCT x, ',')
//   MySQL:      INSERT OR IGNORE -> INSERT IGNORE, ON CONFLICT (...) DO UPDATE SET a=excluded.a -> ON DUPLICATE KEY UPDATE a=VALUES(a), `key` quoted
// Table definitions differ more (types, key lengths, auto-increment) and live per dialect in migrate.go.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type Dialect int

const (
	SQLite Dialect = iota
	Postgres
	MySQL
)

func (d Dialect) String() string { return [...]string{"sqlite", "postgres", "mysql"}[d] }

// ErrExternalDB is returned by operations that only make sense for a file database (Snapshot).
var ErrExternalDB = errors.New("the database is an external server: back it up with its own tools (pg_dump, mariadb-dump)")

// DB and Tx are the thin wrappers that rewrite statements.
type DB struct {
	*sql.DB
	d Dialect
}
type Tx struct {
	*sql.Tx
	d Dialect
}

func (d *DB) Exec(q string, args ...any) (sql.Result, error) {
	return d.DB.Exec(rewrite(d.d, q), args...)
}
func (d *DB) Query(q string, args ...any) (*sql.Rows, error) {
	return d.DB.Query(rewrite(d.d, q), args...)
}
func (d *DB) QueryRow(q string, args ...any) *sql.Row { return d.DB.QueryRow(rewrite(d.d, q), args...) }
func (d *DB) Begin() (*Tx, error) {
	t, err := d.DB.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	return &Tx{t, d.d}, nil
}
func (t *Tx) Exec(q string, args ...any) (sql.Result, error) {
	return t.Tx.Exec(rewrite(t.d, q), args...)
}
func (t *Tx) Query(q string, args ...any) (*sql.Rows, error) {
	return t.Tx.Query(rewrite(t.d, q), args...)
}
func (t *Tx) QueryRow(q string, args ...any) *sql.Row { return t.Tx.QueryRow(rewrite(t.d, q), args...) }

var (
	reOrIgnore   = regexp.MustCompile(`(?i)INSERT OR IGNORE INTO`)
	reConflictUp = regexp.MustCompile(`(?is)ON CONFLICT\s*\([^)]*\)\s*DO UPDATE SET\s*(.*)$`)
	reExcluded   = regexp.MustCompile(`(?i)excluded\.(\w+)`)
	reGroupCat   = regexp.MustCompile(`(?i)GROUP_CONCAT\(DISTINCT (\w+)\)`)
	reKeyWord    = regexp.MustCompile(`(?i)\bkey\b`)
)

func rewrite(d Dialect, q string) string {
	switch d {
	case Postgres:
		if reOrIgnore.MatchString(q) {
			q = reOrIgnore.ReplaceAllString(q, "INSERT INTO") + " ON CONFLICT DO NOTHING"
		}
		q = reGroupCat.ReplaceAllString(q, "string_agg(DISTINCT $1, ',')")
		var b strings.Builder
		n := 0
		for _, r := range q {
			if r == '?' {
				n++
				b.WriteString("$" + strconv.Itoa(n))
			} else {
				b.WriteRune(r)
			}
		}
		return b.String()
	case MySQL:
		q = reOrIgnore.ReplaceAllString(q, "INSERT IGNORE INTO")
		q = reConflictUp.ReplaceAllStringFunc(q, func(m string) string {
			body := reConflictUp.FindStringSubmatch(m)[1]
			return "ON DUPLICATE_KEY_UPDATE " + reExcluded.ReplaceAllString(body, "VALUES($1)")
		})
		q = reKeyWord.ReplaceAllString(q, "`key`")
		return strings.Replace(q, "ON DUPLICATE_KEY_UPDATE", "ON DUPLICATE KEY UPDATE", 1)
	}
	return q
}

// parseDSN decides the dialect and the driver DSN.
func parseDSN(dsn string) (Dialect, string, string, error) {
	switch {
	case strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://"):
		return Postgres, "pgx", dsn, nil
	case strings.HasPrefix(dsn, "mysql://") || strings.HasPrefix(dsn, "mariadb://"):
		u, err := url.Parse(dsn)
		if err != nil {
			return 0, "", "", err
		}
		pw, _ := u.User.Password()
		host := u.Host
		if !strings.Contains(host, ":") {
			host += ":3306"
		}
		q := u.Query()
		if q.Get("charset") == "" {
			q.Set("charset", "utf8mb4")
		}
		q.Set("sql_mode", "'STRICT_ALL_TABLES,NO_BACKSLASH_ESCAPES'")
		return MySQL, "mysql", fmt.Sprintf("%s:%s@tcp(%s)%s?%s", u.User.Username(), pw, host, u.Path, q.Encode()), nil
	}
	return SQLite, "sqlite", "file:" + dsn + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", nil
}
