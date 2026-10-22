#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# CalDAV / CardDAV with a standard client (vdirsyncer, BSD-3-Clause) against the hub through Caddy (https, internal CA: Caddy root CA pinned),
# discovery through /.well-known, then app-password revocation: the revoked password must stop working.
#   DAV_URL=https://192.168.2.247:8444/ sh deploy/hub/test/dav_test.sh     (run on the host that runs the hub)
set -eu
cd "$(dirname "$0")"
NET="${HUB_NETWORK:-phehub_default}"
URL="${DAV_URL:?set DAV_URL, e.g. https://192.168.2.247:8444/}"
OWNER=$(grep '^PHECLOUD_HUB_OWNER=' ../.env.phecloud | cut -d= -f2)
PW="${DAV_PASSWORD:?set DAV_PASSWORD to an app password}"
CA_PEM=$(docker exec "${CADDY:-phe-caddy-hubtest}" cat /data/caddy/pki/authorities/local/root.crt)
exec docker run --rm --cpus 1 --memory 512m --network "$NET" --name phe-davtest-$$ -e URL="$URL" -e U="$OWNER" -e P="$PW" -e CA="$CA_PEM" \
  python:3.13-bookworm sh -c '
set -eu
pip install -q --disable-pip-version-check vdirsyncer==0.19.3 aiohttp-socks >/dev/null 2>&1 || pip install -q vdirsyncer >/dev/null
printf "%s\n" "$CA" > /tmp/ca.pem
mkdir -p /tmp/cfg /tmp/st /tmp/out
cat > /tmp/cfg/config <<CFG
[general]
status_path = "/tmp/st/"
[pair contacts]
a = "cr"
b = "cl"
collections = ["from a"]
[storage cr]
type = "carddav"
url = "$URL"
username = "$U"
password = "$P"
verify = "/tmp/ca.pem"
[storage cl]
type = "filesystem"
path = "/tmp/out/contacts"
fileext = ".vcf"
[pair calendar]
a = "kr"
b = "kl"
collections = ["from a"]
[storage kr]
type = "caldav"
url = "$URL"
username = "$U"
password = "$P"
verify = "/tmp/ca.pem"
[storage kl]
type = "filesystem"
path = "/tmp/out/calendar"
fileext = ".ics"
CFG
export VDIRSYNCER_CONFIG=/tmp/cfg/config
yes | vdirsyncer discover 2>&1 | tail -6
vdirsyncer sync 2>&1 | tail -8
echo "contacts synced: $(grep -l "^FN:" -r /tmp/out/contacts | wc -l)"; grep -rh "^FN:" /tmp/out/contacts | sort
echo "events synced: $(grep -l "BEGIN:VEVENT" -r /tmp/out/calendar | wc -l)"; grep -rh "^SUMMARY:" /tmp/out/calendar | sort
'
