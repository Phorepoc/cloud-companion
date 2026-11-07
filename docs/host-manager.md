# Host manager and board images

Status: design, for review. Built: the manager's features (`cmd/phehost`, `internal/host`, `deploy/features`).

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
| features | containers | Cloud Companion's optional parts, each a manifest in a signed catalogue |

Cloud Companion never gets the Docker socket: a break-in to it must not mean root on the board.

## First boot

1. The image is flashed. Raspberry Pi Imager can write Wi-Fi details; we add an optional `cloud-setup.txt` on the boot
   partition with a setup code.
2. The board boots, grows its file system and starts the manager. It announces itself as `cloud.local` (mDNS).
3. No network (no cable, no Wi-Fi details): the board opens its own Wi-Fi, "Cloud setup", with a page to pick a network.
4. The manager's setup UI on port 80 asks for the setup code (from the boot partition; without one, the first visitor within
   30 minutes of boot may set up, and the window closes after).
5. Steps: network, storage (USB SSD if present: recommended, SD cards wear out), owner name and a **recovery passphrase**,
   features to start with, time zone. Then download and start, with progress per image and retry.
6. When Cloud Companion is healthy, the setup UI sends the browser there (the owner is already signed in) and switches
   itself off.

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

- **Release channel:** a signed index (one per channel: stable, beta) listing tested versions of Cloud Companion and every
  feature, by image digest. The manager checks it daily.
- **Containers:** pull, start the new version, health check, then remove the old; on a failed check, back to the old version
  and the owner is told. Database migrations take a backup first.
- **Host:** Debian security updates install automatically; updates that need a reboot wait for the owner's reboot window and
  show in Cloud Companion. The manager updates itself through the channel too (a new binary, restart, rollback on failure).
- **Owner choices:** automatic (default) or ask first, the reboot window, the channel.

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
5. The signed release channel and host updates.
6. Away from home: Tailscale and WireGuard in setup and System.
7. Rock64, then the install script for other machines.

## Where it lives

In this repository (`cmd/phehost`, `internal/host`, the images under `deploy/images`), released with Cloud Companion on the
same release pages. A release carries a new manager binary and new board images only when something of theirs changed; the
release channel says which manager version each Cloud Companion version needs.

## Open questions

- Backups off the board: a network share, or another Cloud Companion.
