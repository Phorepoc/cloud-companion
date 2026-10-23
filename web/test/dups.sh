#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Duplicate-review test in the browser against the throwaway hub (web/test/hubtest-up.sh up). Screenshots: web/test/out/dups-*.png
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
A=$(docker exec phe-cloud-t phecloud paircode --admin 2>/dev/null)
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network container:phe-cloud-t --ipc=host \
  -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$A" -e OUT=/out \
  mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node dups.mjs'
