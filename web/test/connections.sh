#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
# Connections page test (Docker, repo root): own compose project (phepk2), a go-imap in-memory IMAP server container (phe-imapmem) and
# the Radicale of the spike (DAV_URL, default the box's). Screenshots: web/test/out/connections-*.png
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
DC="docker compose -p phepk2 -f docker-compose.yml -f web/test/compose.passkey.yml"
$DC down -v >/dev/null 2>&1 || true
docker rm -f phe-imapmem >/dev/null 2>&1 || true
$DC up -d --build phecloud >/dev/null 2>&1
trap '$DC down -v >/dev/null 2>&1; docker rm -f phe-imapmem >/dev/null 2>&1' EXIT
NET=$(docker inspect phe-cloud-pk --format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}}{{end}}')
docker run -d --name phe-imapmem --cpus 0.5 --memory 256m --network "$NET" golang:1.26-bookworm sh -c \
  'go run github.com/emersion/go-imap/v2/cmd/imapmemserver@v2.0.0-beta.8 -username me -password secret -insecure-auth -listen 0.0.0.0:1143' >/dev/null
for i in $(seq 1 60); do docker logs phe-imapmem 2>&1 | grep -q "listening" && break; sleep 3; done
docker logs phe-imapmem 2>&1 | tail -2
sleep 3
A=$(docker exec phe-cloud-pk phecloud paircode --admin 2>/dev/null)
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network container:phe-cloud-pk --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$A" -e OUT=/out \
    -e DAV_URL="${DAV_URL:-http://192.168.2.247:5232/phone/fixture/}" -e DAV_USER="${DAV_USER:-phone}" -e DAV_PASS="${DAV_PASS:-phonepass}" \
    -e IMAP_HOST=phe-imapmem -e IMAP_PORT=1143 \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node connections.mjs' || { docker logs phe-cloud-pk 2>&1 | tail -8 | cut -c1-240; exit 1; }
