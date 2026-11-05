#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# The Matrix connection against a real homeserver (repo root): Conduit (Apache-2.0) on its own network, open registration, no
# federation, and the Go test tagged "matrix" next to it.
set -eu
NET=phe-matrix-test
docker rm -f phe-conduit-t phe-matrix-go >/dev/null 2>&1 || true
docker network rm $NET >/dev/null 2>&1 || true
docker network create $NET >/dev/null
trap 'docker rm -f phe-conduit-t phe-matrix-go >/dev/null 2>&1; docker network rm '$NET' >/dev/null 2>&1' EXIT
docker run -d --name phe-conduit-t --network $NET --cpus 1 --memory 512m -e CONDUIT_CONFIG="" -e CONDUIT_SERVER_NAME=test.local \
	-e CONDUIT_DATABASE_PATH=/tmp -e CONDUIT_DATABASE_BACKEND=rocksdb -e CONDUIT_PORT=6167 -e CONDUIT_ADDRESS=0.0.0.0 \
	-e CONDUIT_ALLOW_REGISTRATION=true -e CONDUIT_ALLOW_FEDERATION=false matrixconduit/matrix-conduit:v0.10.15 >/dev/null
sleep 3
mkdir -p internal/webui/dist && touch internal/webui/dist/placeholder.txt
docker run --rm --name phe-matrix-go --network $NET -v "$(pwd)":/src -v "$HOME/.cache/go-docker":/go/pkg -w /src \
	-e GOFLAGS="-mod=mod -buildvcs=false -tags=goolm,matrix" -e CGO_ENABLED=0 -e MATRIX=http://phe-conduit-t:6167 -e MATRIX_LOG="${MATRIX_LOG:-}" \
	golang:1.26 go test -count=1 -timeout 180s -run TestConduit -v ./internal/matrix/ \
	|| { docker logs phe-conduit-t 2>&1 | tail -15; exit 1; }
