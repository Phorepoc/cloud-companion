#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# The README screenshots, DEMO data only (Docker, repo root): own compose project (pheshots), fresh volumes, released images, nothing
# is built. Writes web/test/out/readme-{chats,browser,desktop}.png (copy them to .github/readme/ as chats, browser, desktop).
#   PHE_SITE_URL=<page>   the page the Browser shots open (default https://en.wikipedia.org/wiki/Nokia_701; needs the internet)
#   PHE_EXTRA=<file>      one more compose file: pins images (there is no published phecloud-browser image yet, so name one built from deploy/browser)
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
export PHECLOUD_VERSION=${PHECLOUD_VERSION:-latest} PHECLOUD_BROWSER_PASSWORD=test-browser-password PHECLOUD_BROWSER_SECRET=test-browser-secret PHE_BROWSER_IP=127.0.0.1
DC="docker compose -p pheshots -f docker-compose.yml -f web/test/compose.passkey.yml -f docker-compose.browser.yml -f web/test/compose.browser.yml ${PHE_EXTRA:+-f $PHE_EXTRA}"
$DC down -v >/dev/null 2>&1 || true
sh web/test/pull.sh $DC
$DC up -d phecloud browser >/dev/null 2>&1
trap '$DC down -v >/dev/null 2>&1' EXIT
# the video goes straight from Firefox's container to the browser under test (which shares phecloud's network): use the container's address
export PHE_BROWSER_IP=$(docker inspect phe-browser --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')
$DC up -d --force-recreate browser >/dev/null 2>&1
sleep 20
A=$(docker exec phe-cloud-pk phecloud paircode --admin 2>/dev/null | head -1); P=$(docker exec phe-cloud-pk phecloud paircode 2>/dev/null | head -1)
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network container:phe-cloud-pk --ipc=host \
  -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODES="$A,$P" \
  -e SITE_URL="${PHE_SITE_URL:-https://en.wikipedia.org/wiki/Nokia_701}" -e OUT=/out \
  mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning readme-shots.ts'
