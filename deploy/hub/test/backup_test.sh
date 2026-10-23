#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
#
# Backup and restore test (Docker host, from the repo root). Needs the hub running (phe-stalwart on the network HUB_NETWORK) and phe-cloud
# running with the hub environment, plus the phe-cloud:dev image:
#   HUB_ENV_FILE=<hub's deploy/hub/.env.phecloud> HUB_NETWORK=phehub_default sh deploy/hub/test/backup_test.sh
# It counts contacts, events, messages, devices and the TLS fingerprint, runs `phecloud hub backup`, restores into FRESH volumes and
# containers (phe-rt-*), compares, and removes everything it made. The original hub is only stopped for the few seconds of the backup.
# (The backup directory is root-owned with mode 0700: it holds the TLS key and the hub's secrets, so the script handles it through Docker.)
set -eu
HUB_ENV_FILE=${HUB_ENV_FILE:?set HUB_ENV_FILE}
HUBNET=${HUB_NETWORK:-phehub_default}
BK=${BK:-/tmp/phe-backup-test}
TOKEN=$(grep '^PHECLOUD_HUB_TOKEN=' "$HUB_ENV_FILE" | cut -d= -f2)
OWNER=$(grep '^PHECLOUD_HUB_OWNER=' "$HUB_ENV_FILE" | cut -d= -f2)
U=${OWNER%@*}%40${OWNER#*@}
curlnet() { net=$1; shift; docker run --rm --cpus 0.5 --memory 64m --network "$net" curlimages/curl:8.11.1 -s "$@"; }
bb() { docker run --rm -v "$BK":/b busybox:1.37 "$@"; }
SOCK="-v /var/run/docker.sock:/var/run/docker.sock"
cleanup() {
    docker rm -f phe-rt-stalwart >/dev/null 2>&1 || true
    docker network rm phe-rt-net >/dev/null 2>&1 || true
    docker volume rm phe-rt-stalwart-data phe-rt-stalwart-etc phe-rt-phecloud-data phe-rt2-stalwart-data phe-rt2-stalwart-etc phe-rt2-phecloud-data >/dev/null 2>&1 || true
    [ -d "$BK" ] && { bb sh -c 'rm -rf /b/* /b/.[!.]*' >/dev/null 2>&1 || true; rmdir "$BK" 2>/dev/null || true; }
}
trap cleanup EXIT
cleanup

ACC=$(curlnet "$HUBNET" -H "Authorization: Bearer $TOKEN" http://stalwart:8080/jmap/session | sed -n 's/.*"urn:ietf:params:jmap:mail":"\([^"]*\)".*/\1/p')
# what the hub holds, through the HTTP API of whichever Stalwart is on the network: contacts, events, messages (all folders)
sum() {
    net=$1
    c=$(curlnet "$net" -X PROPFIND -H 'Depth: 1' -H "Authorization: Bearer $TOKEN" "http://stalwart:8080/dav/card/$U/default/" | grep -o '\.vcf' | wc -l)
    e=$(curlnet "$net" -X PROPFIND -H 'Depth: 1' -H "Authorization: Bearer $TOKEN" "http://stalwart:8080/dav/cal/$U/default/" | grep -o '\.ics' | wc -l)
    m=$(curlnet "$net" -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' http://stalwart:8080/jmap \
        -d "{\"using\":[\"urn:ietf:params:jmap:core\",\"urn:ietf:params:jmap:mail\"],\"methodCalls\":[[\"Mailbox/get\",{\"accountId\":\"$ACC\"},\"a\"]]}" |
        grep -o '"totalEmails":[0-9]*' | cut -d: -f2 | { t=0; while read -r n; do t=$((t + n)); done; echo $t; })
    echo "$c $e $m"
}
BEFORE=$(sum "$HUBNET")
[ "$BEFORE" != "0 0 0" ] || { echo "FAIL: the hub shows nothing to back up (is it running, is HUB_ENV_FILE right?)"; exit 1; }
DEV_BEFORE=$(docker exec phe-cloud phecloud devices | wc -l)
FP_BEFORE=$(docker exec phe-cloud phecloud paircode 2>&1 >/dev/null | grep -o '[0-9a-f]\{64\}')
echo "before: contacts/events/mails = $BEFORE, devices = $DEV_BEFORE, tls = $(echo "$FP_BEFORE" | cut -c1-12)..."

docker build -q -t phe-backup:dev -f deploy/hub/Dockerfile.backup . >/dev/null
mkdir -p "$BK"
echo "== backup"
docker run --rm --cpus 1 --memory 256m $SOCK -v "$BK":/backup phe-backup:dev phecloud hub backup /backup
[ "$(docker inspect phe-stalwart --format '{{.State.Running}}')" = true ] || { echo "FAIL: Stalwart was not started again"; exit 1; }
D=$(bb sh -c 'ls -d /b/hub-backup-*' | head -1 | xargs basename)
bb ls -la "/b/$D"

restore() { docker run --rm $SOCK -v "$BK":/backup phe-backup:dev phecloud hub restore "/backup/$D" "$@"; }
echo "== restore into fresh volumes (phe-rt-*)"
restore --prefix phe-rt-
if restore --prefix phe-rt- >/dev/null 2>&1; then echo "FAIL: restore into non-empty volumes was not refused"; exit 1; fi
echo "ok   restore into volumes that already hold data is refused (needs --force)"
bb sh -c "cp /b/$D/stalwart-etc.tgz /b/stalwart-etc.bak && echo x >> /b/$D/stalwart-etc.tgz"
if restore --prefix phe-rt2- >/dev/null 2>&1; then echo "FAIL: a damaged backup was accepted"; exit 1; fi
bb sh -c "mv /b/stalwart-etc.bak /b/$D/stalwart-etc.tgz"
echo "ok   a damaged backup (checksum mismatch) is refused before any volume is touched"

docker network create phe-rt-net >/dev/null
IMG=$(docker inspect phe-stalwart --format '{{.Config.Image}}')
docker run -d --name phe-rt-stalwart --network phe-rt-net --network-alias stalwart --cpus 1 --memory 1g \
    -v phe-rt-stalwart-data:/var/lib/stalwart -v phe-rt-stalwart-etc:/etc/stalwart "$IMG" >/dev/null
i=0
until [ "$(curlnet phe-rt-net -o /dev/null -w '%{http_code}' -X PROPFIND -H 'Depth: 0' -H "Authorization: Bearer $TOKEN" "http://stalwart:8080/dav/card/$U/default/" 2>/dev/null)" = 207 ]; do
    i=$((i + 1))
    [ $i -gt 40 ] && { echo "FAIL: the restored Stalwart did not answer"; docker logs phe-rt-stalwart 2>&1 | tail -5; exit 1; }
    sleep 1
done
AFTER=$(sum phe-rt-net)
echo "after:  contacts/events/mails = $AFTER (restored Stalwart, same API key)"
[ "$BEFORE" = "$AFTER" ] || { echo "FAIL: the restored hub holds different data ($BEFORE vs $AFTER)"; exit 1; }
echo "ok   restored Stalwart: same contacts, events and messages, and the owner's API key still works"

RUN="docker run --rm -v phe-rt-phecloud-data:/data -e PHECLOUD_DB=/data/phecloud.db -e PHECLOUD_TLSDIR=/data --entrypoint phecloud phe-cloud:dev"
DEV_AFTER=$($RUN devices | wc -l)
FP_AFTER=$($RUN paircode 2>&1 >/dev/null | grep -o '[0-9a-f]\{64\}')
[ "$DEV_BEFORE" = "$DEV_AFTER" ] || { echo "FAIL: devices $DEV_BEFORE vs $DEV_AFTER"; exit 1; }
[ "$FP_BEFORE" = "$FP_AFTER" ] || { echo "FAIL: TLS fingerprint changed"; exit 1; }
echo "ok   restored phecloud state: $DEV_AFTER device rows, same TLS certificate"
docker run --rm --network phe-rt-net --env-file "$HUB_ENV_FILE" -v phe-rt-phecloud-data:/data -e PHECLOUD_DB=/data/phecloud.db --entrypoint phecloud phe-cloud:dev hub status
echo "BACKUP PASS"
