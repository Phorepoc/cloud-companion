#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# The Signal connection against a real signal-cli-rest-api with no account linked (repo root): the link QR code and "not linked
# yet". The rest (messages both ways, groups, both receive modes) runs against a mock in `go test ./internal/signal`; linking a real
# phone is checked by hand (docs/messaging.md).
set -eu
NET=phe-signal-test
docker rm -f phe-signal-t phe-signal-go >/dev/null 2>&1 || true
docker network rm $NET >/dev/null 2>&1 || true
docker network create $NET >/dev/null
trap 'docker rm -f phe-signal-t phe-signal-go >/dev/null 2>&1; docker network rm '$NET' >/dev/null 2>&1' EXIT
docker run -d --name phe-signal-t --network $NET --cpus 1 --memory 768m -e MODE=json-rpc bbernhard/signal-cli-rest-api:0.101 >/dev/null
for i in $(seq 1 60); do docker logs phe-signal-t 2>&1 | grep -qi "listening" && break; sleep 1; done
mkdir -p internal/webui/dist && touch internal/webui/dist/placeholder.txt
docker run --rm --name phe-signal-go --network $NET -v "$(pwd)":/src -v "$HOME/.cache/go-docker":/go/pkg -w /src \
	-e GOFLAGS="-mod=mod -buildvcs=false -tags=goolm,signal" -e CGO_ENABLED=0 -e SIGNAL_API=http://phe-signal-t:8080 \
	golang:1.26 go test -count=1 -timeout 120s -run TestRealAPI -v ./internal/signal/ \
	|| { docker logs phe-signal-t 2>&1 | tail -20; exit 1; }
