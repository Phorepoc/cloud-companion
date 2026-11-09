#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
# Firefox's policies, with the way back to Cloud Companion, then Neko's own start.
set -eu
: "${PHE_BROWSER_SECRET:?}"; : "${PHE_BROWSER_BRIDGE:?}"
chown -R neko:neko /home/neko/.config; chown neko:neko /home/neko/Downloads
# the memory this container may use (its limit, or the machine's), for how many tabs Firefox keeps loaded
MEM=$(cat /sys/fs/cgroup/memory.max 2>/dev/null || echo max)
case $MEM in max|'') MEM=$(awk '/^MemTotal/ {print $2 * 1024}' /proc/meminfo) ;; esac
MEM_MB=$((MEM / 1048576))
cat > /usr/share/firefox-esr/distribution/policies.json <<J
{"policies":{"DisableAppUpdate":true,"DisableTelemetry":true,"DisableFirefoxStudies":true,"DontCheckDefaultBrowser":true,
"OverrideFirstRunPage":"","OverridePostUpdatePage":"","Homepage":{"URL":"about:blank","StartPage":"previous-session"},
"ExtensionSettings":{"bridge@cloud-companion.local":{"installation_mode":"force_installed","install_url":"file:///opt/bridge.xpi"}},
"3rdparty":{"Extensions":{"bridge@cloud-companion.local":{"url":"$PHE_BROWSER_BRIDGE","secret":"$PHE_BROWSER_SECRET","memoryMB":$MEM_MB}}}}}
J
mkdir -p "$XDG_RUNTIME_DIR"; chown neko "$XDG_RUNTIME_DIR"; chmod 700 "$XDG_RUNTIME_DIR"; rm -f /tmp/.X99-lock
# what supervisord did in Neko's image: each part restarts if it stops
as_neko() { setpriv --reuid=neko --regid=neko --init-groups env HOME=/home/neko USER=neko DISPLAY="$DISPLAY" PULSE_SERVER="$PULSE_SERVER" XDG_RUNTIME_DIR="$XDG_RUNTIME_DIR" "$@"; }
keep() { while :; do "$@" >/dev/null 2>&1 || true; sleep 1; done; }
keep as_neko /usr/lib/xorg/Xorg "$DISPLAY" -config /etc/neko/xorg.conf -logfile /tmp/Xorg.log -noreset -nolisten tcp &
sleep 2
keep as_neko pulseaudio --log-level=error --disallow-module-loading --disallow-exit --exit-idle-time=-1 &
keep as_neko openbox &
keep as_neko firefox-esr --kiosk --display="$DISPLAY" &
sleep 1
while :; do as_neko sh -c 'cd /home/neko && exec env -i HOME=/home/neko USER=neko DISPLAY=$DISPLAY PULSE_SERVER=$PULSE_SERVER XDG_RUNTIME_DIR=$XDG_RUNTIME_DIR '"$(env | grep ^NEKO_ | sed "s/.*/'&'/" | tr "\n" " ")"' /usr/bin/neko serve' || true; sleep 1; done
