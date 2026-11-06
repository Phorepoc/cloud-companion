#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
# IRC account through the real UI against the fake IRC server (internal/fakes/irc; the real Ergo is in testdata/irc/test.sh).
# Docker, repo root. Screenshots: web/test/out/irc-*.png
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
DC="docker compose -p pheirc -f docker-compose.yml -f docker-compose.build.yml -f web/test/compose.passkey.yml -f web/test/compose.fakes.yml"
$DC down -v >/dev/null 2>&1 || true
sh web/test/pull.sh $DC
FAKES=irc $DC up -d --build phecloud fakes > "$OUT/irc-up.log" 2>&1 || { tail -20 "$OUT/irc-up.log"; exit 1; }
trap '$DC down -v >/dev/null 2>&1' EXIT
sleep 2
A=$(docker exec phe-cloud-pk phecloud paircode --admin 2>/dev/null)
docker run --rm --name phe-playwright-irc --cpus 2 --memory 2g --network container:phe-cloud-pk --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$A" -e OUT=/out -e SERVER=irc \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning irc.ts' \
    || { docker logs phe-cloud-pk 2>&1 | tail -10 | cut -c1-240; docker logs phe-fakes 2>&1 | tail -10 | cut -c1-240; exit 1; }
