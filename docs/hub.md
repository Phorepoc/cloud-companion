# Hub: mail, calendar and contacts for old phones (phases P0 and P1, server side)

One person, one instance. The hub is a compose profile (`hub`) that runs two third-party servers as separate containers, next to
phecloud:

| Container | What | Version (pinned) | Licence |
|---|---|---|---|
| `phe-stalwart` | Stalwart: IMAP, SMTP, CalDAV, CardDAV, JMAP, web admin | `stalwartlabs/stalwart:v0.16.27` | AGPL-3.0 (dual-licensed with Stalwart's proprietary Enterprise licence; we run the AGPL build, unmodified) |
| `phe-zpush` | Z-Push: an Exchange ActiveSync (EAS) endpoint that talks IMAP + CalDAV + CardDAV to Stalwart ("combined" backend) | Z-Push 2.7.7 (tarball SHA-256 pinned in the Dockerfile) on `php:8.3-apache-bookworm` | AGPL-3.0 |
| `phe-caddy` | TLS in front (existing `tls` profile) | `caddy:2` | Apache-2.0 |
| `phe-legacytls` | optional TLS 1.0 to 1.2 listener for old phones (profile `hub-legacy`) | `nginx:1.28-bookworm` | BSD-2-Clause |

Licence checks (2026-10-22):
- **Z-Push**: the repository `LICENSE` is the GNU AGPL v3 text, GitHub reports AGPL-3.0, and 198 of the source files carry the AGPL
  header. A few bundled libraries (the IMAP backend's PEAR-style `Net/`, `Mail/`, `Auth/SASL`, `mimeDecode`) have their own
  permissive licences. The 2.7.7 release notes list security fixes, so we pin 2.7.7 and not 2.7.6.
- **`libawl-php` (Debian package 0.64)**: Z-Push's CalDAV backend needs it. GPL-2.0-or-later. It lives inside the Z-Push container.
- **Stalwart**: its README states AGPL-3.0 and the Stalwart Enterprise License v2 (proprietary, paid) side by side. We use the
  AGPL build and no enterprise feature.
- The AGPL programs run in their own containers and are reached over the network only (IMAPS, SMTPS, HTTP). Nothing of them is
  linked into phecloud (Apache-2.0). `phecloud hub` speaks Stalwart's JMAP management API over HTTP.
- We run Stalwart unmodified. Z-Push is unmodified source plus configuration edits made by `sed` at image build time.

## Set up

```
sh deploy/hub/hub-init.sh                     # HUB_DOMAIN=hub.lan HUB_OWNER=owner  (defaults; HUB_DOMAIN falls back to PHECLOUD_DOMAIN)
docker compose --profile hub up -d            # later starts; hub-init.sh already started both containers
```
`hub-init.sh` takes about 10 s and does this:
1. writes `deploy/hub/.env.hub` (mode 600; git-ignored) with random secrets, starts Stalwart in its bootstrap mode;
2. completes the bootstrap through Stalwart's management API (host name, default mail domain, no ACME because Caddy does TLS);
   the reply carries the permanent administrator login, which is saved in `.env.hub`;
3. restarts Stalwart (it must restart once after bootstrap), creates the **one owner account** (`owner@<domain>`, random
   password in `.env.hub`), raises the app-password limit from 5 to 50;
4. creates an API key on the owner account for phecloud's own CLI and writes `deploy/hub/.env.phecloud` (URL, owner name,
   token). The phecloud container reads this file (`env_file`, optional), so the owner password never enters it;
5. builds and starts Z-Push.

If a run breaks half way, `docker compose --profile hub down -v`, delete `deploy/hub/.env.*` and run it again.

### Per-device app passwords (a phone gets its own, revocable password)
```
docker exec phe-cloud phecloud hub apppw add "old phone" [days]   # prints user + password ONCE (and the server name)
docker exec phe-cloud phecloud hub apppw list
docker exec phe-cloud phecloud hub apppw revoke <id>              # that device gets 401 on ActiveSync, IMAP, SMTP, CalDAV, CardDAV
docker exec phe-cloud phecloud hub status
```
How it works: Stalwart 0.16 has an `AppPassword` object per account (a secret of the form `app_...`, 43 characters, shown only
at creation, optional expiry and IP allow-list). **Only the account itself can create one; an admin can list and revoke but not
create.** So phecloud authenticates as the owner with an owner-scoped API key (`Authorization: Bearer API_...`) and calls
`x:AppPassword/set`. Tested: create, list, revoke; a revoked password is refused immediately on ActiveSync (401) and on DAV
(401); the owner password and the other devices keep working.
Not done: the app passwords are not yet rows in phecloud's `devices` list (`phecloud devices`); `hub apppw` is its own list.
Revoking does not send an ActiveSync RemoteWipe.

Later (passkeys and OIDC, not built): Stalwart 0.16 has an OIDC provider (`OidcProvider`, `OAuthClient` objects) and OAuth
login; phecloud could become the identity provider for the web UI and the admin (passkey login), or use Stalwart's as the
provider. App passwords stay for the legacy clients (an old phone cannot do OAuth). The only secret phecloud needs in the
meantime is the API key, which is independent of the owner's password, so the owner password can be replaced by a passkey
without touching it. If the API key is ever revoked, create a new one as the owner (`stalwart-cli create ApiKey`).

## Endpoints

All behind Caddy on one name. `CADDYFILE=./deploy/hub/Caddyfile.hub PHECLOUD_DOMAIN=<lan name or ip> docker compose --profile hub --profile tls up -d`.
LAN: `tls internal` (Caddy's own CA; the root is `docker exec phe-caddy cat /data/caddy/pki/authorities/local/root.crt`).
Public host: remove `tls internal` and set `PHECLOUD_DOMAIN` to a DNS name; Caddy gets a Let's Encrypt certificate.

| Path | Goes to | Notes |
|---|---|---|
| `/Microsoft-Server-ActiveSync` | Z-Push | long-poll friendly (no buffering, 15 min read timeout) |
| `/.well-known/caldav`, `/.well-known/carddav` | Stalwart | 307 to `/dav/cal`, `/dav/card` |
| `/dav/cal/<user>/default/`, `/dav/card/<user>/default/` | Stalwart | `<user>` = `owner%40hub.lan` |
| `/jmap`, `/.well-known/jmap`, `/admin`, `/api`, `/auth`, `/oauth` and the OAuth/OIDC discovery paths | Stalwart | web admin and JMAP |
| everything else | phecloud web app | |

`default_sni` is set in the Caddyfile: a client that connects to an IP address sends no SNI (curl, old phones), and Caddy
otherwise answers with a TLS "internal error" alert.

Not published by default: IMAPS 993 and SMTPS 465 (desktop mail clients on the LAN: uncomment the `ports` line in
`docker-compose.hub.yml`), and port 25 (mail from outside needs a real domain and MX; not set up).

## TLS and old phones

What the server offers:
- **Caddy**: TLS 1.2 and 1.3 only; ECDHE key exchange; AES-GCM and ChaCha20 only; an ECDSA P-256 certificate from its internal CA.
  Checked with `openssl s_client`: TLS 1.0 and 1.1 are refused, an RSA key exchange cipher (AES256-SHA) gets a handshake failure,
  TLS 1.2 gives `ECDHE-ECDSA-AES128-GCM-SHA256`, TLS 1.3 gives `TLS_AES_128_GCM_SHA256`. Go's TLS library cannot offer TLS 1.0 at all.
- **Stalwart's own HTTPS/IMAPS/SMTPS listeners**: rustls, TLS 1.2 and 1.3 (a `tlsDisableProtocols` option exists per listener).

What a Symbian^3 phone can do (from our own analysis of its firmware): its OpenSSL is 0.9.8-era, **TLS 1.0
at most**, RSA key exchange with AES/3DES-CBC and SHA-1 MACs, no EC cipher suites, no AEAD. So **the phone cannot talk to Caddy's
listener** until the TLS 1.2/1.3 replacement of its SSL library ships. Two options:

1. **Tunnel, plain HTTP (simplest, recommended for now).** The phone reaches the hub through the phetail WireGuard tunnel, which
   already authenticates and encrypts. Z-Push publishes plain HTTP on `${PHECLOUD_EAS_PLAIN_BIND:-127.0.0.1}:8081`; set
   `PHECLOUD_EAS_PLAIN_BIND` to the host's tunnel address. Phone: SSL off, port 8081. Never bind it to a LAN or public address.
2. **Legacy TLS listener** (`docker compose --profile hub --profile hub-legacy up -d`, port `${PHECLOUD_LEGACY_TLS_PORT:-8443}`),
   for use on a LAN or in a tunnel, not the internet: nginx with `ssl_protocols TLSv1 TLSv1.1 TLSv1.2`, security level 0, ciphers
   `AES128-SHA AES256-SHA ... DES-CBC3-SHA`, an RSA-2048 key, a certificate signed (SHA-256) by a small RSA CA created on first
   start. It proxies only `/Microsoft-Server-ActiveSync`. Install the CA on the phone from `https://<host>:8443/ca.cer` (DER) or
   `/ca.crt` (PEM), or copy `/data/ca.crt` out of the `legacytls-data` volume. Checked with `openssl s_client`:
   TLS 1.0, 1.1 and 1.2 all negotiate; `AES128-SHA` works over TLS 1.0; TLS 1.3 is refused. **Not tried on a real phone**: whether
   its stack accepts a SHA-256 signed certificate and the chain is not verified.

## Phone settings (Mail for Exchange)

Use the manual setup (there is no Autodiscover endpoint). The names of the fields on the phone are from memory and may differ a
little; check them on the device.

| Field | Tunnel, plain HTTP | Legacy TLS listener | Caddy (modern clients) |
|---|---|---|---|
| Server address | the hub's tunnel address, e.g. `10.x.y.z` | host name or LAN IP | `hub.lan` / the public name |
| Port | `8081` | `8443` | `443` (or the published port) |
| Secure connection (SSL) | **off** | **on** (CA installed on the phone) | on |
| Domain | leave empty | leave empty | leave empty |
| User name | `owner@hub.lan` (the full address) | same | same |
| Password | the app password from `hub apppw add` (`app_...`, 43 characters, typed once) | same | same |
| Sync | contacts, calendar, tasks, mail; push on | same | same |

Z-Push with the IMAP, CalDAV and CardDAV backends announces ActiveSync **12.0, 12.1 and 14.0** (those backends report 14.0; only
the Kopano backend reports 14.1). The phone's EAS client knows 2.5, 12.0, 12.1 and 14.1, so it will use **12.1**: push mail,
contacts, calendar and tasks all work in 12.1. A 14.1 request is also accepted (the test client ran it), but the server does not
advertise it.

## Tests (deskmini, 2026-10-22)

`deploy/hub/test/` (stdlib Python, run in a container on the compose network):
- `eas.py`: a small EAS client with a WBXML encoder/decoder for the code pages it needs. `test_hub.py` seeds Stalwart (CardDAV PUT
  of 3 contacts, CalDAV PUT of 3 events, IMAP APPEND of 2 mails, 1 mail by SMTP on port 25), then through Z-Push at protocol 12.1:
  OPTIONS, Provision (both phases), FolderSync, Sync of contacts, calendar, inbox and junk, an EAS Add of a contact checked over
  CardDAV, SendMail (copy in Sent Items, delivered to the inbox), and Ping (push).
- `dav_test.sh`: vdirsyncer 0.19 (a standard CalDAV/CardDAV client) through Caddy over HTTPS with the Caddy root pinned: discovery,
  then sync to local files.
- Result, from a clean `hub-init.sh`: **25 of 25 checks pass.** Contacts keep names, e-mail and phone numbers; events keep summary,
  start and location; the all-day event arrives; an EAS-created contact is visible over CardDAV; a mail sent over EAS is delivered
  and filed in Sent Items. The same suite also passes through the Caddy listener (HTTPS) and the legacy listener (TLS 1.2 client);
  vdirsyncer synced 3 contacts and 3 events. Push latency: **31 s** (Z-Push polls the mailbox every 30 s; `PING_INTERVAL`).
- Revocation: after `hub apppw revoke`, ActiveSync and DAV return 401 at once.
- Footprint at idle (no client connected), `docker stats`: Stalwart 118 MiB, Z-Push (Apache + PHP) 38 MiB, Caddy 11 MiB, legacy
  listener 2.4 MiB, **about 170 MiB and 0.0 % CPU in total**. The Stalwart data directory with this test data is 1.9 MB. Images:
  Stalwart 420 MB, Z-Push 817 MB (PHP build tools and dev packages are still in it), Caddy 93 MB, legacy listener 290 MB.
- Not measured: load with a phone attached over days, large mailboxes.

## Open issues
- **Not tried with a real phone.** Field names, the SHA-256 certificate question and the 12.1 negotiation are inferences.
- A phone cannot use the Caddy listener until its TLS stack is replaced (see above); until then it needs the tunnel or the legacy listener.
- Z-Push writes vCard 4.0 contacts from EAS without an `FN` line when the phone does not send `FileAs` (seen with the test client);
  a phone normally sends it. Check on the real device.
- Stalwart's default spam filter files mail from an unauthenticated outside sender (our SMTP test) in Junk.
- The Z-Push image is large. Z-Push is lightly maintained PHP; our own `pheeas` (Go, EAS 14.1 only) is the planned replacement.
- App passwords are 43 characters; typing them on a phone keyboard once is the price. Stalwart sets the secret, we cannot choose it.
- Not built: passkey/OIDC login, devices-list integration, RemoteWipe on revoke, backups, mail from outside (MX, DKIM, SPF).

## Backup and restore

`phecloud hub backup <dir>` writes one directory, `hub-backup-<UTC time>/`, holding:

| file | what | how it stays consistent |
|---|---|---|
| `stalwart-data.tgz`, `stalwart-etc.tgz` | Stalwart's data and configuration volumes (mail, contacts, calendars, accounts, API keys) | Stalwart is stopped for the few seconds the two volumes are archived, then started again (also when the archive fails) |
| `phecloud.db` | phecloud's devices, pairing codes, cookies, settings | SQLite `VACUUM INTO` (`phecloud snapshot <path>`) while the server keeps running |
| `tls.crt`, `tls.key` | the server certificate (phones pin its fingerprint) | copied as is |
| `manifest.json` | creation time, Stalwart image, volume names, SHA-256 of every file | |

The directory is mode 0700 and holds the TLS key and the hub's secrets: keep it on a disk you trust, or encrypt it before it leaves the machine.

It drives the `docker` command line (containers `phe-stalwart` and `phe-cloud`; override with `PHE_STALWART`, `PHE_CLOUD`). On a Docker host without Go, use the helper image:

    docker build -t phe-backup:dev -f deploy/hub/Dockerfile.backup .
    docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -v /srv/backups:/backup phe-backup:dev phecloud hub backup /backup

`phecloud hub restore <backup dir> [--prefix p-] [--force]` verifies every checksum first (a damaged backup is refused before anything is touched), then creates the volumes `<p->stalwart-data`, `<p->stalwart-etc`, `<p->phecloud-data` and unpacks into them. It refuses volumes that already hold data unless `--force`. To restore for real, run it without a prefix after removing the old stack's volumes (or with `--force`), then start Stalwart on the two volumes and phecloud with its volume at `/data`. The API key and app passwords live in Stalwart's data, so they keep working.

Test: `HUB_ENV_FILE=<hub .env.phecloud> sh deploy/hub/test/backup_test.sh` (hub and phe-cloud running). It counts contacts, events, messages, device rows and the TLS fingerprint, backs up, restores into fresh volumes and a fresh `phe-rt-stalwart` container (1 CPU, 1 GB), and compares; it also checks that restoring over existing data and restoring a corrupted backup are both refused. It removes everything it created. Result on the Deskmini: 4 contacts, 3 events, 6 messages, 82 device rows, same certificate; the hub was down for about 3 s during the backup (archive: 861 KB of data).

### Routing note: `/passkey` vs `/auth`
Caddyfile.hub sends `/auth`, `/oauth` and the OAuth discovery documents to Stalwart. The web app's passkey endpoints therefore live
under `/passkey/*` (not `/auth/*`), which falls through to phecloud. Check: `curl -k https://<host>/passkey/status` answers
`{"owner":...}` (phecloud) and `curl -k https://<host>/.well-known/oauth-authorization-server` answers Stalwart's metadata.
