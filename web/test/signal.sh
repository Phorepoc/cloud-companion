#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: AGPL-3.0-only
# Signal account through the real UI against the fake signal-cli-rest-api (internal/fakes/signal, nothing linked; Docker, repo root).
# The real API is in testdata/signal/test.sh. Screenshots: web/test/out/signal-*.png
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
DC="docker compose -p phesig -f docker-compose.yml -f docker-compose.build.yml -f web/test/compose.passkey.yml -f web/test/compose.fakes.yml"
$DC down -v >/dev/null 2>&1 || true
sh web/test/pull.sh $DC
FAKES=signal $DC up -d ${PHE_UP:---build} phecloud fakes > "$OUT/signal-up.log" 2>&1 || { tail -20 "$OUT/signal-up.log"; exit 1; }
trap '$DC down -v >/dev/null 2>&1' EXIT
A=$(docker exec phe-cloud-pk phecloud paircode --admin 2>/dev/null)
docker run --rm --name phe-playwright-sig --cpus 2 --memory 2g --network container:phe-cloud-pk --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$A" -e OUT=/out -e SERVER=http://signal-cli:8080 \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning signal.ts' \
    || { docker logs phe-cloud-pk 2>&1 | tail -10 | cut -c1-240; docker logs phe-fakes 2>&1 | tail -10 | cut -c1-240; exit 1; }
