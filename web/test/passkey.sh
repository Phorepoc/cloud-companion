#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
# Passkey / recovery-code sign-in test with a virtual authenticator (Docker, from the repo root). Screenshots: web/test/out/passkey-*.png
set -eu
ROOT=$(pwd)
OUT=$ROOT/web/test/out
mkdir -p "$OUT"
docker compose down -v >/dev/null 2>&1 || true     # a fresh database: no owner yet
docker compose up -d --build >/dev/null 2>&1
trap 'docker compose down >/dev/null 2>&1' EXIT
sleep 6
A=$(docker exec phe-cloud phecloud paircode --admin 2>/dev/null)
P1=$(docker exec phe-cloud phecloud paircode 2>/dev/null)
P2=$(docker exec phe-cloud phecloud paircode 2>/dev/null)
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network container:phe-cloud --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODES="$A,$P1,$P2" -e OUT=/out \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node passkey.mjs' || { echo '--- server log'; docker logs phe-cloud 2>&1 | tail -12 | cut -c1-260; exit 1; }
