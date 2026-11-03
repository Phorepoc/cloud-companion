<!-- SPDX-License-Identifier: Apache-2.0 -->
# Self-hosted catalogues

Catalogue is the software store on a Phorepoc phone. By default it reads the official list,
<https://phorepoc.github.io/index/index.json>. Cloud Companion can serve **your own list in the same format**: you upload installation
files, it publishes an `index.json` with each file's size and SHA-256, and it can merge in other lists (the official one, a friend's
Cloud Companion, any compatible `index.json`) and keep copies of their files, so a phone downloads everything from one place.

Code: `internal/pkginfo` (SIS / APK / MBM readers), `internal/catalogue` (library scan, merge, files, sync), `internal/webui/catalogue.go` (HTTP), `internal/store/catalogue.go` (tables, migrations 2 and 4),
the **Catalogue** page of the web app (`web/src/pages/CataloguePage.vue`). Tests: `internal/webui/catalogue_test.go`.

## Using it
1. Open **Catalogue** in the web app (owner only) and make a catalogue. Choose **public** (no token) or **private** (a secret token is part of the address).
2. **Add apps**: put the files in the library folder and press **Rescan** (or **Upload a file**); the name, version, vendor and icon are read from the file. Select entries and press **Publish**. Edit the description or anything else with the pencil. See "The library" below.
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

## The library
Packages live in **one library folder** for the whole server, and each catalogue **publishes entries from it** (a file can be in several catalogues,
a catalogue can offer files from any subfolder). One folder with per-catalogue publishing was chosen over a folder per catalogue because the same file
is often offered in more than one list, moving a file between catalogues should not be a file operation, and a scan reads each file once.

`PHECLOUD_CATALOGUE_LIBRARY` is the folder (default `<catalogue dir>/library`; the compose file mounts a volume, or a host folder given by
`PHECLOUD_CATALOGUE_LIBRARY_DIR`, at `/library`). Put `.sis`, `.sisx`, `.apk` (and `.exe`, `.msi`, `.deb`, `.rpm`, `.zip` ... , which carry no
metadata we read) in it, in any subfolders. Dot-files and dot-folders are ignored.

### Scanning
A scan runs **at start, every `PHECLOUD_CATALOGUE_SCAN_INTERVAL` seconds (default 900, 0 = never), and on request** (the **Rescan** button, `POST /phe/catalogue/library/scan`).
- **Added**: a new file becomes an entry (unpublished) with the metadata read from it.
- **Changed**: a file whose size or modification time moved is hashed; a different SHA-256 means the file changed and its metadata is read again.
  A published entry stays published and serves the new file. A package that stops being readable is unpublished and flagged.
- **Moved / renamed**: the same SHA-256 under a new path is the same entry (publication and edits kept).
- **Removed**: the entry is **unpublished at once in every catalogue** and flagged **File missing** with the time it was noticed. The owner either
  deletes the entry or puts the file back and rescans: the entry returns to normal but is **not published again on its own**.
- **Unreadable**: the file is not a valid package of its type (or is too large to read, over 512 MiB for a SIS). It is listed with the reason and can be published
  only after its details are filled in by hand.
- **Upload** in the page (or `POST /phe/catalogue/upload`, same fields as before) stores the file in `uploads/` of the library and scans it; an upload
  with the same name and other content gets the hash in its name. The upload API publishes at once into the named catalogue (`publish=0` to not); a
  same-id-same-version upload replaces the earlier one.

### What the scanner reads
| Package | Read |
|---|---|
| SIS / SISX (Symbian 9) | English (else first) package name, vendor, version, package UID (app id `uid-xxxxxxxx`), languages, target devices (UID names such as S60 5th Edition, Symbian^3, Belle, and the names in the file), the number of installed files, the **processor from the first executable's E32 header** (`arm`, or `x32` for images built for this system), zlib-compressed blocks. **Icon**: the registration resource (`xxx_reg.rsc`, read as a resource file: its index, then the counted UTF-16 text fields) names the localisable resource `xxx_loc`; that names the icon file, and the application icon is number 0 of it. Dictionary-compressed resources fall back to a text search of the bytes. A **.mbm** is decoded (all colour depths, run-length coded, picture 0 with its mask as alpha); a **.mif** is read with its index (versions 1 and 2; version 3 files whose index is in a separate library are not read) and each icon is a bitmap of the `.mbm` of the same name, an **NVG-CS** vector icon or an **SVG-T binary** vector icon; vectors are turned back into SVG (kept, and shown in the page) and drawn to the PNG that the index offers (`icon_url`) by the built-in rasteriser (shapes, solid and gradient fills, strokes with round joins, opacity, transforms; no text, images, filters or dashes). Anything else falls back to a picture file named like an icon. |
| APK | binary `AndroidManifest.xml` and `resources.arsc`: package name (app id, lower case), `versionName` (else `versionCode`), the application label (strings and string resources, no-locale entry first, then English), `minSdkVersion`, the processors from `lib/<abi>/`. **Icon**: the densest PNG of the icon resource; an adaptive icon whose layers are PNGs or colours is composed. Vector and WebP icons are not drawn and the entry says so. |
| other | name from the file name, platform and package type from the extension; the version must be entered. |

The description is **never** read (packages do not carry one): it is yours to enter. Nothing from the package is executed or installed.

### Edits and the rule for keeping them
Name, app id, version, summary, description, homepage, category, license, authors, platform, processors, boards and the icon can be edited per
entry; an edit overrides what the file says, an empty field means "as read". The edits are kept separately from what was read, so a rescan never loses
them. **When the file changes**: the new metadata replaces what was read; edits stay if it is the same app (same app id read from the file) except the
**version**, which is always read again; if the app id read from the file is different, all edits are dropped, since it is another app.

### Publishing
**Publish** / **Unpublish** act on the selected entries for the open catalogue. An entry can only be published when it is not missing and its release
is complete (app id, version, name). Two published entries with the same app id and version in one catalogue are refused (unpublish the other first).
Deleting an entry removes it and its publications; with "also delete the file" it removes the file from the folder as well (otherwise the next scan
finds it again).

### Upgrading
Migration 4 moves every release of the earlier model (`cat_releases`) into the library as a published **stored** entry in its catalogue, with its file
still in `blobs/`; nothing is lost and the served `index.json` is unchanged. The old table is left in place, no longer read.

## Data model (tables from migration 2)
| Table | What it holds |
|---|---|
| `catalogues` | `id` (the address segment), `name`, `public`, `token` (private only), `priority` (of the hosted entries, default 100), `sync_interval` (seconds, default 21600, 0 = only by hand) |
| `cat_library` (migration 4) | one row per package file: path in the library, SHA-256, size, status (`ok` / `missing` / `unreadable`), what was read (`meta`), the owner's `edits`, the icon, or a `stored` entry from an earlier version |
| `cat_publish` (migration 4) | which catalogue publishes which library entry, and since when |
| `cat_releases` | the earlier model, one row per (catalogue, app id, version); migration 4 moved it into the library; no longer read |
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
- Browser tests for the Catalogue page (the Go tests cover the API and the rules above). Not read: icons in a version 3 MIF whose index is in a separate DLL, WebP and vector Android icons, AIF icons of old S60 apps, embedded SIS files.
- Per-release approval, signatures of listings (the SHA-256 is the integrity check), garbage collection of files left by a crash during upload (`tmp/`).
