#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
#
# Style screenshots (Docker, from the repo root): sh web/test/shots.sh before|after [routes]
# Starts the server image as built (use `docker compose build` first for the "after" set), pairs four fresh browsers (phone and
# desktop, light and dark) and writes web/test/screenshots/<label>/*.png.
set -eu
ROOT=$(pwd)
LABEL=${1:-after}
ROUTES=${2:-pair,browse,devices,settings}
OUT=$ROOT/web/test/screenshots/$LABEL
mkdir -p "$OUT"
docker compose up -d >/dev/null 2>&1
trap 'docker compose down >/dev/null 2>&1' EXIT
sleep 6
NET=$(docker inspect phe-cloud --format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}}{{end}}')
CODES=""
for i in 1 2 3 4; do CODES="$CODES$(docker exec phe-cloud phecloud paircode --admin 2>/dev/null),"; done
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network "$NET" --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://phecloud:8080 -e CODES="${CODES%,}" -e OUT=/out -e ROUTES="$ROUTES" \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node shots.mjs'
