# Host manager and board images

Status: design, for review. Built: the manager's features (`cmd/phehost`, `internal/host`, `deploy/features`), the Features page, the setup and recovery page with the Wi-Fi step and setup hotspot, the install script and the Raspberry Pi image (`deploy/board`, `deploy/images/pi`). Not yet: updates, remote access.

## Why

Running Cloud Companion today means Docker, compose files, profiles and environment variables. The goal: flash an SD card,
power the board on, open a page on the phone or laptop, answer a few questions, and use it. Features (messaging, the
browser, files...) are switched on and off in Cloud Companion, not in compose files. People who run their own services keep
connecting to them as today.

## Boards

| board | when | notes |
|---|---|---|
| Raspberry Pi 5 (4/8/16 GB) | first | the browser feature needs 8 GB |
| Raspberry Pi 4 (2/4/8 GB) | first | 2 GB: core features only, no browser |
| Pine64 Rock64 (RK3328, up to 4 GB) | later | Debian/Armbian base, no browser |
| any Debian/Ubuntu machine (x86_64, arm64) | later | an install script instead of an image |

Each feature states what it needs (memory, CPU architecture); the setup and the Features page only offer what the board can
run.

## Pieces

| piece | where | what |
|---|---|---|
| image | SD card | Raspberry Pi OS Lite 64-bit (Debian), Docker, the manager; nothing else. Built in CI with `pi-gen` |
| manager | host, systemd service (Go) | the only thing that touches Docker and the OS: features, updates, health, backups; its own web UI for setup and recovery |
| Cloud Companion | container | asks the manager through a local socket; shows Features and System pages to the owner |
| features | containers | Cloud Companion's optional parts, each a manifest in the catalogue (deploy/features), updated with the board files |

Cloud Companion never gets the Docker socket: a break-in to it must not mean root on the board.

## First boot

1. The image is flashed. Raspberry Pi Imager can write Wi-Fi details; we add an optional `cloud-setup.txt` on the boot
   partition with a setup code.
2. The board boots, grows its file system and starts the manager. It announces itself as `cloud.local` (mDNS).
3. No network (no cable, no Wi-Fi details): the board opens its own Wi-Fi, "Cloud setup", with a page to pick a network.
4. The manager's setup UI on port 80 asks for the setup code (from the boot partition; without one, the first visitor within
   30 minutes of boot may set up, and the window closes after).
5. Steps: network, a **recovery passphrase** (and a warning when the board runs from an SD card),
   features to start with, time zone. Then download and start, with progress per image and retry.
6. When Cloud Companion is healthy, the setup UI sends the browser there (the owner is already signed in) and switches
   itself off.

## Installing

- **Raspberry Pi 4/5:** flash `cloud-companion-raspberry-pi-vX.Y.Z.img.xz` from the release page (Raspberry Pi Imager can add
  Wi-Fi details). For a setup code, rename `cloud-setup.txt.example` on the card's boot partition to `cloud-setup.txt` and
  change the code. The image has no login: the board is run from the setup page and Cloud Companion.
- **Any Debian or Ubuntu machine (arm64, amd64):** `curl -fsSL https://github.com/phorepoc/cloud-companion/releases/latest/download/install.sh | sudo sh`,
  then open `http://<machine>/`. Without `--board` it leaves the machine's name and network alone (no hotspot).

`deploy/board/bundle.sh` makes the release's board files (the manager for both architectures, `cloud-board.tar.gz` with the
compose files, `install.sh`, `SHA256SUMS.board`); `deploy/images/pi/build.sh` makes the image with `pi-gen` (pinned tag) and
the same `install.sh --board`. The release workflow attaches both only when the release changed `cmd/phehost`,
`internal/host`, `deploy/board`, `deploy/features`, `deploy/images` or the compose files; `install.sh` takes the newest
release that has them. Cloud Companion's image is built for amd64 and arm64.

### SD cards

Booting from a USB SSD (Raspberry Pi 4 and 5) or NVMe (Pi 5) is recommended: flash the same image onto it. SD cards wear
out under a server's small constant writes, so the setup page warns when the board runs from one (`board.storage` in
`/v1/system`: `sd`, `usb`, `nvme`). For boards that stay on a card, `install.sh --board` spares it:

- swap is compressed memory (`systemd-zram-generator`, zstd, up to half the memory), not a file on the card; `/tmp` is in
  memory;
- `/var/log` is a 128 MB RAM disk (`cloud-ram-logs`), copied to the card every hour and at shutdown; only on an SD card
  (`ALWAYS=1` in `/etc/default/cloud-ram-logs` for other disks, `SIZE` for its size; the hour is a timer drop-in);
- the containers' logs go to the journal (Docker's `journald` driver; `docker logs` still works), capped at 48 MB.

Cloud Companion's data (database, mail, files) is still written to the card; only booting from USB avoids that. Moving it to
a USB disk plugged in later is left until someone needs it.

### The setup hotspot

On a board (`-wifi wlan0`), when there is no network 90 seconds after boot, the manager opens an open Wi-Fi called "Cloud
setup" (NetworkManager, shared mode, the page at `10.42.0.1`; every DNS name points there, so phones offer it as the
network's sign-in page). After the setup code, the page lists the Wi-Fi networks it saw before the hotspot started; joining
one closes the hotspot, and the page asks to move to that network and open `http://cloud.local`. A failed join reopens the
hotspot with the reason. Setup itself needs the internet and refuses to start without it.

## Backups

Settings, Backups (owner only) turns on nightly backups of everything: every volume of the compose project (Cloud Companion's
database and files, mail, chats, the features' data) and the manager's state (the features, their settings and secrets,
the recovery passphrase). The engine is [restic](https://restic.net), run as a container (`restic/restic`), so nothing is
installed on the board for it.

- **Where:** a USB disk plugged into the board (any partition with ext4, exFAT, FAT, NTFS, Btrfs or XFS; mounted only for the
  run, never formatted), an SFTP server (the board makes its own key and shows it to add to `authorized_keys`), S3-compatible
  storage, or a restic REST server. A Windows or NAS share (SMB) is not offered yet.
- **Encrypted** with the owner's backup password (at least 10 characters), kept on the board (root only) for the nightly
  runs. Without it nobody can read the backups, which the page says.
- **When:** once a day at a chosen time (03:30 by default, the board's time zone). The project's containers stop for the copy
  (`docker stop`, a minute or two), so no database is copied mid-write, and start again however the copy went.
- **Kept:** 7 daily, 4 weekly and 6 monthly backups (`restic forget --prune` after each run); on Sundays `restic check`.
- **Restore:** on a fresh board, the setup page offers "Restore from a backup" next to setting up: the destination, the
  password, then a backup from the list. The volumes are made again with their compose labels, the state and settings come
  back, the features start, and the owner's devices work as before, without pairing. Backups go on to the same place.

The API: `GET /v1/backup` (without secrets, plus the SFTP key), `PUT /v1/backup` (checks the destination, makes the restic
repository there if it is new; a job), `POST /v1/backup/run` (a job), `GET /v1/backup/snapshots`, `GET /v1/backup/usb`.

## Recovery

The manager checks Cloud Companion's health. When it stays unhealthy for 5 minutes (crash loop, failed update that could not
roll back, disk full, database will not open), the manager's UI comes back on port 80, behind the recovery passphrase:

- roll back to the last working version; retry the update
- free disk space (old images, logs)
- logs of each container
- restore a backup
- reboot, factory reset

When Cloud Companion is healthy again, the UI switches off again. The owner can also turn it on from Cloud Companion.

## Features

A feature is a manifest in the catalogue (`deploy/features/*.yaml`, one file each). All features run in one compose project
(`cloud`) from the compose files this repository already has: a feature names the profiles (and extra compose files) it switches
on, so the compose files stay the one description of the containers:

```yaml
id: postgres
title: PostgreSQL database
about: "Keeps Cloud Companion's data in PostgreSQL instead of the built-in SQLite file."
files: [docker-compose.db.yml]   # beside docker-compose.yml
profiles: [db-postgres]
conflicts: [mariadb]
requires: []                     # other features, switched on with it
needs: { memory: 256M }          # and arch: [amd64] where it only runs there
settings:                        # asked when switched on (generate: made by the manager, never asked; secret: never shown back)
  - { key: password, label: Database password, env: PHECLOUD_DB_PASSWORD, generate: true, secret: true }
env:                             # more variables for the compose project, from the settings
  PHECLOUD_DB: postgres://phecloud:${PHECLOUD_DB_PASSWORD}@postgres:5432/phecloud?sslmode=disable
```

The manager writes the settings to the project's `.env` and runs `docker compose -p cloud -f ... --profile ... up -d --wait`;
the containers of features switched off are removed (their volumes stay). A change that fails puts the previous state back.
"Use mine" (a person's own service instead of ours) is a setting of Cloud Companion's, as today in Connections.

The first catalogue is what the compose profiles do today: core, database (SQLite by default, Postgres), TLS proxy, hub
(mail, contacts, calendar), SIP, Matrix/XMPP/IRC bridges, Signal, Telegram, files, the browser.

## Updates

Updates come only from this project's GitHub releases, over HTTPS (`internal/host/update.go`). They are not signed: HTTPS
already stops anyone on the way changing them, and a signing key kept as a GitHub secret would fall with the GitHub account
anyway. What protects the boards is that account (two-factor login, branch protection, few people with write access).

- **What a release carries:** images, every time; board files (the manager, the compose files, `deploy/`) and the Pi image
  only when they changed. `bundle.sh` writes the release's `VERSION` into the board files.
- **Looking:** once a day the manager reads the release list and keeps the newest release (and the newest board files up to
  it) in `update.json`. Drafts, pre-releases and anything not newer than what runs are ignored: a board never goes back.
- **Installing:** at the backup time (03:30 when backups are off), when *Install updates at night* is on (the default), or
  with *Update now* in Settings, Updates:
  1. a backup, when backups are on; if it fails nothing is changed;
  2. new board files, when there are any: downloaded, checked against `SHA256SUMS.board` (a broken download is refused),
     the current ones kept in `StateDir/previous`, the new ones unpacked over `/opt/cloud` (files written on the board stay);
  3. the release's images (`PHECLOUD_VERSION` in `.env`), started with compose;
  4. up to ten minutes for Cloud Companion to be healthy. If it is not, the previous files, manager and images come back and
     the page says the update did not work and nothing was lost. The update is offered again.
  A new manager takes over by the old one exiting; systemd starts the new one.
- **The system:** Debian's security updates install by themselves (`unattended-upgrades`); the board never restarts on its own.

## Access from outside the home

Since the manager controls the whole board, remote access is a setting, not a manual job. Offered in setup (an optional step
after features) and in Cloud Companion (System, "Away from home"):

- **Tailscale:** sign in with a Tailscale account (the login link opens on the phone); the board joins the person's tailnet
  and Cloud Companion is at its tailnet name, with a TLS certificate from Tailscale. Easiest: no router changes.
- **WireGuard:** the board runs the server; the manager makes a key and a QR code / config file per device (phone, laptop),
  lists them and revokes them. Needs one UDP port forwarded on the router: the page says which, checks it from outside where it
  can, and works with a dynamic DNS name if the person has one.
- **Off** (default): reachable at home only.

Both run on the host (Tailscale's and the kernel's WireGuard), managed by the manager; the phone app and Symbian clients use the
address the tunnel gives. Pairing a new device can include its WireGuard config.

## The manager's API

A Unix socket mounted into the Cloud Companion container only; JSON over HTTP; the owner's requests only (Cloud Companion
checks who asks). Versioned (`/v1/`).

| call | what |
|---|---|
| `GET /v1/system` | board, memory, disk, temperature, versions, pending reboot |
| `GET /v1/features` | the catalogue with each feature's state and whether this board can run it |
| `POST /v1/features/{id}` | switch on, with settings; returns a job |
| `DELETE /v1/features/{id}` | switch off (keep data, or delete it) |
| `GET /v1/jobs/{id}` | progress of a job (stream) |
| `GET /v1/logs/{service}` | logs (stream) |
| `GET/PUT /v1/updates` | what is available, the owner's choices; `POST /v1/updates/apply` |
| `POST /v1/backups`, `GET /v1/backups`, `POST /v1/backups/{id}/restore` | backups to the SSD or a network share |
| `GET/PUT /v1/remote` | remote access: off, Tailscale (login link, state), WireGuard (port, peers) |
| `POST /v1/remote/wireguard/peers`, `DELETE .../{id}` | a device's WireGuard config (QR code), revoke |
| `POST /v1/reboot` | reboot |

## In Cloud Companion

Settings gets two owner-only pages:

- **Features:** each feature with a switch, what it does, what it needs, its settings, "run it here" or "use mine".
  Switching shows the job's progress.
- **System:** storage, updates, backups, away from home (Tailscale, WireGuard), the recovery passphrase, reboot.

## Order of work

1. The manager on a plain Debian machine: catalogue, features as compose projects, the API, health and rollback.
2. The Features and System pages in Cloud Companion.
3. The setup and recovery UI.
4. The Raspberry Pi 4/5 image (`pi-gen` in CI), first boot, the setup hotspot.
5. Updates from the GitHub releases, with rollback.
6. Away from home: Tailscale and WireGuard in setup and System.
7. Rock64, then the install script for other machines.

## Where it lives

In this repository (`cmd/phehost`, `internal/host`, the images under `deploy/images`), released with Cloud Companion on the
same release pages. A release carries a new manager binary and new board images only when something of theirs changed
(Updates).

## Open questions

- Backups to an SMB share, or to another Cloud Companion.
