#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# The Nextcloud Talk connection against a real Nextcloud (repo root): the official image with SQLite and Talk on its own network,
# and the Go test tagged "nctalk" next to it.
set -eu
NET=phe-nc-test
docker rm -f phe-nc-t phe-nctalk-go >/dev/null 2>&1 || true
docker network rm $NET >/dev/null 2>&1 || true
docker network create $NET >/dev/null
trap 'docker rm -f phe-nc-t phe-nctalk-go >/dev/null 2>&1; docker network rm '$NET' >/dev/null 2>&1' EXIT
docker run -d --name phe-nc-t --network $NET --cpus 2 --memory 1g -e SQLITE_DATABASE=nc -e NEXTCLOUD_ADMIN_USER=admin \
	-e NEXTCLOUD_ADMIN_PASSWORD=admin-pw-12345 -e NEXTCLOUD_TRUSTED_DOMAINS=phe-nc-t nextcloud:35.0.2-apache >/dev/null
sh testdata/nctalk/setup.sh phe-nc-t
mkdir -p internal/webui/dist && touch internal/webui/dist/placeholder.txt
docker run --rm --name phe-nctalk-go --network $NET -v "$(pwd)":/src -v "$HOME/.cache/go-docker":/go/pkg -w /src \
	-e GOFLAGS="-mod=mod -buildvcs=false -tags=goolm,nctalk" -e CGO_ENABLED=0 -e NEXTCLOUD=http://phe-nc-t \
	golang:1.26 go test -count=1 -timeout 180s -run TestNextcloudTalk -v ./internal/nctalk/ \
	|| { docker logs phe-nc-t 2>&1 | tail -20; exit 1; }
