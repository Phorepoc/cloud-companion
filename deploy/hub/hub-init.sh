#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# One-time setup of the personal hub: starts Stalwart in bootstrap mode, completes the bootstrap (host name, mail domain), creates
# the single owner account and raises the app-password limit. Writes deploy/hub/.env.hub (mode 600) with every secret.
# Safe to re-run: it stops when .env.hub already says the hub was initialised.
#   HUB_DOMAIN=hub.lan HUB_OWNER=owner sh deploy/hub/hub-init.sh
set -eu
cd "$(dirname "$0")/../.."
ENVF=deploy/hub/.env.hub
DOMAIN="${HUB_DOMAIN:-${PHECLOUD_DOMAIN:-hub.lan}}"
OWNER="${HUB_OWNER:-owner}"
CLI_IMAGE="${STALWART_CLI_IMAGE:-stalwartlabs/cli:latest}"
rand() { head -c 24 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c "${1:-20}"; }
if [ -f "$ENVF" ] && grep -q '^HUB_INITIALISED=1' "$ENVF"; then echo "already initialised ($ENVF)"; exit 0; fi
umask 077
if [ ! -f "$ENVF" ]; then
  printf 'STALWART_RECOVERY_ADMIN=admin:%s\n' "$(rand 24)" > "$ENVF"
  cp "$ENVF" deploy/hub/.env.stalwart
fi
. "./$ENVF"
BOOT_PW="${STALWART_RECOVERY_ADMIN#admin:}"
dc() { docker compose --profile hub "$@"; }
cli() { docker run --rm -i --network container:phe-stalwart -e STALWART_URL=http://127.0.0.1:8080 \
  -e STALWART_USER="$U" -e STALWART_PASSWORD="$P" "$CLI_IMAGE" "$@"; }
dc up -d stalwart
echo "waiting for Stalwart bootstrap mode ..."
i=0; until docker run --rm --network container:phe-stalwart "$CLI_IMAGE" --url http://127.0.0.1:8080 --user admin --password "$BOOT_PW" describe >/dev/null 2>&1; do
  i=$((i+1)); [ $i -gt 60 ] && { echo "Stalwart did not come up"; exit 1; }; sleep 1; done
U=admin P="$BOOT_PW"
OUT=$(cli update Bootstrap --field serverHostname="$DOMAIN" --field defaultDomain="$DOMAIN" \
  --field requestTlsCertificate=false --field generateDkimKeys=false)
echo "$OUT" | sed 's/secret: .*/secret: <hidden, saved in .env.hub>/'
ADMIN_USER=$(echo "$OUT" | sed -n 's/.*username: "\(.*\)"/\1/p')
ADMIN_PW=$(echo "$OUT" | sed -n 's/.*secret: "\(.*\)"/\1/p')
[ -n "$ADMIN_PW" ] || { echo "no permanent admin secret in the bootstrap reply"; exit 1; }
docker restart phe-stalwart >/dev/null
echo "waiting for Stalwart (configured) ..."
U="$ADMIN_USER" P="$ADMIN_PW"
i=0; until cli describe >/dev/null 2>&1; do i=$((i+1)); [ $i -gt 60 ] && { echo "Stalwart did not restart"; exit 1; }; sleep 1; done
DOMAIN_ID=$(cli query Domain --json | sed -n 's/.*"id":"\([^"]*\)".*/\1/p' | head -1)
OWNER_PW=$(rand 24)
cli create Account/User --json "{\"name\":\"$OWNER\",\"domainId\":\"$DOMAIN_ID\",\"description\":\"Owner\",\"credentials\":{\"0\":{\"@type\":\"Password\",\"secret\":\"$OWNER_PW\"}}}"
cli update Authentication --field maxAppPasswords=50 >/dev/null
{
  echo "STALWART_ADMIN_USER=$ADMIN_USER"
  echo "STALWART_ADMIN_PASSWORD=$ADMIN_PW"
  echo "HUB_INITIALISED=1"
} >> "$ENVF"
{
  echo "PHECLOUD_HUB_URL=http://stalwart:8080"
  echo "PHECLOUD_HUB_DOMAIN=$DOMAIN"
  echo "PHECLOUD_HUB_OWNER=$OWNER@$DOMAIN"
  echo "PHECLOUD_HUB_OWNER_PASSWORD=$OWNER_PW"
} > deploy/hub/.env.phecloud
dc up -d --build zpush
echo "hub ready: owner $OWNER@$DOMAIN (secrets in $ENVF). Next: phecloud hub apppw add 'my phone'"
