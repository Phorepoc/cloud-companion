#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# Pulls a compose project's images before it starts, trying again with a growing wait when the registry turns us away (on CI many
# suites pull at once: "toomanyrequests"). Usage: sh web/test/pull.sh docker compose -p x -f ... [services]
for i in 1 2 3 4 5; do
	"$@" pull --ignore-buildable -q 2>/tmp/pull.$$ && { rm -f /tmp/pull.$$; exit 0; }
	grep -qi "toomanyrequests\|rate limit\|timeout\|TLS handshake" /tmp/pull.$$ || { cat /tmp/pull.$$ >&2; rm -f /tmp/pull.$$; exit 1; }
	echo "the registry is busy, trying again in $((i * 20)) s" >&2
	sleep $((i * 20))
done
cat /tmp/pull.$$ >&2; rm -f /tmp/pull.$$; exit 1
