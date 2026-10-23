#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# phecloud behind Traefik (Docker, repo root): project phepk3, only Traefik (172.31.77.10) is a trusted proxy. Screenshot: web/test/out/proxy-traefik.png
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
DC="docker compose -p phepk3 -f docker-compose.yml -f web/test/compose.traefik.yml"
$DC down -v >/dev/null 2>&1 || true
$DC up -d --build phecloud traefik >/dev/null 2>&1
trap '$DC down -v >/dev/null 2>&1' EXIT
sleep 8
A=$(docker exec phe-cloud-px phecloud paircode --admin 2>/dev/null)
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network phepk3_default --ipc=host \
  -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e VIA=https://phecloud.test -e DIRECT=http://phe-cloud-px:8080 -e TRAEFIK=phe-traefik-px -e CODE="$A" -e OUT=/out \
  mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node proxy.mjs' || { docker logs phe-cloud-px 2>&1 | tail -5 | cut -c1-200; docker logs phe-traefik-px 2>&1 | tail -5 | cut -c1-200; exit 1; }
