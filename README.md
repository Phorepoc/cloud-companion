# Phorepoc Cloud Companion

Repository: `Phorepoc/cloud-companion`. "Phecloud" was the working name: the server binary, the Docker service and image, and the `PHECLOUD_*` settings keep that name, as does the engine plug-in component (`phewebengine_phecloud`).

Your own small server that does the heavy lifting for old phones and gives you one web app for your things. Self-hosted, one person per instance, Apache-2.0.

- **Cloud browser** for phones whose own browser can no longer open the modern web: the server renders pages in headless Chromium and sends tiles over a small TLS protocol.
- **A web app** (installable PWA) that is also a device: Browser, Chats (SMS through your own phone), Mail, Calendar, Contacts, Connections (pull your other accounts in), Devices, Settings.
- **A personal hub** (optional): mail, calendar and contacts for old phones through Stalwart and Z-Push, so a phone that speaks Exchange ActiveSync, CalDAV or CardDAV just works.
- **Android in the browser** (optional): an Android instance on your server, streamed to a page with touch, keyboard, sound, clipboard and a desktop mode.

Status: working software that is still young. Read "Security" before you put it on the internet.

## Quick start
You need Docker (with the compose plugin) and a few GB of RAM. Nothing else.
```
git clone <this repository> cloud-companion && cd cloud-companion
docker compose up -d --build                      # builds the web app and the server; container phe-cloud
docker exec phe-cloud phecloud paircode --admin   # prints a one-time code such as HFRE-RHAX (valid 10 minutes)
```
Open <http://127.0.0.1:8080>, enter the code: this browser is now your admin device (its key stays in the browser). `localhost` counts as a secure context, so passkeys work there
too: **Settings, Security** adds a passkey and shows a recovery code. Pair a phone with `docker exec phe-cloud phecloud paircode` (no `--admin`) and the same page on the phone, or the
phone app (`docs/phorepoc-phone.md`). `phecloud devices` lists devices, `phecloud revoke <id>` signs one out.

### From other machines (HTTPS)
The web app is plain HTTP on port 8080 and **only listens on 127.0.0.1 by default**. For anything else put a TLS reverse proxy in front (passkeys need `https` and a domain name):
- Easiest, a public name that points at this host: `CADDYFILE=./deploy/Caddyfile PHECLOUD_DOMAIN=cloud.example.org PHECLOUD_HTTPS_PORT=443 PHECLOUD_TRUSTED_PROXIES=172.16.0.0/12 docker compose --profile tls up -d`
  starts Caddy with a Let's Encrypt certificate (the trusted range is Docker's default bridge networks, where Caddy reaches phecloud). For a LAN name leave `CADDYFILE` out: `deploy/Caddyfile.local` uses Caddy's own CA (`tls internal`), install its root on your devices.
- Behind your own proxy (Traefik, nginx, Caddy, Cloudflare Tunnel): set `PHECLOUD_TRUSTED_PROXIES` to the proxy's address so its forwarded headers are believed. Nothing else is trusted
  (the default). Details, tested Traefik configuration and the header rules: [docs/reverse-proxy.md](docs/reverse-proxy.md).
- **Tailscale**: browse to the machine's `*.ts.net` name; either run `tailscale serve --https=443 http://127.0.0.1:8080` on the host (the tailnet gets a real certificate) or use Tailscale
  certificates with Caddy/Traefik (see the reverse-proxy document). A tailnet-only server needs no public ports at all.
- **Cloudflare Tunnel**: `cloudflared` pointing at `http://phecloud:8080`, with `PHECLOUD_TRUSTED_PROXIES` set to the tunnel container's network. WebSockets work as they are.
- `PHECLOUD_WEB_BIND=0.0.0.0` publishes the plain-HTTP port on all interfaces: only on a network you trust.
The **phone port 7400** (TLS 1.3, a pinned self-signed certificate, paired devices only) is published on all interfaces so phones can reach it; `PHECLOUD_PHONE_BIND` narrows it.

### Configuration (environment, see `docker-compose.yml`)
| Variable | Default | Meaning |
|---|---|---|
| `PHECLOUD_DB` | `/data/phecloud.db` | SQLite file, or `postgres://...` / `mysql://...` ([docs/database.md](docs/database.md)) |
| `PHECLOUD_TRUSTED_PROXIES` | empty (nobody) | addresses of your reverse proxy |
| `PHECLOUD_PUBLIC_URL` | derived | the public origin, for OAuth redirects |
| `PHECLOUD_WEB_BIND` / `PHECLOUD_PHONE_BIND` | `127.0.0.1` / `0.0.0.0` | where the two ports are published |
| `PHECLOUD_OAUTH_*` | empty | your own Google / Microsoft app registrations for "Sign in with ..." ([docs/connections.md](docs/connections.md)) |
| `PHECLOUD_ANDROID_SECRET` | empty (feature off) | shared secret of the Android stream ([docs/android.md](docs/android.md)) |
| `PHECLOUD_MAX_PAGE_H` | `8192` | cloud-browser page height cap (px) |
Data (devices, keys, the TLS certificate, the connector key) lives in the `phecloud-data` volume: back it up (`phecloud snapshot`, [docs/hub.md](docs/hub.md)).

## Security
What the defaults do, and what you must still do.
- **Secrets are generated, never defaults:** the device keys at pairing (32 random bytes per device), the TLS certificate and key (created on first start, key mode 0600), the key that seals stored
  account credentials (`/data/connectors.key`, 0600), pairing codes (single use, 10 minutes, rate-limited). No shared password exists. `PHECLOUD_ANDROID_SECRET` and OAuth secrets are yours to set.
- **Listening:** the web app on 127.0.0.1 only; the phone port on all interfaces, TLS 1.3 only, authenticated by HMAC with a per-device key (the key never crosses the wire after pairing). The hub's
  Stalwart is not published; Z-Push's plain-HTTP port is bound to 127.0.0.1. The Android bridge and its Docker proxy are not published; they are reached through phecloud's reverse-proxy route.
- **Web app hardening:** CSP, `X-Frame-Options: DENY`, `nosniff`, `Referrer-Policy: no-referrer`, WebSocket origin check, request header timeouts, per-IP rate limits on pairing and sign-in,
  passkeys (WebAuthn) with a recovery code, signed requests (HMAC over method and path, valid one minute) for every admin API.
- **Android stream:** its WebSocket needs a two-minute ticket (HMAC with the shared secret) that only a signed-in admin device can obtain; app installs need the stream's session id; the bridge
  reaches Docker only through a proxy that allows start / stop / inspect of containers. The Redroid container is `privileged` (Redroid needs it): keep it on an internal network.
- **You must:** use HTTPS for anything off localhost; keep `PHECLOUD_TRUSTED_PROXIES` limited to your proxy; back up the volume; treat the phone port like any internet service if you publish it
  (firewall it to your phones' networks if you can); review the licences of the optional containers (NOTICE).
- Report a vulnerability privately to the maintainers (see SECURITY.md).

## What is where
[docs/status.md](docs/status.md) (what works, the web app's design, measurements, open items) · [docs/reverse-proxy.md](docs/reverse-proxy.md) · [docs/passkeys.md](docs/passkeys.md) ·
[docs/database.md](docs/database.md) · [docs/connections.md](docs/connections.md) (IMAP, CardDAV / CalDAV, Google and Microsoft sign-in, Proton Mail Bridge) · [docs/sms.md](docs/sms.md) ·
[docs/android.md](docs/android.md) · [docs/hub.md](docs/hub.md) · [docs/phorepoc-phone.md](docs/phorepoc-phone.md).
Code: `cmd/phecloud` (server and admin CLI), `cmd/phecloud-client` (test client), `cmd/pheandroid` (Android stream bridge), `internal/` (protocol, store, server, browser, tiles, connectors, web UI backend),
`web/` (the Quasar web app and its browser tests), `client/` (the portable C phone-side core and the Symbian engine / SMS service), `deploy/` (hub, Caddy, Android image).

## Hub (mail, calendar, contacts for old phones; server side of phases P0/P1)
`docker compose --profile hub up -d` (after `sh deploy/hub/hub-init.sh`) adds two AGPL-3.0 servers as separate containers: **Stalwart**
(IMAP, SMTP, CalDAV, CardDAV, JMAP; pinned `v0.16.27`) and **Z-Push 2.7.7** (an Exchange ActiveSync endpoint that reads and writes Stalwart
through IMAP, CalDAV and CardDAV). One instance serves one person: one Stalwart account, and every device gets its own revocable app password
(`phecloud hub apppw add|list|revoke`). Behind Caddy (`--profile tls`, `deploy/hub/Caddyfile.hub`) the endpoints are `/Microsoft-Server-ActiveSync`,
`/dav/...` with the `/.well-known/caldav` and `/.well-known/carddav` redirects, and JMAP. A phone with an old TLS stack cannot use Caddy's TLS 1.2+
listener: use the tunnel (plain HTTP inside it) or the optional TLS 1.0 listener (`--profile hub-legacy`). Tested: 25 of 25 checks (ActiveSync
Provision, FolderSync, Sync of contacts, calendar and mail, SendMail, push; CalDAV/CardDAV with vdirsyncer), about 170 MiB idle.
Choosing the database (SQLite by default, PostgreSQL or MariaDB with `PHECLOUD_DB`): [docs/database.md](docs/database.md). Running behind a reverse proxy (trusted proxies, Traefik, Caddy, nginx, Cloudflare Tunnel, Tailscale certificates): [docs/reverse-proxy.md](docs/reverse-proxy.md). Signing in to the web app with passkeys and a recovery code: [docs/passkeys.md](docs/passkeys.md). Using it from a Phorepoc phone (server start, pairing, fingerprint check): [docs/phorepoc-phone.md](docs/phorepoc-phone.md). Licences, exact phone settings, TLS details, numbers and open issues: [docs/hub.md](docs/hub.md). Outside mail, contacts and calendar accounts (IMAP, CardDAV / CalDAV, Google and Microsoft sign-in, Proton Mail Bridge): [docs/connections.md](docs/connections.md). Text messages through your own phone: [docs/sms.md](docs/sms.md). Android apps in the browser (optional, Redroid + scrcpy, audio, clipboard, app install, parking, desktop-sized displays): [docs/android.md](docs/android.md).

## Tests and CI
`go test ./...` (SQLite), `sh web/test/db_test.sh` (also PostgreSQL and MariaDB), browser suites in `web/test/*.sh` (Playwright in Docker with a virtual authenticator), `make -C client check`.
GitHub Actions runs all of these that do not need Redroid (`.github/workflows/ci.yml`); `release.yml` is a draft that builds the container images and only pushes when told to.
The Android suite (`web/test/android.sh`) needs a Docker host with the `binder_linux` kernel module.

## License
Apache-2.0, see `LICENSE`; third-party components and the separate containers the compose files can start are listed in `NOTICE`.
