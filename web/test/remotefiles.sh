#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
# Files of other services through the real UI: a Nextcloud account added with its own sign-in, its folder in Files (Docker, repo root). Screenshots: web/test/out/remotefiles-*.png
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
DC="docker compose -p phercf -f docker-compose.yml -f docker-compose.build.yml -f web/test/compose.passkey.yml -f web/test/compose.nctalktest.yml"
$DC down -v >/dev/null 2>&1 || true
$DC up -d --build phecloud nextcloud > "$OUT/remotefiles-up.log" 2>&1 || { tail -20 "$OUT/remotefiles-up.log"; exit 1; }
# on a failure anywhere (set -e stops quietly), show what the servers said before they go
trap 'rc=$?; [ $rc -eq 0 ] || { echo "failed (exit $rc): the last log lines" >&2; docker logs --tail 30 phe-nc-ui >&2 2>&1; docker logs --tail 15 phe-cloud-pk >&2 2>&1; }; $DC down -v >/dev/null 2>&1' EXIT
sh testdata/nctalk/wait.sh phe-nc-ui
docker exec -u www-data -e OC_PASS=pw-ann-12345 phe-nc-ui php occ user:add --password-from-env ann >/dev/null
# a folder with a photo in it (a 2x2 PNG), put there over WebDAV as any client would
docker exec phe-nc-ui curl -sf -u ann:pw-ann-12345 -X MKCOL "http://localhost/remote.php/dav/files/ann/CC%20Photos" >/dev/null
printf "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAFklEQVR4nGP8z8DAwMDAxMDAwMDAAAANHQEDasKb6QAAAABJRU5ErkJggg==" | base64 -d | docker exec -i phe-nc-ui curl -sf -u ann:pw-ann-12345 -T - "http://localhost/remote.php/dav/files/ann/CC%20Photos/sea.png" >/dev/null
A=$(docker exec phe-cloud-pk phecloud paircode --admin 2>/dev/null)
docker run --rm --name phe-playwright-rf --cpus 2 --memory 2g --network container:phe-cloud-pk --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$A" -e OUT=/out -e SERVER=http://nextcloud \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning remotefiles.ts' \
    || { docker logs phe-cloud-pk 2>&1 | tail -10 | cut -c1-240; exit 1; }
