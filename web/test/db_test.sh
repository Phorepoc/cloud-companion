#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# The SQL against PostgreSQL and MariaDB, and the whole Go suite on SQLite (Docker, repo root). The SQL is all in internal/store, but most of its
# queries are tested through the packages that use it, so the external databases run every package that opens a store (DBPKGS); both start at once. Throwaway containers phe-pg-t and phe-maria-t on their own
# network, capped; every test makes and drops its own database (store.OpenTest). Usage: sh web/test/db_test.sh [sqlite|postgres|mariadb|external|all]
set -eu
ROOT=$(pwd); NET=phe-dbtest
cleanup() { docker rm -f phe-pg-t phe-maria-t >/dev/null 2>&1 || true; docker network rm $NET >/dev/null 2>&1 || true; }
trap cleanup EXIT
cleanup
docker network create $NET >/dev/null
# a failing test fails the script (the output is filtered, its status is kept)
go_test() {
  pkgs=$1; shift
  docker run --rm --cpus 4 --memory 6g --network $NET -e GOFLAGS="-buildvcs=false -tags=goolm" -e CGO_ENABLED=0 "$@" -v "$ROOT":/src -v phe-gomod:/go/pkg/mod -v phe-gocache:/root/.cache/go-build -w /src golang:1.26-bookworm \
    sh -c "go test -count=1 $pkgs > /tmp/out 2>&1; s=\$?; grep -E '^(ok|FAIL|---|panic)|Error' /tmp/out | head -40; exit \$s"
}
want() { case "$1" in all|"$2") return 0 ;; external) [ "$2" != sqlite ] && return 0 ;; esac; return 1; }
K=${1:-all}
# the packages whose tests open a store (store.OpenTest): grep -rl 'OpenTest(t)' internal --include=*_test.go
DBPKGS='./internal/catalogue/ ./internal/chat/ ./internal/connectors/ ./internal/irc/ ./internal/matrix/ ./internal/nctalk/ ./internal/server/ ./internal/signal/ ./internal/sipgw/ ./internal/sipsvc/ ./internal/store/ ./internal/telegram/ ./internal/webui/ ./internal/xmpp/ ./internal/xmppgw/'
want "$K" postgres && docker run -d --name phe-pg-t --network $NET --cpus 1 --memory 512m -e POSTGRES_PASSWORD=pw postgres:16-alpine >/dev/null
want "$K" mariadb && docker run -d --name phe-maria-t --network $NET --cpus 1 --memory 768m -e MARIADB_ROOT_PASSWORD=pw mariadb:11 >/dev/null
fail=0
if want "$K" sqlite; then echo "== SQLite (everything)"; go_test ./internal/... || fail=1; fi
if want "$K" postgres; then
  for i in $(seq 1 60); do docker exec phe-pg-t pg_isready -U postgres >/dev/null 2>&1 && break; sleep 1; done
  echo "== PostgreSQL 16 (packages with a store)"; go_test "$DBPKGS" -e PHECLOUD_TEST_DB='postgres://postgres:pw@phe-pg-t:5432/postgres?sslmode=disable' || fail=1
fi
if want "$K" mariadb; then
  for i in $(seq 1 60); do docker exec phe-maria-t mariadb -uroot -ppw -e 'select 1' >/dev/null 2>&1 && break; sleep 1; done
  echo "== MariaDB 11 (packages with a store)"; go_test "$DBPKGS" -e PHECLOUD_TEST_DB='mysql://root:pw@phe-maria-t:3306/mysql' || fail=1
fi
exit $fail
