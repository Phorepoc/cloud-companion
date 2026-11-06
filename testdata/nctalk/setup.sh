#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Makes a started Nextcloud container ($1) ready for the Talk tests: waits for its install, adds Talk (spreed, AGPL-3.0, from the
# Nextcloud app store) and the users alice, bob, carol (password pw-<name>-12345) and cc (pw-cc-12345).
set -eu
C=$1
sh "$(dirname "$0")/wait.sh" "$C"
for i in $(seq 1 30); do docker exec -u www-data "$C" php occ app:install spreed >/dev/null 2>&1 && break
	docker exec -u www-data "$C" php occ app:list 2>/dev/null | grep -q "spreed:" && break; sleep 3; done
docker exec -u www-data "$C" php occ app:enable spreed >/dev/null
for u in alice bob carol cc; do docker exec -u www-data -e OC_PASS=pw-$u-12345 "$C" php occ user:add --password-from-env $u >/dev/null; done
