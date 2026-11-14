#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# Immich against a real server (repo root): the official server image with its Postgres (VectorChord) and Valkey, machine learning
# off, as Immich's own compose file runs it; and the Go test tagged "immich" next to it, which signs up the owner, makes an API key,
# uploads photos and makes an album through Immich's API, then reads them through ours.
set -eu
NET=phe-immich-test
ALL="phe-im-t phe-imdb-t phe-imredis-t phe-immich-go"
docker rm -f $ALL >/dev/null 2>&1 || true
docker network rm $NET >/dev/null 2>&1 || true
docker network create $NET >/dev/null
trap 'docker rm -f '"$ALL"' >/dev/null 2>&1; docker network rm '$NET' >/dev/null 2>&1' EXIT
docker run -d --name phe-imdb-t --network $NET --network-alias database --memory 1g -e POSTGRES_PASSWORD=pg-pw-12345 -e POSTGRES_USER=postgres \
	-e POSTGRES_DB=immich -e POSTGRES_INITDB_ARGS=--data-checksums ghcr.io/immich-app/postgres:14-vectorchord0.4.3-pgvectors0.2.0 >/dev/null
docker run -d --name phe-imredis-t --network $NET --network-alias redis --memory 128m valkey/valkey:9 >/dev/null
docker run -d --name phe-im-t --network $NET --network-alias immich.test --cpus 2 --memory 2g -e DB_HOSTNAME=database -e DB_PASSWORD=pg-pw-12345 \
	-e DB_USERNAME=postgres -e DB_DATABASE_NAME=immich -e REDIS_HOSTNAME=redis -e IMMICH_MACHINE_LEARNING_ENABLED=false \
	ghcr.io/immich-app/immich-server:v3.3.6 >/dev/null
for i in $(seq 1 90); do
	docker run --rm --network $NET curlimages/curl:8.11.1 -sf http://immich.test:2283/api/server/ping 2>/dev/null | grep -q pong && break; sleep 4
done
echo "immich ready after $((i * 4))s"
mkdir -p internal/webui/dist && touch internal/webui/dist/placeholder.txt
docker run --rm --name phe-immich-go --network $NET -v "$(pwd)":/src -v "$HOME/.cache/go-docker":/go/pkg -w /src \
	-e GOFLAGS="-mod=mod -buildvcs=false -tags=goolm,immich" -e CGO_ENABLED=0 -e IMMICH=http://immich.test:2283 \
	golang:1.26 go test -count=1 -timeout 300s -run TestRealImmich -v ./internal/remote/ \
	|| { docker logs phe-im-t 2>&1 | tail -15; exit 1; }
