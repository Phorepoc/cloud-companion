#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# Waits for a started Nextcloud container ($1) to finish its first install (up to 10 minutes: slow on a busy CI runner). On a timeout it
# says so and prints the container's state and its last log lines, instead of letting the next occ command fail with nothing to go on.
set -eu
C=$1
for i in $(seq 1 300); do
	docker exec -u www-data "$C" php occ status 2>/dev/null | grep -q "installed: true" && exit 0
	sleep 2
done
echo "Nextcloud ($C) was not installed after 10 minutes" >&2
docker inspect -f 'state: {{.State.Status}}, exit {{.State.ExitCode}}, OOM killed: {{.State.OOMKilled}}' "$C" >&2 || true
docker exec -u www-data "$C" php occ status >&2 2>&1 || true
docker logs --tail 40 "$C" >&2 2>&1 || true
exit 1
