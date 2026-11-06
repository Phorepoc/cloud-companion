#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Files of other services against real servers (repo root), on their own network: Nextcloud (the official image with SQLite;
# user ann, who signs in as ann@example.org) and Seafile 14 (the official image with MariaDB and Redis, as Seafile's own
# compose file runs it, as seafile.test: it wants a host name with a dot), and the Go test tagged "remote" next to them.
set -eu
NET=phe-remote-test
ALL="phe-rnc-t phe-sf-t phe-sfdb-t phe-sfredis-t phe-remote-go"
docker rm -f $ALL >/dev/null 2>&1 || true
docker network rm $NET >/dev/null 2>&1 || true
docker network create $NET >/dev/null
trap 'docker rm -f '"$ALL"' >/dev/null 2>&1; docker network rm '$NET' >/dev/null 2>&1' EXIT
docker run -d --name phe-rnc-t --network $NET --cpus 2 --memory 1g -e SQLITE_DATABASE=nc -e NEXTCLOUD_ADMIN_USER=admin \
	-e NEXTCLOUD_ADMIN_PASSWORD=admin-pw-12345 -e NEXTCLOUD_TRUSTED_DOMAINS=phe-rnc-t nextcloud:35.0.2-apache >/dev/null
docker run -d --name phe-sfdb-t --network $NET --network-alias db --memory 512m -e MYSQL_ROOT_PASSWORD=root-pw-12345 mariadb:10.11 >/dev/null
docker run -d --name phe-sfredis-t --network $NET --network-alias redis --memory 128m redis:7.4-alpine >/dev/null
docker run -d --name phe-sf-t --network $NET --network-alias seafile.test --cpus 2 --memory 2g -e SEAFILE_MYSQL_DB_HOST=db -e SEAFILE_MYSQL_DB_PASSWORD=sf-db-12345 -e SEAFILE_MYSQL_DB_USER=seafile \
	-e SEAFILE_MYSQL_DB_CCNET_DB_NAME=ccnet_db -e SEAFILE_MYSQL_DB_SEAFILE_DB_NAME=seafile_db -e SEAFILE_MYSQL_DB_SEAHUB_DB_NAME=seahub_db \
	-e INIT_SEAFILE_MYSQL_ROOT_PASSWORD=root-pw-12345 -e INIT_SEAFILE_ADMIN_EMAIL=me@example.org -e INIT_SEAFILE_ADMIN_PASSWORD=sf-pw-12345 \
	-e SEAFILE_SERVER_HOSTNAME=seafile.test -e SEAFILE_SERVER_PROTOCOL=http -e JWT_PRIVATE_KEY=test-jwt-key-0123456789012345678901234567890 \
	-e ENABLE_SEADOC=false -e CACHE_PROVIDER=redis -e REDIS_HOST=redis seafileltd/seafile-mc:14.0.9 >/dev/null

sh testdata/nctalk/wait.sh phe-rnc-t
docker exec -u www-data -e OC_PASS=pw-ann-12345 phe-rnc-t php occ user:add --password-from-env ann >/dev/null
docker exec -u www-data phe-rnc-t php occ user:setting ann settings email ann@example.org
echo "nextcloud ready"
for i in $(seq 1 120); do
	docker run --rm --network $NET curlimages/curl:8.11.1 -sf http://seafile.test/api2/ping/ 2>/dev/null | grep -q pong && break; sleep 3
done
echo "seafile ready after $((i * 3))s"

mkdir -p internal/webui/dist && touch internal/webui/dist/placeholder.txt
docker run --rm --name phe-remote-go --network $NET -v "$(pwd)":/src -v "$HOME/.cache/go-docker":/go/pkg -w /src \
	-e GOFLAGS="-mod=mod -buildvcs=false -tags=goolm,remote" -e CGO_ENABLED=0 -e NEXTCLOUD=http://phe-rnc-t -e SEAFILE=http://seafile.test \
	golang:1.26 go test -count=1 -timeout 300s -run TestReal -v ./internal/remote/ \
	|| { docker logs phe-sf-t 2>&1 | tail -15; exit 1; }
