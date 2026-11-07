#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Builds the Raspberry Pi 4/5 image (deploy/images/pi/deploy/*.img.xz) with pi-gen in Docker, from the board files in BUNDLE
# (deploy/board/bundle.sh). On an x86 machine Docker needs arm64 emulation:
#   docker run --privileged --rm tonistiigi/binfmt --install arm64
#   sh deploy/images/pi/build.sh BUNDLE        (repository root)
set -eu
PIGEN_TAG=2026-10-06-raspios-trixie-arm64
PIGEN_SHA=b2cd98a9ee08a31ef7d0110ed09f2492bbd972ec
BUNDLE=$(cd "${1:?BUNDLE}" && pwd); HERE=$(cd "$(dirname "$0")" && pwd)
W=$HERE/work; rm -rf "$W/pi-gen"; mkdir -p "$W"
git clone -q --depth 1 --branch "$PIGEN_TAG" https://github.com/RPi-Distro/pi-gen.git "$W/pi-gen"
[ "$(git -C "$W/pi-gen" rev-parse HEAD)" = "$PIGEN_SHA" ] || { echo "pi-gen $PIGEN_TAG is not $PIGEN_SHA" >&2; exit 1; }
cp -r "$HERE/stage-cloud" "$W/pi-gen/stage-cloud"
cp "$BUNDLE/phehost-linux-arm64" "$BUNDLE/cloud-board.tar.gz" "$BUNDLE/install.sh" "$W/pi-gen/stage-cloud/00-install/files/"
cp "$HERE/config" "$W/pi-gen/config"
# pi-gen wants a password for the first user; stage-cloud locks the account, so a random one nobody keeps
echo "FIRST_USER_PASS=$(head -c 24 /dev/urandom | base64 | tr -d '/+=')" >> "$W/pi-gen/config"
# Lite only: no desktop stages, and stage2 makes no image of its own
touch "$W/pi-gen/stage2/SKIP_IMAGES"
rm -f "$W/pi-gen/stage2/EXPORT_NOOBS"
cd "$W/pi-gen"
CONTAINER_NAME=phe-pigen PRESERVE_CONTAINER=0 ./build-docker.sh
mkdir -p "$HERE/deploy" && mv deploy/*.img.xz "$HERE/deploy/" && ls -la "$HERE/deploy"
