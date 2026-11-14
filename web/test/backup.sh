#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# Backups and a restore on a fresh board (backup.ts): board A (its own Docker daemon, the host manager) is set up and backs up
# to a restic REST server, then is thrown away; board B, new, restores from the server on its setup page.
# (Docker, privileged; repo root)
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"; W=$(mktemp -d)
# the board writes as root in $W: removed from a container
trap 'docker rm -f -v phe-board phe-rest >/dev/null 2>&1; docker network rm phe-bk >/dev/null 2>&1; docker run --rm -v "$W":/w alpine:3.22 rm -rf /w/cloud /w/state; rm -rf "$W"' EXIT
[ -n "${PHE_UP:-}" ] || docker build -q -t phe-cloud:dev . >/dev/null
docker run --rm -v "$ROOT":/src -v "$W":/out -w /src -e CGO_ENABLED=0 -e GOFLAGS="-tags=goolm -buildvcs=false" \
  golang:1.26-bookworm@sha256:23ce71337b726608afb1bc00986e630a05034a87a87ea1fc2cd1ae5cf33387eb go build -o /out/phehost ./cmd/phehost
docker save phe-cloud:dev -o "$W/phecloud.tar"
docker network create phe-bk >/dev/null
docker run -d --name phe-rest --network phe-bk restic/rest-server:0.14.0 sh -c 'rest-server --path /data --no-auth' >/dev/null
REST="http://$(docker inspect -f '{{(index .NetworkSettings.Networks "phe-bk").IPAddress}}' phe-rest):8000/cloud"
B() { docker exec phe-board "$@"; }
board() { # a fresh board: its own disk, state and Docker daemon
  docker rm -f -v phe-board >/dev/null 2>&1 || true
  docker run --rm -v "$W":/w alpine:3.22 rm -rf /w/cloud /w/state; mkdir -p "$W/cloud/deploy" "$W/state" "$W/boot"
  cp docker-compose*.yml "$W/cloud/" && cp -r deploy/features deploy/Caddyfile* deploy/hub deploy/asterisk "$W/cloud/deploy/"
  printf 'code=PINE-APPLE-42\n' > "$W/boot/cloud-setup.txt"
  docker run -d --name phe-board --privileged --network phe-bk -e DOCKER_TLS_CERTDIR= -v "$W":/t docker:27-dind >/dev/null
  for i in $(seq 1 60); do B docker info >/dev/null 2>&1 && break; sleep 1; done
  B docker load -q -i /t/phecloud.tar >/dev/null
  B docker tag phe-cloud:dev ghcr.io/phorepoc/phecloud:v0.2.0
  B sh -c 'mkdir -p /run/phehost && (/t/phehost -compose /t/cloud -state /t/state -socket /run/phehost/api.sock -setup-code /t/boot/cloud-setup.txt >/t/phehost.log 2>&1 &)'
  sleep 2
}
play() {
  docker run --rm --network container:phe-board --ipc=host -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp \
    -e BASE=http://127.0.0.1 -e OUT=/out -e REST="$REST" -e PHASE="$1" mcr.microsoft.com/playwright:v1.49.1-noble \
    sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning backup.ts' \
    || { B tail -30 /t/phehost.log; exit 1; }
}
rm -f "$OUT/backup-browser.json" "$OUT/backup-creds.json"
board; play backup
[ -n "${ONLY_BACKUP:-}" ] && { echo "backup phase ok"; exit 0; }
board; play restore
echo "backup ok"
