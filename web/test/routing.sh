#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
# Connections → Messaging test (Docker, repo root): own compose project, no host ports. Screenshots: web/test/out/routing-*.png
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
DC="docker compose -p phert -f docker-compose.yml -f docker-compose.build.yml -f web/test/compose.passkey.yml"
$DC down -v >/dev/null 2>&1 || true
sh web/test/pull.sh $DC
$DC up -d ${PHE_UP:---build} phecloud >/dev/null 2>&1
trap '$DC down -v >/dev/null 2>&1' EXIT
sleep 4
A=$(docker exec phe-cloud-pk phecloud paircode --admin 2>/dev/null)
docker run --rm --name phe-playwright-rt --cpus 2 --memory 2g --network container:phe-cloud-pk --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$A" -e OUT=/out \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning routing.ts' || { docker logs phe-cloud-pk 2>&1 | tail -8 | cut -c1-240; exit 1; }
