#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# The Browser page with real Firefox (Docker, repo root): own compose project (phebr), fresh volumes. Screenshots: web/test/out/browser-*.png
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
export PHECLOUD_BROWSER_PASSWORD=test-browser-password PHECLOUD_BROWSER_SECRET=test-browser-secret
export PHE_BROWSER_IP=${PHE_BROWSER_IP:-$(hostname -I | cut -d' ' -f1)}
DC="docker compose -p phebr -f docker-compose.yml -f docker-compose.build.yml -f web/test/compose.passkey.yml -f docker-compose.browser.yml -f web/test/compose.browser.yml"
$DC down -v >/dev/null 2>&1 || true
$DC up -d ${PHE_UP:---build} phecloud browser site >/dev/null 2>&1
done_() { docker logs phe-browser 2>&1 | grep -iE 'clip|paste|error' | tail -8; $DC down -v >/dev/null 2>&1; }
trap done_ EXIT
sleep 15
A=$(docker exec phe-cloud-pk phecloud paircode --admin 2>/dev/null); B=$(docker exec phe-cloud-pk phecloud paircode --admin 2>/dev/null)
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network container:phe-cloud-pk --ipc=host \
  -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$A" -e CODE2="$B" -e OUT=/out \
  mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning browser.ts'
# what Firefox keeps is on the volume: a new container still has the tab's site history and profile
P=$(docker exec phe-browser sh -c 'ls -d /home/neko/.config/mozilla/firefox/*.default*' | head -1); docker exec phe-browser touch "$P/phe-survives"
$DC up -d --force-recreate browser >/dev/null 2>&1; sleep 5
docker exec phe-browser test -f "$P/phe-survives" && echo 'ok - the Firefox profile survives a new container'
