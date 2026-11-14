#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# Installs Cloud Companion's host manager (docs/host-manager.md) on a Debian or Ubuntu machine (arm64 or amd64), as root:
# Docker with compose, the manager as a systemd service, and Cloud Companion's compose files in /opt/cloud. Cloud Companion
# itself is downloaded when it is set up, on the setup page (http://<this machine>/).
#
#   curl -fsSL https://github.com/phorepoc/cloud-companion/releases/latest/download/install.sh | sudo sh
#
#   --version vX.Y.Z  a release with board files (default: the newest one that has them)
#   --from DIR        phehost-linux-<arch> and cloud-board.tar.gz from DIR instead of the release (tests, the image build)
#   --board           a machine that is only Cloud Companion (the Raspberry Pi image): named cloud-companion.local, NetworkManager's setup
#                     hotspot; never on a machine you use for other things
#   --no-start        install only (an image being built)
set -eu
REPO=phorepoc/cloud-companion
VERSION= FROM= BOARD= START=1
while [ $# -gt 0 ]; do
  case $1 in
    --version) VERSION=$2; shift ;;
    --from) FROM=$2; shift ;;
    --board) BOARD=1 ;;
    --no-start) START= ;;
    *) echo "install.sh: unknown option $1" >&2; exit 2 ;;
  esac
  shift
done
[ "$(id -u)" = 0 ] || { echo "install.sh: run as root (sudo)" >&2; exit 1; }
case $(dpkg --print-architecture) in
  arm64) ARCH=arm64 ;; amd64) ARCH=amd64 ;;
  *) echo "install.sh: Cloud Companion runs on 64-bit ARM or x86 only" >&2; exit 1 ;;
esac
. /etc/os-release
say() { echo "== $*"; }

export DEBIAN_FRONTEND=noninteractive
say "packages"
apt-get update -q
apt-get install -y -q --no-install-recommends ca-certificates curl gnupg avahi-daemon >/dev/null

if ! docker compose version >/dev/null 2>&1; then
  say "Docker"
  DIST=$ID; [ "$ID" = raspbian ] && DIST=debian
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL "https://download.docker.com/linux/$DIST/gpg" -o /etc/apt/keyrings/docker.asc
  echo "deb [arch=$ARCH signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/$DIST $VERSION_CODENAME stable" \
    > /etc/apt/sources.list.d/docker.list
  apt-get update -q
  apt-get install -y -q docker-ce docker-ce-cli containerd.io docker-compose-plugin >/dev/null
fi

W=$(mktemp -d); trap 'rm -rf "$W"' EXIT
if [ -n "$FROM" ]; then
  cp "$FROM/phehost-linux-$ARCH" "$FROM/cloud-board.tar.gz" "$W/"
else
  if [ -z "$VERSION" ]; then
    # the newest release that carries board files: releases without changes to them have none
    VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases?per_page=30" \
      | tr ',' '\n' | sed -n 's|.*"browser_download_url": *".*/download/\([^/"]*\)/cloud-board.tar.gz".*|\1|p' | head -1)
    [ -n "$VERSION" ] || { echo "install.sh: no release with board files found" >&2; exit 1; }
  fi
  say "Cloud Companion host manager $VERSION"
  B="https://github.com/$REPO/releases/download/$VERSION"
  for f in "phehost-linux-$ARCH" cloud-board.tar.gz SHA256SUMS.board; do curl -fsSL -o "$W/$f" "$B/$f"; done
  (cd "$W" && grep -E " (phehost-linux-$ARCH|cloud-board.tar.gz)\$" SHA256SUMS.board | sha256sum -c --quiet -)
fi

say "files"
install -m 0755 "$W/phehost-linux-$ARCH" /usr/local/bin/phehost
mkdir -p /opt/cloud
tar -xzf "$W/cloud-board.tar.gz" -C /opt/cloud --no-same-owner
install -m 0644 /opt/cloud/deploy/board/phehost.service /etc/systemd/system/phehost.service

if [ -n "$BOARD" ]; then
  say "board"
  # written in place: in the image build's chroot hostnamectl has no systemd, and /etc/hosts may be a mount
  echo cloud-companion > /etc/hostname
  { grep -v '^127\.0\.1\.1' /etc/hosts; printf '127.0.1.1\tcloud-companion\n'; } > "$W/hosts" && cat "$W/hosts" > /etc/hosts
  hostname cloud-companion 2>/dev/null || true
  apt-get install -y -q --no-install-recommends network-manager >/dev/null
  mkdir -p /etc/NetworkManager/dnsmasq-shared.d
  install -m 0644 /opt/cloud/deploy/board/dnsmasq-cloud-setup.conf /etc/NetworkManager/dnsmasq-shared.d/cloud-setup.conf
  # sparing the SD card: compressed swap in RAM, /tmp in RAM, logs in RAM (synced hourly; only when running from a card),
  # the containers' logs in the journal
  S=/opt/cloud/deploy/board/sd
  apt-get install -y -q --no-install-recommends systemd-zram-generator rsync >/dev/null
  # Debian's security updates by themselves; never a restart on its own (docs/host-manager.md, Updates)
  apt-get install -y -q --no-install-recommends unattended-upgrades >/dev/null
  printf 'APT::Periodic::Update-Package-Lists "1";\nAPT::Periodic::Unattended-Upgrade "1";\n' > /etc/apt/apt.conf.d/20auto-upgrades
  install -m 0644 $S/zram-generator.conf /etc/systemd/zram-generator.conf
  for u in dphys-swapfile rpi-swap; do systemctl disable "$u" >/dev/null 2>&1 || true; done
  systemctl unmask tmp.mount >/dev/null 2>&1 || true
  systemctl enable tmp.mount >/dev/null 2>&1 || true
  install -m 0755 $S/ram-logs.sh /usr/local/sbin/cloud-ram-logs
  install -m 0644 $S/cloud-ram-logs.service $S/cloud-ram-logs-sync.service $S/cloud-ram-logs-sync.timer /etc/systemd/system/
  mkdir -p /etc/systemd/journald.conf.d && install -m 0644 $S/journald.conf /etc/systemd/journald.conf.d/cloud.conf
  [ -e /etc/docker/daemon.json ] || { mkdir -p /etc/docker; install -m 0644 $S/docker-daemon.json /etc/docker/daemon.json; }
  systemctl enable cloud-ram-logs.service cloud-ram-logs-sync.timer >/dev/null 2>&1 || true
  mkdir -p /etc/systemd/system/phehost.service.d   # the setup hotspot: only on a board
  printf '[Service]\nExecStart=\nExecStart=/usr/local/bin/phehost -compose /opt/cloud -state /var/lib/phehost -socket /run/phehost/api.sock -binary /usr/local/bin/phehost -ssh-user cloud -wifi wlan0\n' \
    > /etc/systemd/system/phehost.service.d/board.conf
fi

systemctl enable docker avahi-daemon phehost >/dev/null 2>&1 || true
if [ -n "$START" ]; then
  systemctl daemon-reload
  systemctl restart phehost
  say "done: open http://$(hostname).local/ (or this machine's address) to set up Cloud Companion"
else
  say "installed; phehost starts at boot"
fi
