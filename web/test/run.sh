#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
#
# Web app smoke test (Docker, from the repo root): builds the server image (which builds the Quasar app), starts it, and runs the
# Playwright test in a container on the same network. Screenshots land in web/test/out/.
set -eu
ROOT=$(pwd)
OUT=$ROOT/web/test/out
mkdir -p "$OUT"
docker compose up -d --build >/dev/null 2>&1
trap 'docker compose down >/dev/null 2>&1' EXIT
sleep 6
CODE=$(docker exec phe-cloud phecloud paircode --admin 2>/dev/null)
NET=$(docker inspect phe-cloud --format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}}{{end}}')
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network "$NET" --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://phecloud:8080 -e CODE="$CODE" -e OUT=/out \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node smoke.mjs'
