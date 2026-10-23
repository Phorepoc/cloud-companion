#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Run the end-to-end connector test against the throwaway hub (web/test/hubtest-up.sh up) and the spike's Radicale.
set -eu
T=${HUBTEST_DIR:-$HOME/phecloud-spike/hubtest}
ROOT=$(pwd)
docker run --rm --cpus 2 --memory 2g --network phetest_default --env-file "$T/deploy/hub/.env.phecloud" \
  -e RADICALE_URL="${RADICALE_URL:-http://127.0.0.1:5232}" -e GOFLAGS=-buildvcs=false -v "$ROOT":/src -w /src golang:1.26-bookworm \
  go test -count=1 -tags integration -run TestHubEndToEnd -v ./internal/connectors
