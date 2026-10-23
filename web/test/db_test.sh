#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# The same Go suite against SQLite, PostgreSQL and MariaDB (Docker, repo root). Throwaway containers phe-pg-t and phe-maria-t on their own
# network, capped; every test makes and drops its own database (store.OpenTest). Usage: sh web/test/db_test.sh [sqlite|postgres|mariadb|all]
set -eu
ROOT=$(pwd); NET=phe-dbtest
cleanup() { docker rm -f phe-pg-t phe-maria-t >/dev/null 2>&1 || true; docker network rm $NET >/dev/null 2>&1 || true; }
trap cleanup EXIT
cleanup
docker network create $NET >/dev/null
go_test() { docker run --rm --cpus 2 --memory 2g --network $NET -e GOFLAGS=-buildvcs=false "$@" -v "$ROOT":/src -w /src golang:1.26-bookworm go test -count=1 ./internal/... 2>&1 | grep -E "^(ok|FAIL|---|panic)|Error" | head -30; }
case "${1:-all}" in all|sqlite) echo "== SQLite"; go_test ;; esac
case "${1:-all}" in all|postgres)
  docker run -d --name phe-pg-t --network $NET --cpus 1 --memory 512m -e POSTGRES_PASSWORD=pw postgres:16-alpine >/dev/null
  for i in $(seq 1 40); do docker exec phe-pg-t pg_isready -U postgres >/dev/null 2>&1 && break; sleep 1; done; sleep 2
  echo "== PostgreSQL 16"; go_test -e PHECLOUD_TEST_DB='postgres://postgres:pw@phe-pg-t:5432/postgres?sslmode=disable' ;;
esac
case "${1:-all}" in all|mariadb)
  docker run -d --name phe-maria-t --network $NET --cpus 1 --memory 768m -e MARIADB_ROOT_PASSWORD=pw mariadb:11 >/dev/null
  for i in $(seq 1 60); do docker exec phe-maria-t mariadb -uroot -ppw -e 'select 1' >/dev/null 2>&1 && break; sleep 1; done; sleep 2
  echo "== MariaDB 11"; go_test -e PHECLOUD_TEST_DB='mysql://root:pw@phe-maria-t:3306/mysql' ;;
esac
