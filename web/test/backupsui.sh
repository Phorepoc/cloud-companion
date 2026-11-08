#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# How Settings, Backups (backupsui.ts) or another host manager page (UI_TEST=updatesui) looks: Cloud Companion (phe-cloud:dev) with a real host manager beside it and no board;
# the manager's backup.json is written here to show the page's other states. (Docker; repo root)
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"; W=$(mktemp -d)
CODE=Q7XK-M3PD-T9RW-B2HF
trap 'docker rm -f -v phe-ui-cloud phe-ui-host >/dev/null 2>&1; docker volume rm phe-ui-sock >/dev/null 2>&1; docker run --rm -v "$W":/w alpine:3.22 rm -rf /w/state; rm -rf "$W"' EXIT
[ -n "${PHE_UP:-}" ] || docker build -q -t phe-cloud:dev . >/dev/null
docker run --rm -v "$ROOT":/src -v "$W":/out -w /src -e CGO_ENABLED=0 -e GOFLAGS="-tags=goolm -buildvcs=false" \
  golang:1.26-bookworm@sha256:23ce71337b726608afb1bc00986e630a05034a87a87ea1fc2cd1ae5cf33387eb go build -o /out/phehost ./cmd/phehost
mkdir -p "$W/state"; docker volume create phe-ui-sock >/dev/null
# UI_TEST=systemui: the board's remote login for real: sshd, a locked user cloud, systemctl standing in for systemd
HOST_IMAGE=debian:trixie-slim HOST_ARGS= HOST_MOUNT=
if [ "${UI_TEST:-}" = systemui ]; then
  HOST_IMAGE=phe-ui-board:test HOST_ARGS="-ssh-user cloud"
  printf '%s\n' 'FROM debian:trixie-slim' \
    'RUN apt-get update -q && apt-get install -y -q --no-install-recommends openssh-server >/dev/null && rm -rf /var/lib/apt/lists/* && useradd -m cloud && passwd -l cloud && mkdir -p /run/sshd' \
    | docker build -q -t phe-ui-board:test - >/dev/null
  printf '#!/bin/sh\ncase "$1 $2" in\n"enable --now") /usr/sbin/sshd ;;\n"disable --now") kill "$(cat /run/sshd.pid)" 2>/dev/null; true ;;\nesac\n' > "$W/systemctl"
  chmod +x "$W/systemctl"; HOST_MOUNT="-v $W/systemctl:/usr/local/bin/systemctl:ro"
fi
docker run -d --name phe-ui-host -v phe-ui-sock:/run/phehost -v "$W":/w -v "$ROOT/deploy/features":/features:ro $HOST_MOUNT $HOST_IMAGE \
  /w/phehost -compose /w -features /features -state /w/state -socket /run/phehost/api.sock -listen 127.0.0.1:1 $HOST_ARGS >/dev/null
docker run -d --name phe-ui-cloud -v phe-ui-sock:/run/phehost -e PHECLOUD_FIRST_OWNER_CODE=$CODE phe-cloud:dev >/dev/null
sleep 5
docker run --rm --network container:phe-ui-cloud --ipc=host -v "$ROOT/web/test":/t -v "$OUT":/out -v "$W/state":/state -w /t -e HOME=/tmp \
  -e BASE=http://127.0.0.1:8080 -e OUT=/out -e CODE=$CODE \
  -e UI_TEST="${UI_TEST:-backupsui}" mcr.microsoft.com/playwright:v1.49.1-noble \
  sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning $UI_TEST.ts'
# systemui leaves remote login on with its password in OUT/ssh-password: log in for real, then the test turns it off
if [ "${UI_TEST:-}" = systemui ]; then
  P=$(cat "$OUT/ssh-password"); rm -f "$OUT/ssh-password"
  docker run --rm --network container:phe-ui-host -e SSHPASS="$P" alpine:3.22 sh -c \
    'apk add -q openssh-client sshpass >/dev/null && sshpass -e ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR cloud@127.0.0.1 "echo logged in as \$(whoami)"' | grep -x "logged in as cloud"
  echo "ssh login ok"
fi
