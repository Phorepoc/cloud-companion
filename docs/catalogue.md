<!-- SPDX-License-Identifier: Apache-2.0 -->
# Self-hosted catalogues

Catalogue is the software store on a Phorepoc phone. By default it reads the official list,
<https://phorepoc.github.io/index/index.json>. Cloud Companion can serve **your own list in the same format**: you upload installation
files, it publishes an `index.json` with each file's size and SHA-256, and it can merge in other lists (the official one, a friend's
Cloud Companion, any compatible `index.json`) and keep copies of their files, so a phone downloads everything from one place.

Code: `internal/catalogue` (merge, files, sync), `internal/webui/catalogue.go` (HTTP), `internal/store/catalogue.go` (tables, migration 2),
the **Catalogue** page of the web app (`web/src/pages/CataloguePage.vue`). Tests: `internal/webui/catalogue_test.go`.

## Using it
1. Open **Catalogue** in the web app (owner only) and make a catalogue. Choose **public** (no token) or **private** (a secret token is part of the address).
2. **Upload an app**: the file, an id (`my-app`), a version and a name; summary, description, platform, processors, target boards and an icon are optional.
   Uploading the same id with a new version adds a release; the newest upload gives the name and description.
3. Copy the **address for Catalogue** shown under the catalogue (`https://you.example.org/catalogue/<id>/index.json`, private ones
   `.../catalogue/<id>/<token>/index.json`). The address is built from `PHECLOUD_PUBLIC_URL`, else from the host your browser used (behind a
   reverse proxy: set `PHECLOUD_TRUSTED_PROXIES`, see reverse-proxy.md). Phones need a plain `http://` or `https://` address they can reach.
4. **Sources** (optional): add other lists by the address of their `index.json`, with a priority and, if you want, **Keep copies of its files here**.

### What the phone needs
Catalogue on the phone reads the official list and, in addition, every address listed (one per line) in `c:\data\pheget\indexes.txt` (`#`
starts a comment). It sends no headers beyond `User-Agent`, so **a private catalogue must be given with the token in the address** (the path
form). It skips any item whose `compatibility.arch` does not contain the phone's own architecture (`arm` or `x32`), so entries for other
platforms never show up on a phone, and it checks the SHA-256 of every download before it installs. As of this writing the phone app has no
screen for adding a list; the file is the way. The app merges the lists by `id` and **the first list wins**, and the official one is always
first, so an app id that is also in the official list shows the official entry on the phone; use your own ids. (Giving the official list to
your catalogue as a source is still useful for keeping copies of its files on your server and for other clients.)

## Data model (tables from migration 2)
| Table | What it holds |
|---|---|
| `catalogues` | `id` (the address segment), `name`, `public`, `token` (private only), `priority` (of the hosted entries, default 100), `sync_interval` (seconds, default 21600, 0 = only by hand) |
| `cat_releases` | one row per (catalogue, app id, version): the metadata and file description as JSON (`catalogue.Release`) |
| `cat_sources` | a federated source: `url`, optional bearer `token`, `priority` (default 50), `enabled`, `mirror`, last sync time and error |
| `cat_source_items` | the source's last fetched listing, one JSON item per app id, with the per-app `mirror` override (-1 follow the source, 0 never, 1 always) |
| `cat_mirror` | per source and SHA-256: `ok`, `failed` or `mismatch` (and the reason) for the cached file |

The files are on disk in `PHECLOUD_CATALOGUE_DIR` (default `<PHECLOUD_TLSDIR>/catalogue`, that is `/data/catalogue` in the container, so the
`phecloud-data` volume holds them), as `blobs/<first two hex>/<sha256>`; one file is stored once however many releases or sources use it. Back up
the volume like the rest. A file is deleted when its last release or mirror is removed. Tokens and source tokens are stored in the database like
device keys are; they are not encrypted.

Limits: a file may be 2 GiB (`PHECLOUD_CATALOGUE_MAX_FILE`, bytes), an icon 4 MiB, a screenshot 8 MiB (at most 12), a source listing 32 MiB. Ids
are `^[a-z0-9][a-z0-9.-]{1,62}$` (the index's rule); file names are reduced to 100 safe characters (no directories, no `..`), because Catalogue
refuses anything else.

## The served index.json
Same document as the official one:
```json
{"format": 1, "generated": "2026-10-28T12:00:00Z",
 "catalogue": {"id": "mine", "name": "My catalogue"},
 "items": [ {"id": "my-app", "name": "...", "summary": "...", "category": "app", "license": "...",
             "source": {"type": "companion", "catalogue": "mine"},
             "releases": ["1.0"], "compatibility": {"arch": ["arm"]},
             "icon_url": "...", "screenshot_urls": [],
             "resolved": [{"tag": "1.0", "published": "2026-10-28T12:00:00Z", "asset": "my-app.sisx",
                           "url": "https://.../catalogue/mine/files/<sha256>/my-app.sisx", "size": 12345, "sha256": "<64 hex>"}]} ]}
```
A Symbian `.sis`/`.sisx` entry looks exactly like an official one, apart from `source.type`, which is `companion` where the official index has
`github` (clients do not read `source`). Everything Catalogue reads is there: `id`, `name`, `summary`, `category`, `compatibility.arch/boards`,
`resolved[].asset/url/sha256/published/tag`. `description`, `homepage`, `authors`, `compatibility.min_core/legacy/needs_platsec_hack` follow the
official schema when you set them.

### The extension: any platform
Two **optional** fields, which existing clients ignore (they only read the fields above):
- item `platform`: `symbian`, `android`, `windows`, `linux`, `macos`, `ios`, `other` (or any short lower case word). **Absent means `symbian`.**
- `resolved[].package`: the package type, normally the file's extension: `sis`, `sisx`, `apk`, `exe`, `msi`, `appimage`, `deb`, `rpm`, `zip`,
  `tar.gz`... **Absent means `sis`/`sisx`.** Both are left out for Symbian packages, so those entries stay identical to official ones.

Both are detected from the file name when you do not say (`.apk` is android/apk, `.deb` is linux/deb, `.zip` is other/zip). For a platform that
is not Symbian, `compatibility.arch` holds that platform's processor names (`aarch64`, `x86_64`, `any` as default) and not `arm`/`x32`, so the
phone's Catalogue, which lists only items matching its own architecture, never offers them. A client for another platform filters on `platform`
and its own `arch`.

The official `schema/listing.schema.json` has `additionalProperties: false` and an `arch` enum of `arm`/`x32`, so it would reject these
entries; it describes the files in the `index` repository, not the generated document, which the clients read loosely. If the official index
should carry other platforms too, its schema needs `platform` (string), `resolved[].package` (string) and more `arch` values. A Symbian entry
of ours passes the official check unchanged except `source.type` (checked against `tools/build_index.py`).

## Access
- **Public**: `GET /catalogue/<id>/index.json` and `/catalogue/<id>/files/<sha256>/<name>`, no authentication.
- **Private**: the same with the token as a path segment, `/catalogue/<id>/<token>/index.json` (for clients that cannot send headers), or
  `Authorization: Bearer <token>` on `/catalogue/<id>/index.json`. The `url`s inside a private index carry the token, so a phone that fetched
  the list can download. Everything else answers 404 (a catalogue does not reveal that it exists), and wrong tokens count against the same
  limit as pairing codes: 8 a minute per client address. **Make a new token** (owner UI, `rotate_token`) kills the old address at once.
  Use HTTPS for anything that is not your own network: the token is part of the address.
- A file is served only if the catalogue's current index lists it, so knowing a hash does not reach another catalogue's files.
- **Owner API** (`/phe/catalogue*`): admin devices only, requests signed with the device key like the other owner calls (`X-Phe-Auth`).
  The listing is in the header comment of `internal/webui/catalogue.go`. Upload is `multipart/form-data` to `/phe/catalogue/upload` with the
  fields `catalogue, app_id, version, name, file` and the optional `summary, description, category, license, homepage, authors, platform,
  package, arch, boards, min_core, legacy, needs_platsec_hack, icon, screenshot`.

## Federation
A source is the address of any `index.json`-shaped document (`{"items": [...]}`); `https://phorepoc.github.io/index/index.json`, another Cloud
Companion's `.../catalogue/<id>/index.json` (with its token in the address, or entered as the source's token, sent as a bearer header to
that host only), or anything compatible.

**Sync.** The listing is fetched (http/https only, 32 MiB at most), every item is checked (valid id, a name, at least one release with a
64-hex `sha256` and an http(s) address; relative addresses become absolute against the source's address) and stored. Unusable items are
skipped and counted. A failed fetch keeps the previous listing and shows the error on the source. The server syncs every enabled source
whose catalogue's interval has passed (checked once a minute) and **Sync now** does it on request, for one source or all. Sources are
fetched by the server on your say-so, including addresses on your own network; only the owner can add them.

**Merging and conflicts.** The served list has one item per app id. Candidates are the catalogue's own uploads (priority = the catalogue's
priority) and the enabled sources' items (priority = the source's). The **highest priority wins and the whole item comes from it**: name,
description and all its releases. Ties go to the newest release (`published`), then to the catalogue's own uploads, then to the source order.
Versions are not compared (there is no common version scheme), so a higher priority source wins even with an older version. The owner view
shows for each app the winner and the others with the reason (`lower priority`, `same priority, older release`...). Change a priority and the
result changes immediately; nothing is lost.

**Keeping copies (mirror).** Per source and per app (`mirror`: follow the source / never / always) the server downloads the release files at
sync. A file is **kept only if its SHA-256 equals the one in the source's listing**; otherwise it is deleted, the source shows `mismatch`
and the release keeps the source's own download address (which the phone checks against the same hash). A cached file is identified by its
hash, and each time it is served it is read and hashed again unless unchanged since the last check, so a file changed on disk is never sent.
Only releases of the winning source are rewritten to the local address, and only when every cached copy is verified. Icons and screenshots of
a source are not copied; they keep the source's address.

## Not done
- A settings screen in the phone's Catalogue for adding lists and tokens (today: `indexes.txt`).
- A command line (`phecloud catalogue ...`); the owner API and web page cover everything.
- Browser tests for the Catalogue page (the Go tests cover the API and the rules above).
- Per-release approval, signatures of listings (the SHA-256 is the integrity check), garbage collection of files left by a crash during upload (`tmp/`).
