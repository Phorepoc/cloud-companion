# Cloud Companion

Cloud Companion is a small server you run yourself that helps old phones with the modern internet. It's for one
person: you, your phones, and your accounts.

- **A browser in the cloud.** Old phone browsers can't open most websites anymore. The server opens the page in
  Chromium instead and sends the phone pictures of it, over a compact encrypted connection.
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
docker compose up -d --build
docker exec phe-cloud phecloud paircode --admin
```

The last command prints a one-time code like `HFRE-RHAX`, which is good for 10 minutes. Open
<http://127.0.0.1:8080>, type the code in, and that browser becomes your admin device. In **Settings > Security** you
can add a passkey and get a recovery code.

To add a phone, run `docker exec phe-cloud phecloud paircode` without `--admin` and enter the code on the phone. For a
Phorepoc phone, [docs/phorepoc-phone.md](docs/phorepoc-phone.md) explains the steps. `phecloud devices` lists what's
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
| `PHECLOUD_PUBLIC_URL` | the address people reach it at, needed for signing in with Google or Microsoft |
| `PHECLOUD_OAUTH_*` | your own Google and Microsoft app registrations ([docs/connections.md](docs/connections.md)) |
| `PHECLOUD_ANDROID_SECRET` | turns on the Android stream ([docs/android.md](docs/android.md)) |
| `PHECLOUD_CATALOGUE_DIR`, `PHECLOUD_CATALOGUE_MAX_FILE` | where uploaded and kept installation files live (default `/data/catalogue`) and the largest file in bytes ([docs/catalogue.md](docs/catalogue.md)) |

Your devices, keys and certificate live in the `phecloud-data` volume, so back it up. `phecloud snapshot` helps with
that.

## Your own software catalogue

Cloud Companion can host a software list for Catalogue, the store on a Phorepoc phone. In the web app open **Catalogue**, make a catalogue (public,
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

`go test ./...` runs the server tests. `sh web/test/db_test.sh` repeats them against PostgreSQL and MariaDB, the
browser tests are in `web/test/` (TypeScript, run by Node directly with `--experimental-strip-types`; `npm run check` in that
directory type-checks them), and `make -C client check` tests the phone-side code. GitHub Actions runs all of
them except the Android tests, which need a Docker host with the `binder_linux` kernel module.

The server and its admin commands are in `cmd/phecloud`. The web app is in `web/`, the phone-side code in `client/`, and
the extra containers in `deploy/`. [docs/status.md](docs/status.md) has what works and what's still open.

## License

Apache-2.0. The optional containers the compose files can start keep their own licenses, which `NOTICE` lists.
