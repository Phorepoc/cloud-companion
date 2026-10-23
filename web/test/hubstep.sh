#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
#
# Contacts and Calendar through a running hub (Docker, from the repo root):
#   HUB_ENV_FILE=<the hub's deploy/hub/.env.phecloud> HUB_NETWORK=phehub_default sh web/test/hubstep.sh
# Starts the server image with the hub overlay (web/test/compose.hub.yml) and runs web/test/hub.mjs with Playwright.
set -eu
ROOT=$(pwd)
OUT=$ROOT/web/test/out
mkdir -p "$OUT"
export HUB_ENV_FILE HUB_NETWORK="${HUB_NETWORK:-phehub_default}"
docker compose -f docker-compose.yml -f web/test/compose.hub.yml up -d >/dev/null 2>&1
trap 'docker compose -f docker-compose.yml -f web/test/compose.hub.yml down >/dev/null 2>&1' EXIT
sleep 6
NET=$(docker inspect phe-cloud --format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} {{end}}' | tr ' ' '\n' | grep -v '^$' | grep -v "^$HUB_NETWORK\$" | head -1)
CODE=$(docker exec phe-cloud phecloud paircode --admin 2>/dev/null)
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network "$NET" --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://phecloud:8080 -e CODE="$CODE" -e OUT=/out \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node hub.mjs' \
    || { echo '--- server log'; docker logs phe-cloud 2>&1 | tail -12 | cut -c1-260; exit 1; }
