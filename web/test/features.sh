#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Settings → Features on a board: the host manager (phehost) in its own Docker daemon starts Cloud Companion with its socket, and
# the owner switches Signal on and off in the web app; the containers are checked in between (Docker, privileged; repo root)
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"; W=$(mktemp -d)
trap 'docker rm -f -v phe-board >/dev/null 2>&1; docker run --rm -v "$W":/w alpine:3.22 sh -c "rm -rf /w/* /w/.[!.]*" >/dev/null 2>&1; rm -rf "$W"' EXIT
[ -n "${PHE_UP:-}" ] || docker build -q -t phe-cloud:dev . >/dev/null
docker run --rm -v "$ROOT":/src -v "$W":/out -w /src -e CGO_ENABLED=0 -e GOFLAGS="-tags=goolm -buildvcs=false" \
  golang:1.26-bookworm@sha256:23ce71337b726608afb1bc00986e630a05034a87a87ea1fc2cd1ae5cf33387eb go build -o /out/phehost ./cmd/phehost
mkdir -p "$W/cloud/deploy" "$W/state"
# a board that has been set up (the setup page's job, setup.sh): only Cloud Companion on
echo '{"features":{"core":{}}}' > "$W/state/state.json"
cp docker-compose*.yml "$W/cloud/" && cp -r deploy/features deploy/Caddyfile* deploy/hub deploy/asterisk "$W/cloud/deploy/"
docker save phe-cloud:dev -o "$W/phecloud.tar"
docker run -d --name phe-board --privileged -e DOCKER_TLS_CERTDIR= -v "$W":/t docker:27-dind >/dev/null
B() { docker exec phe-board "$@"; }
for i in $(seq 1 60); do B docker info >/dev/null 2>&1 && break; sleep 1; done
B docker load -q -i /t/phecloud.tar >/dev/null
B docker tag phe-cloud:dev ghcr.io/phorepoc/phecloud:v0.2.0
B sh -c 'mkdir -p /run/phehost && (/t/phehost -compose /t/cloud -state /t/state -socket /run/phehost/api.sock >/t/phehost.log 2>&1 &)'
for i in $(seq 1 120); do B docker exec phe-cloud true >/dev/null 2>&1 && break; sleep 1; done
sleep 4
A=$(B docker exec phe-cloud phecloud paircode --admin 2>/dev/null)
rm -f "$OUT/features-on-done" "$OUT/features-checked"
fail() { echo "$1"; B cat /t/phehost.log | tail -20; exit 1; }
docker run --rm --network container:phe-board --ipc=host -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp \
  -e BASE=http://127.0.0.1:8080 -e CODE="$A" -e OUT=/out mcr.microsoft.com/playwright:v1.49.1-noble \
  sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning features.ts' &
P=$!
for i in $(seq 1 900); do [ -e "$OUT/features-on-done" ] && break; kill -0 $P 2>/dev/null || break; sleep 1; done
[ -e "$OUT/features-on-done" ] || { wait $P || true; fail "switching on failed"; }
B docker ps --format '{{.Names}}' | grep -qx phe-signal-cli || fail "Signal's container is not running"
touch "$OUT/features-checked"
wait $P || fail "switching off failed"
! B docker ps -a --format '{{.Names}}' | grep -qx phe-signal-cli || fail "Signal's container is still there"
B cat /t/state/state.json | grep -q signal && fail "Signal is still in the saved state"
echo "features ok"
