#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
# Android stream test on the Deskmini: a Redroid container and the pheandroid bridge in their OWN network (phepa), capped, then a Playwright browser.
# Needs: image phe-redroid-native:13 (docker/redroid-native, see docs/android.md), the scrcpy server jar (JAR=/path), the repo root as cwd.
#   QUERY='&codec=vp8' selects the codec (Playwright's Chromium has no H.264; real browsers do)
set -eu
JAR=${JAR:?path of the scrcpy server jar}
ROOT=$(pwd)
OUT=$ROOT/web/test/out
mkdir -p "$OUT"
cleanup() { docker rm -f phe-redroid phe-android >/dev/null 2>&1 || true; docker network rm phepa >/dev/null 2>&1 || true; }
cleanup; trap cleanup EXIT
docker network create phepa >/dev/null
docker run -d --name phe-redroid --network phepa --privileged --cpus 2 --memory 3g phe-redroid-native:13 \
    androidboot.redroid_width=720 androidboot.redroid_height=1280 androidboot.redroid_dpi=320 androidboot.use_memfd=1 >/dev/null
docker build -q -f cmd/pheandroid/Dockerfile -t phe-android-bridge:dev . >/dev/null
docker run -d --name phe-android --network phepa --cpus 1 --memory 256m -e PHE_ANDROID_SECRET=testsecret -e PHE_ANDROID_SERIAL=phe-redroid:5555 \
    -v "$JAR":/scrcpy-server.jar:ro phe-android-bridge:dev >/dev/null
until docker logs phe-android 2>&1 | grep -q "server jar pushed"; do sleep 2; done
EXP=$(( $(date +%s) + 3000 ))
SIG=$(printf "android.$EXP" | openssl dgst -sha256 -hmac testsecret -hex | sed 's/.*= //')
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network container:phe-android --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8090 -e TICKET="$EXP.$SIG" -e QUERY="${QUERY:-}" -e OUT=/out \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node android.mjs' || { docker logs phe-android 2>&1 | tail -8; exit 1; }
docker stats --no-stream --format 'redroid {{.CPUPerc}} {{.MemUsage}}' phe-redroid
