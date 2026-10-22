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
- **Phone protocol** (`internal/proto`): one TCP connection (plain TCP on :7400 for v0, TLS to come), frames =
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

## Measured (2026-10-22, Ryzen server container, 2 CPUs, LAN loopback, cold Chromium tab per connection)
Bytes are tile payloads (wire = payload plus about 6%); "render" is open-to-done on the server: navigation, lazy
pass, screenshots, tiling and encoding.

| page | page size | tiles (png / jpeg) | tile bytes | wire bytes | regions | render |
|---|---|---|---|---|---|---|
| https://example.com | 360x918 | 4 (4 / 0) | 32 KB | 32.9 KB | 1 | 1.25-1.3 s |
| https://en.m.wikipedia.org/wiki/Nokia (redirects to the responsive site) | 360x5576 | 21 (19 / 2) | 446 KB | 474 KB | 253 | 5.4 s |
| https://news.ycombinator.com | 360x(1793-2048) | 8 (8 / 0) | 303 KB | 330 KB | 230 | 2.2 s |
| tap "Learn more" on example.com (new page, iana.org) | 360x1303 | 6 (5 / 1) | 59 KB | 62.6 KB | 31 | 1.6 s |
| type text into an input (layers page) | 360x1711 | 1 changed | 4.8 KB | 8.3 KB | 4 | 1.0 s |
| `testdata/layers.html` first load | 360x1711 | 10 + 3 layers | 20.7 KB | 24.8 KB | 4 | 1.4 s |

Idle server: 16 MiB (plus Chromium once a tab is open). Repeating a load gives the same byte counts (the
rendering is deterministic). Fixed costs per update: about 0.5 s in Snapshot even for a tiny page (the lazy scroll
pass, 100 ms settle, 250 ms + 250 ms in `settle`); these are the first things to tune.

## Not done yet (next)
- TLS on the phone port; scopes and token expiry; a web admin/login (passkeys).
- Incremental updates without a full re-screenshot (MutationObserver dirty rects), image/video elements, popups and
  new tabs, pinch/zoom (dpr is fixed at 1), cookies/session persistence between connections.
- A longer page than 8192 px is truncated (`Truncated` is not yet sent to the client).
- Sticky layers: only their natural position at scroll 0 is sent, plus the CSS `top`/`bottom` offset.
- Tune the fixed per-update delays; keep one warm Chromium tab pool.

## Layout
`cmd/phecloud` (server + admin CLI), `cmd/phecloud-client` (test client), `internal/{proto,store,server,browser,tiles}`.

## License
Apache-2.0, see `LICENSE`.
