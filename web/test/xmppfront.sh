#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
# The XMPP front (Docker, repo root): logins made through the real UI (xmppfront.ts), then two Jabber clients (slixmpp, BSD-3/MIT)
# chat through the front, once over modern TLS and once over TLS 1.0 as the N9. Own compose project, no host ports.
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
DC="docker compose -p phexf -f docker-compose.yml -f docker-compose.build.yml -f web/test/compose.passkey.yml -f web/test/compose.xmppfront.yml"
$DC down -v >/dev/null 2>&1 || true
$DC up -d --build phecloud > "$OUT/xmppfront-up.log" 2>&1 || { tail -20 "$OUT/xmppfront-up.log"; exit 1; }
trap '$DC down -v >/dev/null 2>&1' EXIT
sleep 4
A=$(docker exec phe-cloud-pk phecloud paircode --admin 2>/dev/null)
docker run --rm --name phe-playwright-xf --cpus 2 --memory 2g --network container:phe-cloud-pk --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$A" -e OUT=/out \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning xmppfront.ts' \
    || { docker logs phe-cloud-pk 2>&1 | tail -10 | cut -c1-240; exit 1; }
for tls in modern 1.0; do
    docker run --rm --name phe-slixmpp-xf --cpus 1 --memory 512m --network container:phe-cloud-pk -v "$ROOT/web/test":/t:ro -v "$OUT":/out \
        -e OUT=/out -e TLS=$tls -e PIP_DISABLE_PIP_VERSION_CHECK=1 python:3.13-slim \
        sh -c 'pip install --quiet --root-user-action=ignore slixmpp==1.17.0 && python -I /t/xmppfront.py' \
        || { docker logs phe-cloud-pk 2>&1 | tail -10 | cut -c1-240; exit 1; }
done
echo "xmppfront: all passed"
