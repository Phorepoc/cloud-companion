#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# "Sign in with Google" against a fake provider (Docker, repo root): own compose project (phepk5), fresh volume. Screenshots: web/test/out/oauth-*.png
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
DC="docker compose -p phepk5 -f docker-compose.yml -f web/test/compose.passkey.yml -f web/test/compose.oauth.yml"
$DC down -v >/dev/null 2>&1 || true
$DC up -d --build phecloud >/dev/null 2>&1
trap '$DC down -v >/dev/null 2>&1' EXIT
sleep 6
A=$(docker exec phe-cloud-pk phecloud paircode --admin 2>/dev/null)
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network container:phe-cloud-pk --ipc=host \
  -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$A" -e OUT=/out \
  mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning oauth.ts' || { docker logs phe-cloud-pk 2>&1 | tail -6 | cut -c1-240; exit 1; }
