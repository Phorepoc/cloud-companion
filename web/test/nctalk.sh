#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
# Nextcloud Talk account through the real UI against a throwaway Nextcloud (Docker, repo root). Screenshots: web/test/out/nctalk-*.png
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
DC="docker compose -p phenct -f docker-compose.yml -f docker-compose.build.yml -f web/test/compose.passkey.yml -f web/test/compose.nctalktest.yml"
$DC down -v >/dev/null 2>&1 || true
sh web/test/pull.sh $DC
$DC up -d --build phecloud nextcloud > "$OUT/nctalk-up.log" 2>&1 || { tail -20 "$OUT/nctalk-up.log"; exit 1; }
# on a failure anywhere (set -e stops quietly), show what the servers said before they go
trap 'rc=$?; [ $rc -eq 0 ] || { echo "failed (exit $rc): the last log lines" >&2; docker logs --tail 30 phe-nc-ui >&2 2>&1; docker logs --tail 15 phe-cloud-pk >&2 2>&1; }; $DC down -v >/dev/null 2>&1' EXIT
sh testdata/nctalk/setup.sh phe-nc-ui
A=$(docker exec phe-cloud-pk phecloud paircode --admin 2>/dev/null)
docker run --rm --name phe-playwright-nct --cpus 2 --memory 2g --network container:phe-cloud-pk --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$A" -e OUT=/out -e SERVER=http://nextcloud \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning nctalk.ts' \
    || { docker logs phe-cloud-pk 2>&1 | tail -10 | cut -c1-240; exit 1; }
