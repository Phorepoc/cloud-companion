#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Snapshot the running phecloud database before an update. Every call writes a NEW timestamped file (never overwrites) and keeps the newest KEEP (default 10).
#   sh deploy/hub/snapshot.sh [backup-dir] [container]        defaults: ./backups, phe-cloud
set -eu
DIR=${1:-./backups}
C=${2:-phe-cloud}
KEEP=${KEEP:-10}
mkdir -p "$DIR"
F="$DIR/phehub-$(date +%Y%m%d-%H%M%S).db"
[ ! -e "$F" ] || { echo "snapshot $F exists" >&2; exit 1; }
docker exec "$C" phecloud snapshot /tmp/snap.db
docker cp "$C:/tmp/snap.db" "$F"
docker exec "$C" rm -f /tmp/snap.db
# keep the newest $KEEP (names sort by time)
ls -1 "$DIR"/phehub-*.db 2>/dev/null | sort | head -n -"$KEEP" | while read -r old; do rm -f "$old"; done
echo "$F"
