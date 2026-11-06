#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
# Files over WebDAV (Docker, repo root): dav.ts makes a phone login in Connections and finds the address in Files → Use from other
# apps; then rclone (a real WebDAV client, its official image) lists, writes, reads back, moves and deletes with that login,
# and is refused with a wrong password. Screenshots: web/test/out/dav-*.png
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
DC="docker compose -p phedav -f docker-compose.yml -f docker-compose.build.yml -f web/test/compose.passkey.yml"
$DC down -v >/dev/null 2>&1 || true
$DC up -d --build phecloud > "$OUT/dav-up.log" 2>&1 || { tail -20 "$OUT/dav-up.log"; exit 1; }
trap '$DC down -v >/dev/null 2>&1' EXIT
A=$(docker exec phe-cloud-pk phecloud paircode --admin 2>/dev/null)
rm -f "$OUT/dav-login"
docker run --rm --name phe-playwright-dav --cpus 2 --memory 2g --network container:phe-cloud-pk --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$A" -e OUT=/out \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning dav.ts' \
    || { docker logs phe-cloud-pk 2>&1 | tail -10 | cut -c1-240; exit 1; }
read -r URL USER PASS < "$OUT/dav-login"
rc() { p=$1; shift; docker run --rm --network container:phe-cloud-pk -e RCLONE_CONFIG_CC_TYPE=webdav -e RCLONE_CONFIG_CC_URL="$URL" \
	-e RCLONE_CONFIG_CC_USER="$USER" -e RCLONE_CONFIG_CC_PASS="$(docker run --rm rclone/rclone:1.71.1 obscure "$p")" rclone/rclone:1.71.1 -q "$@" 2>&1; }
check() { echo "$2" | grep -q "$3" || { echo "FAIL $1: $2"; exit 1; }; echo "ok   $1"; }
printf 'written by rclone' > "$OUT/dav-up.txt"
check "the top lists the uploaded file" "$(rc "$PASS" lsf cc:)" "hello.txt"
check "reads a file back" "$(rc "$PASS" cat cc:hello.txt)" "hello from the browser"
check "reads part of it" "$(rc "$PASS" cat cc:hello.txt --offset 6 --count 4)" "^from$"
docker run --rm --network container:phe-cloud-pk -v "$OUT":/o -e RCLONE_CONFIG_CC_TYPE=webdav -e RCLONE_CONFIG_CC_URL="$URL" -e RCLONE_CONFIG_CC_USER="$USER" \
	-e RCLONE_CONFIG_CC_PASS="$(docker run --rm rclone/rclone:1.71.1 obscure "$PASS")" rclone/rclone:1.71.1 -q copyto /o/dav-up.txt cc:Docs/up.txt
check "writes into a new folder" "$(rc "$PASS" cat cc:Docs/up.txt)" "written by rclone"
rc "$PASS" moveto cc:Docs/up.txt cc:Docs/moved.txt >/dev/null
check "moves" "$(rc "$PASS" lsf cc:Docs)" "^moved.txt$"
rc "$PASS" deletefile cc:Docs/moved.txt >/dev/null
check "deletes" "$(rc "$PASS" lsf cc:Docs; echo end)" "^end$"
check "a wrong password is refused" "$(rc wrong-password lsf cc: || true)" "401"
echo "dav: all passed"
