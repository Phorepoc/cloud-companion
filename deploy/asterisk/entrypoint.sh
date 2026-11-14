#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# First start: write the SIP accounts (Cloud Companion and two phones) and the message routing, then run Asterisk in the foreground.
set -eu
C=/etc/asterisk
T=/usr/share/phecloud-asterisk
if ! grep -q "phecloud-asterisk" "$C/pjsip.conf" 2>/dev/null; then
	: "${PHECLOUD_ASTERISK_SECRET:?set PHECLOUD_ASTERISK_SECRET: the password Cloud Companion signs in to Asterisk with}"
	gen() { head -c 12 /dev/urandom | od -An -tx1 | tr -d ' \n'; }
	P201=${PHECLOUD_ASTERISK_PHONE_201:-$(gen)}
	P202=${PHECLOUD_ASTERISK_PHONE_202:-$(gen)}
	EXT=""
	if [ -n "${PHECLOUD_ASTERISK_PUBLIC:-}" ]; then # behind Docker's NAT: the address phones reach this machine at (calls need it)
		EXT="external_media_address=$PHECLOUD_ASTERISK_PUBLIC
external_signaling_address=$PHECLOUD_ASTERISK_PUBLIC
local_net=172.16.0.0/12
local_net=10.0.0.0/8
local_net=192.168.0.0/16"
	fi
	awk -v cc="$PHECLOUD_ASTERISK_SECRET" -v p201="$P201" -v p202="$P202" -v ext="$EXT" \
		'{ gsub(/@CC_SECRET@/, cc); gsub(/@P201@/, p201); gsub(/@P202@/, p202); if ($0 == "@EXTERNAL@") $0 = ext; print }' \
		"$T/pjsip.conf.in" > "$C/pjsip.conf"
	cp "$T/extensions.conf" "$T/rtp.conf" "$C/"
	chmod 640 "$C/pjsip.conf"
	echo "phecloud-asterisk: configuration written to $C; the phones' passwords are in $C/pjsip.conf (password=)"
fi
chown -R asterisk:asterisk "$C" /var/lib/asterisk /var/spool/asterisk /var/log/asterisk /var/run/asterisk 2>/dev/null || true
exec asterisk -f -U asterisk -G asterisk -vvv
