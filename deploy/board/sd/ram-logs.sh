#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# Logs in RAM on a board that runs from an SD card (deploy/board/install.sh --board): /var/log becomes a RAM disk, copied to
# the card by cloud-ram-logs-sync.timer (hourly) and at shutdown, so the card is not written for every log line.
#   ram-logs.sh start | sync | stop
# Settings in /etc/default/cloud-ram-logs: SIZE (128M), ALWAYS=1 also on USB and NVMe disks.
set -eu
SIZE=128M ALWAYS=
[ -r /etc/default/cloud-ram-logs ] && . /etc/default/cloud-ram-logs
DISK=/var/log.disk
on_ram() { [ "$(findmnt -no FSTYPE /var/log 2>/dev/null)" = tmpfs ]; }
case ${1:-} in
start)
  on_ram && exit 0
  case $(findmnt -no SOURCE /) in /dev/mmcblk*) ;; *) [ -n "$ALWAYS" ] || { echo "not on an SD card: logs stay on disk"; exit 0; } ;; esac
  mkdir -p $DISK
  mount --bind /var/log $DISK
  mount -t tmpfs -o size=$SIZE,mode=0755,nosuid,nodev cloud-logs /var/log
  # the journal can be bigger than the RAM disk: it starts empty there and keeps its old files on the card
  rsync -a --exclude=/journal/ $DISK/ /var/log/ || true
  mkdir -p /var/log/journal
  echo "logs in RAM ($SIZE)"
  ;;
sync)
  on_ram || exit 0
  journalctl --sync 2>/dev/null || true
  rsync -a --inplace /var/log/ $DISK/
  ;;
stop)
  on_ram || exit 0
  "$0" sync
  umount -l /var/log
  umount -l $DISK
  ;;
*) echo "usage: $0 start|sync|stop" >&2; exit 2 ;;
esac
