# phecloud

Server for Phorepoc Cloud: a self-hosted middleware that does the heavy lifting for old phones (a Symbian^3 phone,
Phorepoc OS on a PinePhone, x86), plus a cloud browser. Status: **v0 spike** (device pairing, phone protocol, cloud
browser v1 backend, a test client). Apache-2.0. Chromium is an external runtime dependency (not vendored), pinned to
`chromedp/headless-shell:156.0.8078.48`.

## What works
- **Pairing**: `phecloud paircode` prints a single-use short code (`XXXX-XXXX`, 10 min). The device sends `pair`
  with the code and gets a per-device 32-byte key; later connections authenticate with HMAC-SHA256(key, nonce)
  against a server challenge (the key never crosses the wire again). SQLite (`devices`, `pair_codes`);
  `phecloud devices`, `phecloud revoke <id>`.
- **Phone protocol** (`internal/proto`): one TCP connection (TLS 1.3 on :7400), frames =
  4-byte big-endian length + CBOR map with a `"t"` type key. Frames: `challenge`, `pair`/`hello`, `welcome`,
  `error`, `open`, `navigate` (url | back | forward | reload), `input` (tap / scroll / text), `page`, `layers`, `tile`,
  `regions`, `done`.
- **Cloud browser v1** (`internal/browser`, `internal/tiles`): headless Chromium through one adapter interface
  (`browser.Factory` / `browser.Session`; chromedp v0.11.2 is the only implementation). The page is rendered at
  360 px width, dpr 1, mobile UA, full height (cap 8192 px, `PHECLOUD_MAX_PAGE_H`), after a scroll pass that triggers
  lazy-loaded images. It is cut into 360x256 tiles: paletted PNG when a tile has <= 256 colours (text), plain PNG
  for in-between, JPEG q70 when it has > 1200 distinct colours (photo-like).
  `position:fixed` and `position:sticky` elements are cut out as separate layer images (and hidden while the page
  is captured); links, buttons, inputs, selects and textareas become tap regions with ids and rects (relative to
  the layer they sit in).
- **Round 2:** settle is event-driven (CDP Network events: nothing in flight for 60 ms, load, two animation frames; no sleeps); the lazy scroll pass runs only if the page has lazy images; a pool of 2 warm tabs, each in its own browser context (isolated cookies); the cookie jar is saved per device in SQLite after every update and restored on the next open (verified: a cookie set in one connection is sent in the next); the `page` frame carries `trunc` when the page was cut at the height cap; the phone port is TLS 1.3 only, with a self-signed ECDSA certificate created at first start in `/data`. `phecloud paircode` prints the code (stdout) and the certificate fingerprint (stderr); the client pins the fingerprint at pairing (`-fp` to check it, else trust on first use) and refuses a changed certificate afterwards.
- **Input and updates**: a tap, scroll or text input goes back through CDP; the server re-renders, hashes every
  tile (FNV-64a of the pixels) and sends only tiles whose hash changed. A URL change starts a new page id.
- **`phecloud-client`** (`cmd/phecloud-client`): pairs, opens URLs, saves the tiles, prints byte totals and timing.
  `-tap-first-link` and `-type TEXT` exercise input.

## Run
```
docker compose up -d --build            # container phe-cloud: 2 CPUs, 2 GB, port 7400
docker exec phe-cloud phecloud paircode # -> HFRE-RHAX
docker run --rm --network host -v $PWD/run:/w -w /w --entrypoint phecloud-client phe-cloud:dev \
  -addr 127.0.0.1:7400 -code HFRE-RHAX -tap-first-link \
  -url https://example.com -url https://en.m.wikipedia.org/wiki/Nokia
```
Tiles land in `tiles/p<N>/L<layer>_y<row>_x<col>.png|jpg`. `testdata/layers.html` (+ `.url`, a `data:` URL) is a page
with a fixed header, a sticky bar, a fixed footer and an input, for the layers.

- **C client core** (`client/`): `phc`, a portable C99 implementation of the phone side (CBOR, framing, pairing + HMAC auth,
  tile cache, layers, tap regions with hit-testing, focus/keyboard state, select options, dialogs; no malloc, the transport and
  image decoding are the host's), `phc_mbedtls` (TLS 1.3 on mbedTLS 3.6, pinned certificate, non-blocking), selftest and a
  frame fuzzer run under ASan/UBSan. `client/test/run.sh` cross-checks it against the Go client on a live server over the real
  TLS listener (example.com with a tap and history back, the layers page, a Wikipedia article, a page with a search field, a
  select, a JavaScript prompt and a download link): identical tile counts, bytes and pixel hashes, regions, layers, titles,
  history flags, focus, options, dialog and download results; both clients also refuse a changed certificate.
  `client/SYMBIAN.md` is the design note for turning it into a web-engine plug-in.
- **Phone-protocol additions** (round 4): `page.back/fwd`; a `focus` frame after every update (kind, input type, value, caret,
  inputmode); `input` kinds `key`, `select` and `text` into the focused field; `<select>` options in the region list;
  `dialog`/`dialogreply` (alert, confirm, prompt, beforeunload; the page waits for the phone, 60 s timeout); `download`
  (downloads are refused in the cloud and reported). Each tab is its own browser context, so these are per device.
- **Round 5**: progressive loads (page load and taps send the first three tile rows and everything near them first, with
  `done.more = true`, then the rest after the lazy-image pass); the layout is always exactly 360 px (not "mobile" emulation, so
  pages without a viewport meta no longer render at 980 px and get cropped) and layers are clamped to the viewport; inner
  `overflow:auto` boxes of known size become `scroll` layers (the whole content is rendered, the phone scrolls it locally, a tap
  inside it carries content coordinates and the server scrolls the real box to that point); HTTP authentication (`dialog` kind
  `auth`, reply `text` = user and `pass`) and certificate errors (`dialog` kind `cert`; OK = load once with errors ignored); a
  per-device byte budget (`settings`: JPEG quality, grayscale), applied to every later tile.

## Web app (`web/`)
A Quasar v2 PWA (Vue 3, Vite; direct dependencies all MIT: vue, vue-router, quasar, @quasar/extras, cbor-x, @noble/hashes) served
by the Go server (the built `dist` is embedded into the binary; the Dockerfile builds it in a `node` stage). The web is just
another device type: it speaks the same CBOR frames as the phones, as binary WebSocket messages on `/ws`, with the same
pairing code and HMAC authentication; the device key is kept in IndexedDB.
- **Pairing page**: enter the code from `phecloud paircode` (`--admin` makes the new device an admin; the first paired device
  is always admin).
- **Browser page**: tiles on a canvas with local scrolling (wheel and drag), layers drawn on top (fixed, sticky, inner scroll
  boxes that scroll locally), tap -> hit-test -> region -> `input tap` frame with a short local highlight, the `focus` frame
  opens a real `<input>`/`<textarea>` over the field (typed text is sent as `text` frames, Enter as a key), JS dialogs, HTTP
  auth and certificate errors as Quasar dialogs, a `<select>` chooser, back/forward/reload, an address bar, refused-download
  notice.
- **Devices page** (admin only): list and revoke (`devices` / `revoke` frames, checked on the server).
- **Settings page**: JPEG quality slider and grayscale (the per-device byte budget), forget this device.
- **Style guide** (`web/STYLE.md`): principles (calm, content first, phone first, 44 px targets, WCAG AA, keyboard, reduced motion), design
  tokens (`web/src/css/tokens.css`: light and dark palettes with computed contrast ratios, system font scale, 4 px spacing, radii, elevation,
  motion), the app shell (bottom bar on a phone, icon rail on a tablet, labelled drawer on a desktop; sections Browser, Chats, Mail, Calendar,
  Contacts, Files, Devices, Settings from `web/src/nav.js`, placeholders for those not built yet), component rules, Material Symbols icons
  (Apache-2.0) and the writing style. Dark mode follows the system and has a toggle in Settings. The product name and logo are one replaceable
  token (`web/src/brand.js`: it feeds the page title, the manifest and the icon). Before/after screenshots (light and dark, 390 px and
  1280 px) are in `web/test/screenshots/before` and `.../after` (`sh web/test/shots.sh before|after`).
- **Contacts and Calendar** (needs the hub, see `docs/hub.md`): the server talks CardDAV and CalDAV to Stalwart as the hub's single owner
  (`internal/davhub`, using `PHECLOUD_HUB_URL` and the API key from `hub-init.sh`; the credentials never reach the browser). New frames
  `hub` / `hubres` carry `contacts.list|save|delete` and `events.list|save|delete`. Contacts: searchable list, view, create, edit (phones,
  emails, company, note), delete. Calendar: month agenda with previous/next/today, create, edit (title, all day, start/end, place, notes),
  delete. Existing items keep what the editor does not show (vCard lines such as photos, iCalendar recurrence rules, alarms and time
  zone definitions are written back unchanged). Deleting is deferred for the length of the "Undo" toast. Tests: `go test ./internal/davhub`
  and `HUB_ENV_FILE=<hub's .env.phecloud> sh web/test/hubstep.sh` (Playwright against a running hub; screenshots in `web/test/screenshots/hub`).
- **Round 11**: *App passwords* are rows in Devices (admin devices only): list, add (name and optional expiry; the secret is shown once with the
  user name and server, and can be copied), switch off (Stalwart's `AppPassword` objects through the server's `hub` frames
  `apppw.list|add|revoke`). *Calendar*: a Monday-first month grid (dots on phones, chips on larger screens, a selected-day list below) with the
  agenda kept as a toggle; timed events are written with the browser's time zone as `TZID` plus a minimal `VTIMEZONE` (offset at the event's
  start), and events that have a `TZID` are read as UTC and shown in the viewer's zone. *Mail* (read-only): folders, a message list with
  "show older", and a reader that shows plain text (HTML bodies are reduced to text; no pictures, links or scripts load), over JMAP; messages are
  not marked as read. Go tests: `go test ./internal/server` drives the `hub` frames against a fake Stalwart (CardDAV, CalDAV, JMAP), including
  the admin check, UID-preserving edits, the foreign-address refusal, time zones, app passwords and mail.
- **Round 12**: *Mail actions* (JMAP through the server): mark read/unread, move (Inbox, Junk, Trash and Archive where the hub has one) and delete
  (to Trash; from Trash it is destroyed) both wait for the length of the Undo toast before they reach the hub; reply (recipient, `Re:` subject, quoted
  text, `In-Reply-To`) and a composer, sent as a draft plus an `EmailSubmission` with the copy filed in Sent. **Outside sending is off by default**: the
  server only delivers to addresses at the hub's own domain unless `PHECLOUD_HUB_ALLOW_EXTERNAL=1`. A message with an HTML part also gets a *Formatted
  view*: the HTML is sanitised on the server (bluemonday: formatting and links only; no images, styles, scripts, forms or frames; links get
  `rel="nofollow noreferrer noopener"` and `target="_blank"`) and shown in a sandboxed frame with a `default-src 'none'` content policy. *Browser
  section*: a "Tabs on your devices" list (the button next to the tab strip) shows every connected device and its open cloud-browser tabs (the server keeps a
  registry of live sessions and the tab list each one last sent); from it a tab can be closed on its device, a page can be opened on another device,
  or its tab opened here. Actions on another device run inside that device's own request loop. Tests: Go (`go test ./internal/server`: mail actions,
  the outside-sending refusal, the sanitiser, the sessions registry and remote close) and Playwright (`hubstep.sh`: compose, refuse outside, send
  to the owner, mark, reply, move with Undo, delete to Trash then for good, cleanup; `run.sh`: two devices at once).
- **Icons and toasts**: the Material Symbols font is subset to the icons in use at build time (6.5 KB instead of 3.6 MB), and toasts follow STYLE.md (4 seconds, Undo, paused on hover or focus).
- Web server: `PHECLOUD_HTTP_LISTEN` (default `:8080`, plain HTTP: use the Caddy profile below for anything but localhost, or
  `PHECLOUD_HTTP_TLS=1` for the self-signed certificate).
- **Zoom** (pinch with two pointers, ctrl + wheel, or the zoom buttons): the cached tiles scale locally while the gesture runs; when it
  settles the web app asks the cloud to **re-render at the nearest scale of a ladder** (100, 125, 150, 200, 250, 300 percent, advertised
  as the `zoom` feature in the welcome frame; the `zoom` frame carries a percentage). The page is then re-laid-out at `360/scale`
  css px and rendered at that device scale factor, so text stays sharp; tiles stay 360 device pixels wide, and every coordinate
  (page height, layers, regions, taps) is in device pixels at that scale (`page.zoom`, and `page.reset` marks a same-page re-render).
  The view keeps the centre of the window in place; a server without the feature keeps the local scaling.
- **Tabs**: up to 3 views per device over one connection. Every request and page frame carries a view id (`v`); `tab` frames
  (`new`, `close`, `switch`, `list`) answer with a `tabs` list (ids, titles, urls, the active one). Switching re-sends the view in full; the
  tab strip above the address bar shows the tabs. Each tab is its own browser context (the warm pool of 2 is shared by the server and
  refilled whenever a tab is taken); the cookie jar is per device.
- **HTTPS** (`docker compose --profile tls up -d`): a Caddy container (`deploy/Caddyfile`) terminates TLS and proxies HTTPS and
  WebSocket to the web port. For a public host set `PHECLOUD_DOMAIN=cloud.example.org` (DNS pointing here, ports 80/443
  open): Caddy gets and renews a Let's Encrypt certificate by itself. For a LAN use
  `CADDYFILE=./deploy/Caddyfile.local PHECLOUD_DOMAIN=<lan name or ip>`: Caddy's internal CA signs the certificate (trust its root,
  `docker exec phe-caddy cat /data/caddy/pki/authorities/local/root.crt`, or accept the warning). `PHECLOUD_HTTPS_PORT` changes the
  published port (default 443). Keep the phone port (7400, TLS 1.3 with a pinned certificate) as it is; the proxy is only for browsers.
- Tests (`sh web/test/run.sh`): besides the pairing/tap/back/devices/settings/reload path, Playwright covers zoom (hit-test and tap
  under 1.56x, ctrl+wheel), the keyboard overlay (typing and Backspace reach the field on the server), the select chooser, alert,
  confirm (cancel and ok) and prompt dialogs, HTTP auth (a wrong password is asked again, the right one accepted), and, through
  Caddy, pair + tap over HTTPS/wss.
- Smoke test: `sh web/test/run.sh` (Docker, from the repo root) builds everything, starts the server and runs Playwright (Chromium
  in a container) through: pair, key stored in IndexedDB, open example.com, scroll the canvas, tap the link on the canvas,
  assert the title "Example Domains", back button, devices page, settings, reload authenticates from the stored key.
  Screenshots go to `web/test/out/` (a copy of a run is in `web/test/screenshots/`).

## Measured (2026-10-22, round 2: event-driven settle, TLS 1.3, warm tab pool; Ryzen server container, 2 CPUs, loopback)
Bytes are tile payloads (wire = payload plus about 6%); "render" is open-to-done on the server (navigation, settle, screenshots, tiling, encoding). Round 1 (fixed sleeps) in brackets.

| page | page size | tiles (png / jpeg) | tile bytes | wire bytes | regions | render |
|---|---|---|---|---|---|---|
| https://example.com | 360x918 | 4 (4 / 0) | 32 KB | 32.9 KB | 1 | **0.40-0.50 s** [1.3 s] |
| https://en.m.wikipedia.org/wiki/Nokia (redirects to the responsive site) | 360x5576 | 21 (19 / 2) | 446 KB | 474 KB | 253 | 5.1 s [5.4 s] |
| https://news.ycombinator.com | 360x(1793-2048) | 8 (8 / 0) | 303 KB | 330 KB | 230 | 1.5 s [2.2 s] |
| tap "Learn more" on example.com (new page, iana.org) | 360x1303 | 6 (5 / 1) | 59 KB | 62.6 KB | 31 | 1.05 s [1.6 s] |
| type text into an input (layers page) | 360x1711 | 1 changed | 4.8 KB | 8.3 KB | 4 | **0.33 s** [1.0 s] |
| `testdata/layers.html` first load | 360x1711 | 10 + 3 layers | 20.7 KB | 24.8 KB | 4 | 0.45 s [1.4 s] |

Per-update fixed cost is now: Snapshot about 70-110 ms (screenshots + tiling) plus one 60 ms network-quiet window and two animation frames. The rest of a load is Chromium fetching the page; Wikipedia is dominated by about 5000 px of lazy images (the scroll pass only runs when the page has lazy images). Idle server 16 MiB plus Chromium (2 warm tabs).


### Round 5 (progressive load, scroll layers, auth/cert dialogs, byte budget)
Server render with the first screenful streamed first (`done.more`); "first screen" = time until the first `done`, as seen by
the client (round 4 in brackets where measured before: the whole page arrived at once):

| page | first screen | complete | tile bytes |
|---|---|---|---|
| https://example.com | 0.35-0.38 s | 0.43-0.47 s [0.40-0.50 s] | 32 KB |
| Wikipedia, Nokia article (5.3k px) | **1.3 s** [5.1 s] | 5.3 s | 446 KB |
| https://news.ycombinator.com | 1.2 s | 1.65 s [1.5 s] | 303 KB |
| tap link on example.com -> iana.org | 0.9 s | 1.04 s [1.05 s] | 59 KB |
| text into a focused input | 0.31 s | 0.31 s [0.33 s] | 5 KB |

Wikipedia's remaining 4 s are the lazy-image pass over 5000 px (it now runs once per page load, not on every update).
Byte budget (`settings` frame, per device, stored by the server): a gradient test page of 4 tiles is 16.8 KB at JPEG q70 and

### Memory per open tab (round 8; 2 warm tabs in the pool; measured on the server container, pages loaded)
| pages | idle server | after tab 1 | after tab 2 | after tab 3 |
|---|---|---|---|---|
| example.com x3: container (cgroup) | 86 MiB | 116 MiB | 135 MiB | 152 MiB |
| example.com x3: summed Chromium RSS (counts shared pages repeatedly) | 493 MB | 621 MB | 729 MB | 837 MB |
| Wikipedia, Hacker News, Wikipedia: container | 80 MiB | 215 MiB | 223 MiB | 360 MiB |
| the same: summed Chromium RSS | 497 MB | 730 MB | 849 MB | 1072 MB |

A light tab costs about 20-30 MiB of container memory (about 110 MB of RSS, much of it shared); a long Wikipedia page about 135 MiB, Hacker News
about 10 MiB. Budget roughly 150 MiB per open heavy tab, so three tabs of one device are within 0.5 GiB, and the 2 GiB container cap
leaves room for several devices. (`bash client/test/memory.sh [urls]` repeats the measurement.)
8.6 KB at q35 + grayscale.
## Not done yet (next)
- Scopes and token expiry; a web admin/login (passkeys).
- Incremental updates without a full re-screenshot (MutationObserver dirty rects), image/video elements, popups and
  new tabs, pinch/zoom (dpr is fixed at 1).
- Sticky layers: only their natural position at scroll 0 is sent, plus the CSS `top`/`bottom` offset.
- Wide sticky elements (e.g. JSON viewers) are not clamped to the viewport width.

## Layout
`web/` (the Quasar web app), `cmd/phecloud` (server + admin CLI), `cmd/phecloud-client` (test client, `-summary` for machine-readable output), `client/` (the C core), `internal/{proto,store,server,browser,tiles,tlsutil}`.

## Hub (mail, calendar, contacts for old phones; server side of phases P0/P1)
`docker compose --profile hub up -d` (after `sh deploy/hub/hub-init.sh`) adds two AGPL-3.0 servers as separate containers: **Stalwart**
(IMAP, SMTP, CalDAV, CardDAV, JMAP; pinned `v0.16.27`) and **Z-Push 2.7.7** (an Exchange ActiveSync endpoint that reads and writes Stalwart
through IMAP, CalDAV and CardDAV). One instance serves one person: one Stalwart account, and every device gets its own revocable app password
(`phecloud hub apppw add|list|revoke`). Behind Caddy (`--profile tls`, `deploy/hub/Caddyfile.hub`) the endpoints are `/Microsoft-Server-ActiveSync`,
`/dav/...` with the `/.well-known/caldav` and `/.well-known/carddav` redirects, and JMAP. A phone with an old TLS stack cannot use Caddy's TLS 1.2+
listener: use the tunnel (plain HTTP inside it) or the optional TLS 1.0 listener (`--profile hub-legacy`). Tested: 25 of 25 checks (ActiveSync
Provision, FolderSync, Sync of contacts, calendar and mail, SendMail, push; CalDAV/CardDAV with vdirsyncer), about 170 MiB idle.
Licences, exact phone settings, TLS details, numbers and open issues: [docs/hub.md](docs/hub.md).

## License
Apache-2.0, see `LICENSE`.
