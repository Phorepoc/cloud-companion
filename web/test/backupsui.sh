#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# How Settings, Backups looks (backupsui.ts): Cloud Companion (phe-cloud:dev) with a real host manager beside it and no board;
# the manager's backup.json is written here to show the page's other states. (Docker; repo root)
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"; W=$(mktemp -d)
CODE=Q7XK-M3PD-T9RW-B2HF
trap 'docker rm -f -v phe-ui-cloud phe-ui-host >/dev/null 2>&1; docker volume rm phe-ui-sock >/dev/null 2>&1; docker run --rm -v "$W":/w alpine:3.22 rm -rf /w/state; rm -rf "$W"' EXIT
[ -n "${PHE_UP:-}" ] || docker build -q -t phe-cloud:dev . >/dev/null
docker run --rm -v "$ROOT":/src -v "$W":/out -w /src -e CGO_ENABLED=0 -e GOFLAGS="-tags=goolm -buildvcs=false" \
  golang:1.26-bookworm@sha256:23ce71337b726608afb1bc00986e630a05034a87a87ea1fc2cd1ae5cf33387eb go build -o /out/phehost ./cmd/phehost
mkdir -p "$W/state"; docker volume create phe-ui-sock >/dev/null
docker run -d --name phe-ui-host -v phe-ui-sock:/run/phehost -v "$W":/w -v "$ROOT/deploy/features":/features:ro debian:trixie-slim \
  /w/phehost -compose /w -features /features -state /w/state -socket /run/phehost/api.sock -listen 127.0.0.1:1 >/dev/null
docker run -d --name phe-ui-cloud -v phe-ui-sock:/run/phehost -e PHECLOUD_FIRST_OWNER_CODE=$CODE phe-cloud:dev >/dev/null
sleep 5
docker run --rm --network container:phe-ui-cloud --ipc=host -v "$ROOT/web/test":/t -v "$OUT":/out -v "$W/state":/state -w /t -e HOME=/tmp \
  -e BASE=http://127.0.0.1:8080 -e OUT=/out -e CODE=$CODE mcr.microsoft.com/playwright:v1.49.1-noble \
  sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning backupsui.ts'
