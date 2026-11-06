#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
# Photos through the real UI against a fake Immich (internal/fakes/immich, seeded) and a photo folder (Docker, repo root).
# The real Immich is in testdata/immich/test.sh. Screenshots: web/test/out/photos-*.png
set -eu
export FAKES=immich
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
DC="docker compose -p phephoto -f docker-compose.yml -f docker-compose.build.yml -f web/test/compose.passkey.yml -f web/test/compose.fakes.yml"
$DC down -v >/dev/null 2>&1 || true
sh web/test/pull.sh $DC
$DC up -d --build phecloud fakes > "$OUT/photos-up.log" 2>&1 || { tail -20 "$OUT/photos-up.log"; exit 1; }
trap '$DC down -v >/dev/null 2>&1' EXIT
A=$(docker exec phe-cloud-pk phecloud paircode --admin 2>/dev/null)
docker run --rm --name phe-playwright-ph --cpus 2 --memory 2g --network container:phe-cloud-pk --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$A" -e KEY=fake-immich-api-key-ann -e OUT=/out -e SERVER=http://immich:2283 \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning photos.ts' \
    || { docker logs phe-cloud-pk 2>&1 | tail -10 | cut -c1-240; docker logs phe-fakes 2>&1 | tail -10 | cut -c1-240; exit 1; }
