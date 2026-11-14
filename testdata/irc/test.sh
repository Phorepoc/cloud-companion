#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# The IRC connection against a real server (repo root): Ergo (MIT) on its own network with its default configuration (TLS on 6697
# with a self-made certificate, account registration open), and the Go test tagged "irc" next to it.
set -eu
NET=phe-irc-test
docker rm -f phe-ergo-t phe-irc-go >/dev/null 2>&1 || true
docker network rm $NET >/dev/null 2>&1 || true
docker network create $NET >/dev/null
trap 'docker rm -f phe-ergo-t phe-irc-go >/dev/null 2>&1; docker network rm '$NET' >/dev/null 2>&1' EXIT
docker run -d --name phe-ergo-t --network $NET --cpus 1 --memory 256m ghcr.io/ergochat/ergo:v2.19.1 >/dev/null
for i in $(seq 1 30); do docker logs phe-ergo-t 2>&1 | grep -q "Server running" && break; sleep 1; done
mkdir -p internal/webui/dist && touch internal/webui/dist/placeholder.txt
docker run --rm --name phe-irc-go --network $NET -v "$(pwd)":/src -v "$HOME/.cache/go-docker":/go/pkg -w /src \
	-e GOFLAGS="-mod=mod -buildvcs=false -tags=goolm,irc" -e CGO_ENABLED=0 -e IRC=phe-ergo-t:6697 \
	golang:1.26 go test -count=1 -timeout 180s -run TestErgo -v ./internal/irc/ \
	|| { docker logs phe-ergo-t 2>&1 | tail -20; exit 1; }
