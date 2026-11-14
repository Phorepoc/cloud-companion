#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# The XMPP connection against a real server (repo root): Prosody (MIT) on its own network with its localhost certificate, users
# alice, bob, carol and dave, a group chat service (conference.localhost), and the Go test tagged "xmpp" next to it.
set -eu
NET=phe-xmpp-test
docker rm -f phe-prosody-t phe-xmpp-go >/dev/null 2>&1 || true
docker network rm $NET >/dev/null 2>&1 || true
docker network create $NET >/dev/null
trap 'docker rm -f phe-prosody-t phe-xmpp-go >/dev/null 2>&1; docker network rm '$NET' >/dev/null 2>&1' EXIT
docker run -d --name phe-prosody-t --network $NET --cpus 1 --memory 256m -e PROSODY_VIRTUAL_HOSTS=localhost -e PROSODY_COMPONENTS=conference.localhost:muc \
	-e PROSODY_C2S_RATE_LIMIT=1mb/s -e PROSODY_LOGLEVEL="${PROSODY_LOGLEVEL:-info}" prosodyim/prosody:13.0 >/dev/null
for i in $(seq 1 30); do docker exec phe-prosody-t prosodyctl status >/dev/null 2>&1 && break; sleep 1; done
for u in alice bob carol dave; do docker exec phe-prosody-t prosodyctl register $u localhost pw-$u; done
mkdir -p internal/webui/dist && touch internal/webui/dist/placeholder.txt
docker run --rm --name phe-xmpp-go --network $NET -v "$(pwd)":/src -v "$HOME/.cache/go-docker":/go/pkg -w /src \
	-e GOFLAGS="-mod=mod -buildvcs=false -tags=goolm,xmpp" -e CGO_ENABLED=0 -e XMPP=phe-prosody-t:5222 -e XMPP_LOG="${XMPP_LOG:-}" \
	golang:1.26 go test -count=1 -timeout 180s -run "TestProsody" -v ./internal/xmpp/ \
	|| { docker logs phe-prosody-t 2>&1 | tail -${LOGLINES:-20}; exit 1; }
