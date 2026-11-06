#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
# XMPP account through the real UI against a throwaway Prosody (Docker, repo root). Screenshots: web/test/out/xmpp-*.png
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
DC="docker compose -p phexm -f docker-compose.yml -f docker-compose.build.yml -f web/test/compose.passkey.yml -f web/test/compose.xmpptest.yml"
$DC down -v >/dev/null 2>&1 || true
sh web/test/pull.sh $DC
$DC up -d --build phecloud prosody > "$OUT/xmpp-up.log" 2>&1 || { tail -20 "$OUT/xmpp-up.log"; exit 1; }
trap '$DC down -v >/dev/null 2>&1' EXIT
for i in $(seq 1 30); do docker exec phe-prosody-ui prosodyctl status >/dev/null 2>&1 && break; sleep 1; done
docker exec phe-prosody-ui prosodyctl register cc localhost pw-cc >/dev/null
sleep 2
A=$(docker exec phe-cloud-pk phecloud paircode --admin 2>/dev/null)
docker run --rm --name phe-playwright-xm --cpus 2 --memory 2g --network container:phe-cloud-pk --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$A" -e OUT=/out -e SERVER=prosody \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning xmpp.ts' \
    || { docker logs phe-cloud-pk 2>&1 | tail -10 | cut -c1-240; exit 1; }
