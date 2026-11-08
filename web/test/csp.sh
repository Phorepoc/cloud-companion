#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# The web app under its Content-Security-Policy and security headers (Docker, repo root): own compose project (phepk6), fresh volume.
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
DC="docker compose -p phepk6 -f docker-compose.yml -f docker-compose.build.yml -f web/test/compose.passkey.yml"
$DC down -v >/dev/null 2>&1 || true
sh web/test/pull.sh $DC
$DC up -d ${PHE_UP:---build} phecloud >/dev/null 2>&1
trap '$DC down -v >/dev/null 2>&1' EXIT
sleep 6
A=$(docker exec phe-cloud-pk phecloud paircode --admin 2>/dev/null)
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network container:phe-cloud-pk --ipc=host \
  -v "$ROOT/web/test":/t -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$A" -e PW_EXPERIMENTAL_SERVICE_WORKER_NETWORK_EVENTS=1 \
  mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning csp.ts' || { docker logs phe-cloud-pk 2>&1 | tail -8 | cut -c1-240; exit 1; }
