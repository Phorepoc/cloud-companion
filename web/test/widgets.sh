#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# Desktop widgets (Firefox): adding one, using it, and what it keeps after a reload (Docker, repo root): own compose project (phewg). Screenshots: web/test/out/widgets-*.png
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
DC="docker compose -p phewg -f docker-compose.yml -f docker-compose.build.yml -f web/test/compose.passkey.yml"
$DC down -v >/dev/null 2>&1 || true
sh web/test/pull.sh $DC
$DC up -d ${PHE_UP:---build} phecloud >/dev/null 2>&1
trap '$DC down -v >/dev/null 2>&1' EXIT
sleep 6
A=$(docker exec phe-cloud-pk phecloud paircode --admin 2>/dev/null)
B=$(docker exec phe-cloud-pk phecloud paircode 2>/dev/null)
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network container:phe-cloud-pk --ipc=host \
  -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$A" -e CODE2="$B" -e OUT=/out \
  mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning widgets.ts' || { docker logs phe-cloud-pk 2>&1 | tail -8 | cut -c1-240; exit 1; }
