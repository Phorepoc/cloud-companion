#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# The SIP service connection against the real Asterisk of the compose profile (repo root): builds the image, starts it on its own
# network with a fresh configuration, and runs the Go test tagged "asterisk" next to it.
set -eu
NET=phe-asterisk-test
docker rm -f phe-asterisk-t phe-asterisk-go >/dev/null 2>&1 || true
docker network rm $NET >/dev/null 2>&1 || true
docker build -q -t phecloud-asterisk:test deploy/asterisk >/dev/null
docker network create $NET >/dev/null
trap 'docker rm -f phe-asterisk-t phe-asterisk-go >/dev/null 2>&1; docker network rm '$NET' >/dev/null 2>&1' EXIT
docker run -d --name phe-asterisk-t --network $NET --cpus 1 --memory 512m \
	-e PHECLOUD_ASTERISK_SECRET=cc-test-secret -e PHECLOUD_ASTERISK_PHONE_201=phone-201-secret phecloud-asterisk:test >/dev/null
for i in $(seq 1 60); do docker logs phe-asterisk-t 2>&1 | grep -q "Asterisk Ready" && break; sleep 1; done
docker logs phe-asterisk-t 2>&1 | grep -E "phecloud-asterisk|Asterisk Ready" | head -3
mkdir -p internal/webui/dist && touch internal/webui/dist/placeholder.txt
docker run --rm --name phe-asterisk-go --network $NET -v "$(pwd)":/src -v "$HOME/.cache/go-docker":/go/pkg -w /src \
	-e GOFLAGS="-mod=mod -buildvcs=false" -e ASTERISK=phe-asterisk-t -e CC_SECRET=cc-test-secret -e P201=phone-201-secret \
	golang:1.26 go test -count=1 -timeout 120s -tags asterisk -run TestAsterisk -v ./internal/sipsvc/ \
	|| { docker logs phe-asterisk-t 2>&1 | tail -25; exit 1; }
