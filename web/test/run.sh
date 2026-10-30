#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
#
# Web app smoke test (Docker, from the repo root): builds the server image (which builds the Quasar app), starts it, and runs the
# Playwright test in a container on the same network. Screenshots land in web/test/out/.
set -eu
ROOT=$(pwd)
OUT=$ROOT/web/test/out
mkdir -p "$OUT"
docker compose up -d --build >/dev/null 2>&1
trap 'docker compose --profile tls down >/dev/null 2>&1' EXIT
sleep 6
CODE=$(docker exec phe-cloud phecloud paircode --admin 2>/dev/null)
NET=$(docker inspect phe-cloud --format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}}{{end}}')
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network "$NET" --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://phecloud:8080 -e CODE="$CODE" -e OUT=/out \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning smoke.ts' || { echo '--- server log'; docker logs phe-cloud 2>&1 | tail -12 | cut -c1-260; exit 1; }

# (the desktop view and the Etesian theme: web/test/desktop.sh. It needs a secure origin (localhost) for the Android window, so it
# has its own run, as in CI.)

# HTTPS through the Caddy reverse proxy (compose profile "tls", internal CA): pair, open example.com, tap the link over wss
if [ "${TLS_CHECK:-1}" = 1 ]; then
    echo "== HTTPS via Caddy (profile tls)"
    PHECLOUD_HTTPS_PORT=18443 PHECLOUD_DOMAIN=caddy CADDYFILE=./deploy/Caddyfile.local docker compose --profile tls up -d caddy >/dev/null 2>&1
    sleep 6
    CODE2=$(docker exec phe-cloud phecloud paircode --admin 2>/dev/null)
    docker run --rm --name phe-playwright --cpus 2 --memory 2g --network "$NET" --ipc=host \
        -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=https://caddy -e CODE="$CODE2" -e OUT=/out -e QUICK=1 -e IGNORE_TLS=1 \
        mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning smoke.ts'
fi

# two devices at once: the "tabs on your devices" list (Browser section)
echo "== tabs across devices"
CA=$(docker exec phe-cloud phecloud paircode 2>/dev/null); CB=$(docker exec phe-cloud phecloud paircode 2>/dev/null)
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network "$NET" --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://phecloud:8080 -e CODES="$CA,$CB" -e OUT=/out \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning multi.ts'
