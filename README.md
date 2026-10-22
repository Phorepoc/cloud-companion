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

## Not done yet (next)
- Scopes and token expiry; a web admin/login (passkeys).
- Incremental updates without a full re-screenshot (MutationObserver dirty rects), image/video elements, popups and
  new tabs, pinch/zoom (dpr is fixed at 1).
- Sticky layers: only their natural position at scroll 0 is sent, plus the CSS `top`/`bottom` offset.
- Wide sticky elements (e.g. JSON viewers) are not clamped to the viewport width.

## Layout
`cmd/phecloud` (server + admin CLI), `cmd/phecloud-client` (test client, `-summary` for machine-readable output), `client/` (the C core), `internal/{proto,store,server,browser,tiles,tlsutil}`.

## License
Apache-2.0, see `LICENSE`.
