#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# The board files of a release (deploy/board/install.sh fetches them): phehost for arm64 and amd64, cloud-board.tar.gz (the
# compose files and deploy/) and SHA256SUMS.board, in OUT. Needs Go; run from the repository root.
#   sh deploy/board/bundle.sh OUT
set -eu
OUT=${1:?OUT}; mkdir -p "$OUT"
for a in arm64 amd64; do
  CGO_ENABLED=0 GOOS=linux GOARCH=$a go build -trimpath -ldflags=-s -o "$OUT/phehost-linux-$a" ./cmd/phehost
done
tar -czf "$OUT/cloud-board.tar.gz" --owner=0 --group=0 --sort=name \
  docker-compose*.yml deploy/features deploy/board deploy/Caddyfile* deploy/hub deploy/asterisk
cp deploy/board/install.sh "$OUT/install.sh"
(cd "$OUT" && sha256sum phehost-linux-arm64 phehost-linux-amd64 cloud-board.tar.gz install.sh > SHA256SUMS.board)
