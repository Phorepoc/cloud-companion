#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Hub test: run test_hub.py in a python container on the hub compose network (default project name phehub).
#   sh deploy/hub/test/run.sh [seed|eas|all]     needs a running hub (deploy/hub/hub-init.sh); EAS_PASSWORD = an app password (optional)
set -eu
cd "$(dirname "$0")"
NET="${HUB_NETWORK:-phehub_default}"
OWNER=$(grep '^PHECLOUD_HUB_OWNER=' ../.env.phecloud | cut -d= -f2)
OPW=$(grep '^PHECLOUD_HUB_OWNER_PASSWORD=' ../.env.hub | cut -d= -f2)
exec docker run --rm --cpus 1 --memory 512m --network "$NET" --name phe-hubtest-$$ \
  -e HUB_USER="$OWNER" -e HUB_PASSWORD="$OPW" -e EAS_PASSWORD="${EAS_PASSWORD:-$OPW}" \
  -e EAS_HOST="${EAS_HOST:-zpush}" -e EAS_PORT="${EAS_PORT:-80}" -e EAS_TLS="${EAS_TLS:-0}" -e EAS_VER="${EAS_VER:-12.1}" -e EAS_HOSTHEADER="${EAS_HOSTHEADER:-}" \
  -v "$PWD:/t:ro" -w /t python:3.13-bookworm python -B test_hub.py "${1:-all}"
