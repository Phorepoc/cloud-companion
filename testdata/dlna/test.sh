#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# The DLNA media server with a real client (repo root): Cloud Companion with PHECLOUD_DLNA_LISTEN on a Docker network, a song and
# a PDF in its files, and async-upnp-client (Home Assistant's UPnP library) next to it finding it by multicast and browsing it.
set -eu
DC="docker compose -p phedlna -f docker-compose.yml -f docker-compose.build.yml -f web/test/compose.passkey.yml -f testdata/dlna/compose.yml"
$DC down -v >/dev/null 2>&1 || true
$DC up -d --build phecloud > /dev/null 2>&1 || { $DC logs 2>&1 | tail -20; exit 1; }
trap '$DC down -v >/dev/null 2>&1' EXIT
T=$(mktemp -d); mkdir -p "$T/Music/Album"; printf 'ID3-0123456789' > "$T/Music/Album/01 Sea.mp3"; printf '%%PDF' > "$T/taxes.pdf"
docker exec -u 0 phe-cloud-pk mkdir -p /data/files
docker cp "$T/." phe-cloud-pk:/data/files/ && rm -rf "$T"
docker exec -u 0 phe-cloud-pk chown -R "$(docker exec phe-cloud-pk id -u):$(docker exec phe-cloud-pk id -g)" /data/files 2>/dev/null || true
docker run --rm --name phe-dlna-client --network phedlna_default -v "$(pwd)/testdata/dlna":/t python:3.13-slim \
	sh -c 'pip install -q --root-user-action=ignore --disable-pip-version-check async-upnp-client==0.49.0 && python /t/client.py' \
	|| { docker logs phe-cloud-pk 2>&1 | tail -10; exit 1; }
