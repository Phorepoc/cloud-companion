#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
# Matrix account through the real UI against the fake homeserver (internal/fakes/matrix; Docker, repo root). The real Conduit is in
# testdata/matrix/test.sh. Screenshots: web/test/out/matrix-*.png
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
DC="docker compose -p phemx -f docker-compose.yml -f docker-compose.build.yml -f web/test/compose.passkey.yml -f web/test/compose.fakes.yml"
export FAKES=matrix
$DC down -v >/dev/null 2>&1 || true
sh web/test/pull.sh $DC
$DC up -d --build phecloud fakes > "$OUT/matrix-up.log" 2>&1 || { tail -20 "$OUT/matrix-up.log"; exit 1; }
trap '$DC down -v >/dev/null 2>&1' EXIT
sleep 2
A=$(docker exec phe-cloud-pk phecloud paircode --admin 2>/dev/null)
docker run --rm --name phe-playwright-mx --cpus 2 --memory 2g --network container:phe-cloud-pk --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$A" -e OUT=/out -e HS=http://matrix:6167 \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning matrix.ts' \
    || { docker logs phe-cloud-pk 2>&1 | tail -10 | cut -c1-240; docker logs phe-fakes 2>&1 | tail -10 | cut -c1-240; exit 1; }
