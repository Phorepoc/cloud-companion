#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# A board's first boot and recovery: the host manager (phehost) in its own Docker daemon with a setup code on its "card"; the
# owner sets up in the browser, becomes Cloud Companion's owner, and after Cloud Companion is stopped uses the recovery page
# (Docker, privileged; repo root)
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"; W=$(mktemp -d)
trap 'docker rm -f -v phe-board >/dev/null 2>&1; docker run --rm -v "$W":/w alpine:3.22 sh -c "rm -rf /w/* /w/.[!.]*" >/dev/null 2>&1; rm -rf "$W"' EXIT
[ -n "${PHE_UP:-}" ] || docker build -q -t phe-cloud:dev . >/dev/null
docker run --rm -v "$ROOT":/src -v "$W":/out -w /src -e CGO_ENABLED=0 -e GOFLAGS="-tags=goolm -buildvcs=false" \
  golang:1.26-bookworm@sha256:23ce71337b726608afb1bc00986e630a05034a87a87ea1fc2cd1ae5cf33387eb go build -o /out/phehost ./cmd/phehost
mkdir -p "$W/cloud/deploy" "$W/state" "$W/boot"
cp docker-compose*.yml "$W/cloud/" && cp -r deploy/features deploy/Caddyfile* deploy/hub deploy/asterisk "$W/cloud/deploy/"
printf '# written by Raspberry Pi Imager\ncode=PINE-APPLE-42\n' > "$W/boot/cloud-setup.txt"
docker save phe-cloud:dev -o "$W/phecloud.tar"
docker run -d --name phe-board --privileged -e DOCKER_TLS_CERTDIR= -v "$W":/t docker:27-dind >/dev/null
B() { docker exec phe-board "$@"; }
for i in $(seq 1 60); do B docker info >/dev/null 2>&1 && break; sleep 1; done
B docker load -q -i /t/phecloud.tar >/dev/null
B docker tag phe-cloud:dev ghcr.io/phorepoc/phecloud:v0.2.0
B sh -c 'mkdir -p /run/phehost && (/t/phehost -compose /t/cloud -state /t/state -socket /run/phehost/api.sock -setup-code /t/boot/cloud-setup.txt -recover-after 20s -hand-over 5s >/t/phehost.log 2>&1 &)'
sleep 2
rm -f "$OUT/setup-owner-done" "$OUT/setup-stopped"
fail() { echo "$1"; B tail -20 /t/phehost.log; exit 1; }
docker run --rm --network container:phe-board --ipc=host -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp \
  -e BASE=http://127.0.0.1 -e OUT=/out mcr.microsoft.com/playwright:v1.49.1-noble \
  sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning setup.ts' &
P=$!
for i in $(seq 1 900); do [ -e "$OUT/setup-owner-done" ] && break; kill -0 $P 2>/dev/null || break; sleep 1; done
[ -e "$OUT/setup-owner-done" ] || { wait $P || true; fail "setup failed"; }
B docker stop phe-cloud >/dev/null
touch "$OUT/setup-stopped"
wait $P || { B docker ps -a --format "{{.Names}} {{.Status}}"; B docker logs --tail 15 phe-cloud 2>&1; fail "recovery failed"; }
B grep -q '"core"' /t/state/state.json || fail "no saved state"
echo "setup ok"
