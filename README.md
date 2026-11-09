# Cloud Companion

Cloud Companion is a small server you run yourself, so your devices can keep doing what you need them to, whatever their
age. It's for one person: you, your devices and your accounts, on a machine you control.

It doesn't mind what you connect to it. A Nokia N9 or a Symbian phone that can no longer open a modern website, an
Android phone, a laptop, or no phone at all: some people will only want their own cloud desktop in a browser tab, and
that's a perfectly good way to use it. Old devices are where it helps the most, because they lost the most when the
services they relied on moved on.

Cloud Companion is a [Phorepoc](https://github.com/Phorepoc) project, and one of the places where Phorepoc (the Symbian
revival) and [Etesian](https://etesian-design.com) (the design language) meet: its web desktop wears the Etesian look,
and Phorepoc OS phones get a few extras, like pairing and the Catalogue store. Neither is needed to use it.

- **A browser in the cloud** (being replaced, not available right now). Old phone browsers can't open most websites
  anymore, so the server is to open them for you.
- **A messaging gateway.** Your SIP or XMPP phone, or the web app, can reach SMS, Matrix, XMPP, IRC, Nextcloud Talk,
  Signal and Telegram, and you decide which address goes where.
- **A web app** you can install on any device, with a browser, your text messages (sent through your own phone), mail,
  calendar and contacts in one place.
- **Mail, calendar and contacts for old phones**, if you want them. An optional hub speaks Exchange ActiveSync, CalDAV
  and CardDAV, so phones that understand any of those just work.
- **Android in a browser tab**, also optional. An Android system runs on your server and streams to a web page, with
  touch, keyboard, sound and clipboard.

This is an experimental service. The software works but it's young, so please read the security section before you put
it on the internet. (You'll still see its working name, `phecloud`, on the server program, the Docker service and the
`PHECLOUD_*` settings.)

## Getting started

All you need is Docker with the compose plugin, and a few GB of RAM.

```sh
git clone https://github.com/Phorepoc/cloud-companion && cd cloud-companion
docker compose pull
docker compose up -d
docker logs phe-cloud
```

This pulls the released images from `ghcr.io/phorepoc` (`phecloud`, and `phecloud-android-bridge` for the optional Android page); the compose files default to the release this checkout was made for (v0.2.0;
`PHECLOUD_VERSION=latest` or another `vX.Y.Z` overrides it). To update, `git pull` and `docker compose pull && docker compose up -d`. To build from this checkout instead (development):
`docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build` (the Android bridge: add `-f docker-compose.android.build.yml`). Building needs a GitHub token that can read packages (a classic token with only `read:packages`) in `GITHUB_PACKAGES_TOKEN`: the web app uses the Etesian design packages, which GitHub Packages serves only to signed-in users. The token is passed as a build secret and is not kept in the image.

A brand-new server has no owner yet, so at start it prints a **first-start code** in its log, in a box like this:

```
+----------------------------------------------------------------------------+
| FIRST START: this server has no owner yet.                                 |
| Open http://127.0.0.1:8080                                                 |
| and enter this code to register the first owner:                           |
|         HFRE-RHAX-K3MN                                                     |
+----------------------------------------------------------------------------+
```

Open <http://127.0.0.1:8080>, type the code in, and that browser becomes your admin device. In **Settings > Security** you
can add a passkey and get a recovery code. The first-start code stays valid until it is used (once), so there is no
hurry; every restart of a server that still has no owner prints a new one and the old one stops working. As soon as
the first owner exists (a passkey is registered or a first device is paired) the code is gone for good and nothing is
printed any more. The code is stored only as a hash. A phone can use it as well: pairing the first device with it makes
that phone the admin.

Prefer to choose the code yourself? Set `PHECLOUD_FIRST_OWNER_CODE` (in `docker-compose.yml`, commented out there): at least
12 letters, digits, `-` or `_` with at least 6 different characters. The log then only says that it is active and does not
print it. A weak value is rejected with an error in the log and a generated code is used instead. It works once, only for
the first owner, and is ignored (with a log line) once the server has an owner.

Later admins (another browser or computer, a replacement passkey) need an admin pairing code, valid for 10 minutes:
`docker exec phe-cloud phecloud paircode --admin`.

To add a phone, run `docker exec phe-cloud phecloud paircode` without `--admin` and enter the code on the phone. For a
Phorepoc OS phone, [docs/phorepoc-phone.md](docs/phorepoc-phone.md) explains the steps. `phecloud devices` lists what's
paired and `phecloud revoke <id>` signs one out.

## Reaching it from other machines

Out of the box the web app only listens on 127.0.0.1, over plain HTTP. To use it from anywhere else, put it behind
HTTPS. Passkeys need that anyway.

The simplest way is the bundled Caddy, if you have a domain name pointing at the server:

```sh
CADDYFILE=./deploy/Caddyfile PHECLOUD_DOMAIN=cloud.example.org PHECLOUD_HTTPS_PORT=443 \
PHECLOUD_TRUSTED_PROXIES=172.16.0.0/12 docker compose --profile tls up -d
```

That gets a Let's Encrypt certificate by itself. Already have Traefik, nginx or a Cloudflare Tunnel? Point it at port
8080 and set `PHECLOUD_TRUSTED_PROXIES` to its address. On a tailnet, `tailscale serve --https=443 http://127.0.0.1:8080`
gives you a real certificate with no open ports at all. [docs/reverse-proxy.md](docs/reverse-proxy.md) covers each of
these.

Phones connect on a separate port, 7400. It only allows TLS 1.3 and only paired devices, and it's open on all
interfaces so your phones can reach it. `PHECLOUD_PHONE_BIND` narrows that down.

## Settings

Everything is set through environment variables in `docker-compose.yml`. The ones you're most likely to change:

| Variable | What it does |
|---|---|
| `PHECLOUD_DB` | where the data is kept. SQLite by default, or a `postgres://` or `mysql://` address ([docs/database.md](docs/database.md)) |
| `PHECLOUD_TRUSTED_PROXIES` | your reverse proxy's address. Nobody is trusted by default |
| `PHECLOUD_FIRST_OWNER_CODE` | your own first-start code for the very first owner, instead of the generated one in the log (see Getting started) |
| `PHECLOUD_PUBLIC_URL` | the address people reach it at, needed for signing in with Google or Microsoft |
| `PHECLOUD_OAUTH_*` | your own Google and Microsoft app registrations ([docs/connections.md](docs/connections.md)) |
| `PHECLOUD_DLNA_LISTEN`, `PHECLOUD_DLNA_NAME` | turns on the DLNA media server for TVs and players on your home network, e.g. `:8200`, and the name they show ([docs/files.md](docs/files.md)) |
| `PHECLOUD_ANDROID_SECRET` | turns on the Android stream ([docs/android.md](docs/android.md)) |
| `PHECLOUD_CATALOGUE_DIR`, `PHECLOUD_CATALOGUE_MAX_FILE` | where uploaded and kept installation files live (default `/data/catalogue`) and the largest file in bytes ([docs/catalogue.md](docs/catalogue.md)) |
| `PHECLOUD_FILES_DIR` | the Files section and the Music library: the owner's files, shared by every paired device (default `/data/files`); public share links are served at `/s/<token>` |

Your devices, keys and certificate live in the `phecloud-data` volume, so back it up. `phecloud snapshot` helps with
that.

## Your own software catalogue

Cloud Companion can host a software list for Catalogue, the store on a Phorepoc OS phone. In the web app open **Catalogue**, make a catalogue (public,
or private with a secret token in its address), and upload installation files: `.sisx`, but also `.apk`, `.exe`, `.deb`, `.AppImage`, `.zip` and
others, with a name, version and description. It publishes them as an `index.json` in the same format as the official list, with the size and
SHA-256 of every file. You can also add other lists as sources (the official one, a friend's Cloud Companion, any compatible `index.json`), give
each a priority, and have the server keep copies of their files, which it only does when the SHA-256 matches the source's listing.

To use it on a phone, add the catalogue's address (the web app shows it, for a private one it contains the token) as a line in
`c:\data\pheget\indexes.txt` next to the official list; the phone app has no screen for that yet. Entries for other platforms are not listed by
the phone. Set `PHECLOUD_PUBLIC_URL` so the addresses in the list are the ones your phone can reach, and use HTTPS beyond your own network.
Details, the data model and the small extension to the list format: [docs/catalogue.md](docs/catalogue.md).

## The mail hub

```sh
sh deploy/hub/hub-init.sh
docker compose --profile hub up -d
```

This adds two more containers. [Stalwart](https://stalw.art) is a mail, calendar and contacts server, and
[Z-Push](https://z-push.org) puts Exchange ActiveSync in front of it, which is what most old phones speak. Every device
gets its own password that you can revoke (`phecloud hub apppw add`). Phones too old for modern TLS can use the tunnel,
or an optional TLS 1.0 listener (`--profile hub-legacy`). [docs/hub.md](docs/hub.md) has the exact phone settings.

## Messaging

Cloud Companion passes messages between your devices and your services: SMS through your phones, SIP services (your own Asterisk:
`docker compose --profile asterisk`, or a provider), Matrix (encrypted), XMPP, IRC and Nextcloud Talk today, with Signal and Telegram
planned. You decide in Connections → Messaging which address goes where. Phones with a SIP or XMPP (Jabber) client (the Nokia N9,
Symbian, softphones) can sign in and chat through it: set `PHECLOUD_SIP_LISTEN=:5060` and/or `PHECLOUD_XMPP_LISTEN=:5222` and make
a login per phone. [docs/messaging.md](docs/messaging.md) explains the rules and has the phone settings.

## Your other clouds

Photos gathers your pictures in one timeline, newest first: your Immich library (its albums too) and any folder you mark with
*Show in Photos*, on Cloud Companion or in another cloud. Your Immich albums also appear in Files, so every device can reach them.

Your files on Nextcloud, Seafile or any WebDAV server (a NAS, ownCloud, a hosting provider) show up in Files next to the ones kept
on Cloud Companion, one folder per account. Add them in Connections; Nextcloud signs in with its own approval page, so your
password never passes through here. They are read-only for now: open, play and download them on any of your devices, and change
them where they live.

Files also opens in file managers and phones that speak WebDAV (Dolphin, Nautilus, Windows, iOS Files, Symbian and N9 apps) at
`/dav/` on this server, with a phone login. TVs, consoles and media players on your home network can play your music, videos
and photos too, over DLNA, once you turn it on. [docs/files.md](docs/files.md) has the details.

## Security

What the defaults do for you:

- There are no default passwords. Device keys, the TLS certificate and the key that protects your stored account
  passwords are all generated on first use, and pairing codes work once and expire after 10 minutes.
- The web app only listens locally. The phone port only accepts TLS 1.3 from paired devices, and a device's key never
  crosses the network after pairing.
- The web app uses a strict content security policy, passkeys with a recovery code, rate limits on pairing and sign-in,
  and signed requests for everything an admin can do.
- The Android stream needs a short-lived ticket that only a signed-in admin can get. The Android container itself has
  to run privileged, so keep it on an internal network.

What's still up to you:

- Use HTTPS for anything that isn't on your own machine.
- Keep `PHECLOUD_TRUSTED_PROXIES` limited to your actual proxy.
- Back up the data volume.
- If you open the phone port to the internet, firewall it to your phones' networks if you can.
- Check the licenses of the optional containers, listed in `NOTICE`.

Please report vulnerabilities privately, as described in `SECURITY.md`.

## Working on it

`go test -tags goolm ./...` runs the server tests (the tag, or `GOFLAGS=-tags=goolm`, is needed for every build: Matrix
encryption uses mautrix-go's pure-Go olm instead of cgo and libolm). `sh web/test/db_test.sh` repeats them against PostgreSQL and MariaDB, the
browser tests are in `web/test/` (TypeScript, run by Node directly with `--experimental-strip-types`; `npm run check` in that
directory type-checks them), and `make -C client check` tests the phone-side code. GitHub Actions runs all of
them except the Android tests, which need a Docker host with the `binder_linux` kernel module.

`npm install` sets up the git hooks: commit messages are linted, and the web app and browser tests are type-checked before each commit.

The server and its admin commands are in `cmd/phecloud`. The web app is in `web/`, the phone-side code in `client/`, and
the extra containers in `deploy/`. [docs/status.md](docs/status.md) has what works and what's still open.

## Thank you

Cloud Companion is a small layer on top of a great deal of other people's work. Every message it routes, every mail it copies and
every page it shows depends on the projects below, and on the people who write, review, maintain, package and document them,
often in their own time and for years. We are deeply grateful to all of them: none of this would be possible without the work and
care they have put into their projects. If Cloud Companion is useful to you, please consider supporting them too.

(`NOTICE` lists each one's licence.)

**Built into the server**
- Language and core: [Go](https://go.dev) and its [extended libraries](https://pkg.go.dev/golang.org/x) (crypto, net, sync, sys, text, exp)
- Messaging:
  [gotd/td](https://github.com/gotd/td) with [ige](https://github.com/gotd/ige), [neo](https://github.com/gotd/neo) and [log](https://github.com/gotd/log) (Telegram);
  [mautrix-go](https://github.com/mautrix/go) and [go.mau.fi/util](https://github.com/mautrix/go-util) (Matrix);
  [mellium xmpp](https://mellium.im/xmpp), [sasl](https://mellium.im/sasl), [xmlstream](https://mellium.im/xmlstream) and [reader](https://mellium.im/reader) (XMPP);
  [ergochat/irc-go](https://github.com/ergochat/irc-go) (IRC);
  [sipgo](https://github.com/emiago/sipgo) and [icholy/digest](https://github.com/icholy/digest) (SIP);
  [rsc.io/qr](https://github.com/rsc/qr) (QR codes)
- Mail: [go-imap](https://github.com/emersion/go-imap), [go-message](https://github.com/emersion/go-message), [go-sasl](https://github.com/emersion/go-sasl)
- Sign-in and security: [go-webauthn](https://github.com/go-webauthn/webauthn) and [x](https://github.com/go-webauthn/x),
  [golang-jwt](https://github.com/golang-jwt/jwt), [go-tpm](https://github.com/google/go-tpm),
  [edwards25519](https://github.com/FiloSottile/edwards25519), [uTLS](https://github.com/refraction-networking/utls),
  [bluemonday](https://github.com/microcosm-cc/bluemonday) with [douceur](https://github.com/aymerick/douceur) and [gorilla/css](https://github.com/gorilla/css)
- Databases: [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) with [libc](https://gitlab.com/cznic/libc), [memory](https://gitlab.com/cznic/memory)
  and [mathutil](https://gitlab.com/cznic/mathutil), [bigfft](https://github.com/remyoudompheng/bigfft);
  [pgx](https://github.com/jackc/pgx), [puddle](https://github.com/jackc/puddle), [pgpassfile](https://github.com/jackc/pgpassfile),
  [pgservicefile](https://github.com/jackc/pgservicefile); [go-sql-driver/mysql](https://github.com/go-sql-driver/mysql)
- Web and browser: [coder/websocket](https://github.com/coder/websocket); [gobwas ws](https://github.com/gobwas/ws),
  [httphead](https://github.com/gobwas/httphead) and [pool](https://github.com/gobwas/pool)
- Encoding, compression and tools: [fxamacker/cbor](https://github.com/fxamacker/cbor), [x448/float16](https://github.com/x448/float16),
  [go-faster jx](https://github.com/go-faster/jx), [errors](https://github.com/go-faster/errors) and [xor](https://github.com/go-faster/xor),
  [segmentio/asm](https://github.com/segmentio/asm), [klauspost/compress](https://github.com/klauspost/compress),
  [brotli](https://github.com/andybalholm/brotli), [xxhash](https://github.com/cespare/xxhash), [easyjson](https://github.com/mailru/easyjson),
  [intern](https://github.com/josharian/intern), [msgp](https://github.com/tinylib/msgp) with [fwd](https://github.com/philhofer/fwd),
  [gjson](https://github.com/tidwall/gjson), [sjson](https://github.com/tidwall/sjson), [match](https://github.com/tidwall/match),
  [pretty](https://github.com/tidwall/pretty), [mapstructure](https://github.com/go-viper/mapstructure), [go-yaml](https://github.com/yaml/go-yaml),
  [google/uuid](https://github.com/google/uuid), [go-humanize](https://github.com/dustin/go-humanize),
  [backoff](https://github.com/cenkalti/backoff), [zerolog](https://github.com/rs/zerolog),
  [go-colorable](https://github.com/mattn/go-colorable), [go-isatty](https://github.com/mattn/go-isatty),
  [goid](https://github.com/petermattis/goid), [uber-go/atomic](https://github.com/uber-go/atomic),
  [multierr](https://github.com/uber-go/multierr), [OpenTelemetry Go](https://github.com/open-telemetry/opentelemetry-go)

**The web app**
[Vue](https://vuejs.org), [Vue Router](https://router.vuejs.org), the [Etesian Design Language](https://etesian-design.com) packages (the desktop, windows and components), [Quasar](https://quasar.dev) and [Quasar extras](https://github.com/quasarframework/quasar/tree/dev/extras)
(with Google's [Material Symbols](https://github.com/google/material-design-icons)), [Fira Sans](https://github.com/mozilla/Fira) via
[Fontsource](https://fontsource.org), [noble-hashes](https://github.com/paulmillr/noble-hashes), [cbor-x](https://github.com/kriszyp/cbor-x),
[Vite](https://vite.dev), [TypeScript](https://www.typescriptlang.org) and [vue-tsc](https://github.com/vuejs/language-tools), and
[Playwright](https://playwright.dev) for the browser tests.

**The phone client**
[Mbed TLS](https://github.com/Mbed-TLS/mbedtls), [libpng](http://www.libpng.org/pub/png/libpng.html) and [libjpeg-turbo](https://libjpeg-turbo.org).

**The containers the compose files run**
- [Stalwart Mail Server](https://stalw.art), [Z-Push](https://github.com/Z-Hub/Z-Push), [Radicale](https://radicale.org),
  [Asterisk](https://www.asterisk.org) (packaged by [Alpine Linux](https://alpinelinux.org)),
  [signal-cli-rest-api](https://github.com/bbernhard/signal-cli-rest-api) and [signal-cli](https://github.com/AsamK/signal-cli)
- [Caddy](https://caddyserver.com), [Traefik](https://traefik.io), [PostgreSQL](https://www.postgresql.org), [MariaDB](https://mariadb.org),
  [docker-socket-proxy](https://github.com/Tecnativa/docker-socket-proxy), [nginx](https://nginx.org), [PHP](https://www.php.net),
  [Docker](https://www.docker.com)
- Android: [Redroid](https://github.com/remote-android/redroid-doc), the [Android Open Source Project](https://source.android.com)
  and its SDK tools, [Gradle](https://gradle.org), [Eclipse Temurin](https://adoptium.net) and [scrcpy](https://github.com/Genymobile/scrcpy)
- The base systems under all of them: [Debian](https://www.debian.org), [Alpine Linux](https://alpinelinux.org), [Node.js](https://nodejs.org),
  [OpenSSL](https://www.openssl.org) and [fontTools](https://github.com/fonttools/fonttools)

**The boards**
[Raspberry Pi OS](https://www.raspberrypi.com/software/) and [pi-gen](https://github.com/RPi-Distro/pi-gen) for the board image,
[Docker Engine](https://docs.docker.com/engine/) with [Compose](https://github.com/docker/compose), [NetworkManager](https://networkmanager.dev)
with [dnsmasq](https://thekelleys.org.uk/dnsmasq/doc.html) for the setup hotspot, [Avahi](https://avahi.org) for `cloud-companion.local`,
[systemd](https://systemd.io), [restic](https://restic.net) for backups (and its [REST server](https://github.com/restic/rest-server) in our tests), and [QEMU](https://www.qemu.org) with [binfmt](https://github.com/tonistiigi/binfmt) for building it on x86.

**The servers our tests run against**
[Conduit](https://conduit.rs) (Matrix), [Prosody](https://prosody.im) (XMPP), [Ergo](https://ergo.chat) (IRC),
[Nextcloud](https://nextcloud.com) and [Nextcloud Talk](https://github.com/nextcloud/spreed), [Seafile](https://www.seafile.com) (with
[Redis](https://redis.io) beside it), [Immich](https://immich.app) (with [Valkey](https://valkey.io), and PostgreSQL with
[VectorChord](https://github.com/tensorchord/VectorChord) and [pgvecto.rs](https://github.com/tensorchord/pgvecto.rs)), [curl](https://curl.se),
[rclone](https://rclone.org), [async-upnp-client](https://github.com/StevenLooman/async_upnp_client)
with [Python](https://www.python.org), and [slixmpp](https://codeberg.org/poezio/slixmpp).

## License

Apache-2.0. The optional containers the compose files can start keep their own licenses, which `NOTICE` lists.
