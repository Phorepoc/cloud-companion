#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
# Files of other services through the real UI: a Nextcloud account added with its own sign-in, its folder in Files, against a fake
# Nextcloud (internal/fakes/nextcloud: user ann, pw-ann-12345, with "CC Photos/sea.png"; Docker, repo root); the real Nextcloud is in
# testdata/remote/test.sh. Screenshots: web/test/out/remotefiles-*.png
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
DC="docker compose -p phercf -f docker-compose.yml -f docker-compose.build.yml -f web/test/compose.passkey.yml -f web/test/compose.fakes.yml"
$DC down -v >/dev/null 2>&1 || true
sh web/test/pull.sh $DC
FAKES=nextcloud $DC up -d --build phecloud fakes > "$OUT/remotefiles-up.log" 2>&1 || { tail -20 "$OUT/remotefiles-up.log"; exit 1; }
# on a failure anywhere (set -e stops quietly), show what the servers said before they go
trap 'rc=$?; [ $rc -eq 0 ] || { echo "failed (exit $rc): the last log lines" >&2; docker logs --tail 30 phe-fakes >&2 2>&1; docker logs --tail 15 phe-cloud-pk >&2 2>&1; }; $DC down -v >/dev/null 2>&1' EXIT
A=$(docker exec phe-cloud-pk phecloud paircode --admin 2>/dev/null)
docker run --rm --name phe-playwright-rf --cpus 2 --memory 2g --network container:phe-cloud-pk --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$A" -e OUT=/out -e SERVER=http://nextcloud \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning remotefiles.ts' \
    || { docker logs phe-cloud-pk 2>&1 | tail -10 | cut -c1-240; exit 1; }
