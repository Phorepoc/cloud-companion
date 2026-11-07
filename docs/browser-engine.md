# Cloud browser engine: pages as drawing lists

Status: design, for review. Nothing here is built yet.

## Why

The cloud browser today runs the page in Chromium on the server and sends it to the client as image tiles (`proto` frames
`page`, `tile`, `layers`, `regions`). That was the first step. Its limits:

- every change on the page costs whole image tiles; text is pixels, so it cannot be selected, searched, re-flowed or
  read aloud, and is blurry after a zoom until new tiles arrive;
- the client knows nothing about the page beyond tap regions, so every interaction waits for the server;
- the browser behind it was a trimmed, automation-flagged Chromium, which sites treat as a bot.

The goal is what Opera Mini did, and further: the server runs the real browser (scripts, network, layout) and sends the
client a compact description of what to draw; the client draws it with compiled code and handles scrolling, zoom,
selection, find and typing on its own. Unlike Opera Mini the page stays live on the server: changes are sent as small
updates, not as a new snapshot after every tap.

## Pieces

| piece | where | what |
|---|---|---|
| browser | server | full Chromium, an ordinary window on a virtual display, one browser context per view (unchanged sessions) |
| capture | server, `internal/browser` | turns the page into a drawing list (from DevTools `DOMSnapshot.captureSnapshot` with styles, layout and paint order, plus `LayerTree` for scrolling and fixed parts) |
| encoder | server, `internal/draw` (new) | the drawing list in the wire format; diffs against what the client has; resources (fonts, images) once each |
| protocol | `internal/proto` | new frames, behind the feature `draw` the client asks for in `hello` |
| renderer | client, `client/phc` (C) | decodes the list and draws it: the same C code for Symbian (the engine plug-in), WebAssembly for the web app and the desktop, the NDK for the phone |

The tile path stays as the fallback (clients without `draw`, and parts of a page that cannot be described: see below).

## The drawing list

A page is a tree of **nodes**. Each node has a stable id (from the browser's node, kept across updates while the element
lives), a box, and one kind:

| kind | carries |
|---|---|
| `box` | background colour or gradient, border (widths, colours, radii), shadow, opacity, clip |
| `text` | runs: font id, size, colour, decorations, the string, and the x advance of every glyph cluster from the server's layout, so the client never measures or wraps text and lines break exactly as on the server |
| `image` | resource id, the drawn rectangle, the source crop |
| `path` | an SVG/canvas-free vector path (inline SVG icons): commands, fill, stroke |
| `link` | wraps children; the target URL (status line, long-press menu) |
| `field` | a form control: type (text, password, search, checkbox, radio, select, button), value, placeholder, state; the client draws and edits it locally |
| `scroll` | a box that scrolls on its own: its content size and offset; the client scrolls it locally |
| `fixed` | stays put while the page scrolls (`position: fixed` / `sticky`, with its sticky limits) |
| `pixels` | a part that cannot be described (video, canvas, WebGL, plug-ins, cross-origin frames that refuse capture): an id for a pixel stream (the current tile/video path) shown at that box |

Order of drawing is the browser's paint order, flattened into the tree's child order plus a z-index on `fixed` and
`scroll` nodes. Coordinates are CSS pixels as 16.16 fixed point relative to the parent; the client scales.

**Resources** are sent once per connection and referenced by id:

- **fonts**: the subset of glyphs the page uses, as a compact outline font (TrueType `glyf` table in a small container);
  more glyphs are added as needed. Clients with too little memory ask for `fontmode=system` and draw with their own fonts
  at the server's advances.
- **images**: re-encoded for the client at the size they are drawn (WebP or JPEG, PNG for flat art; the client's byte
  budget from `settings` applies), progressive, lazily: only images near the visible part are sent first.

## Updates

The server keeps, per view, the list it last sent. After a change on the page (load, script, input, animation frame
rate-limited to the client's budget) it captures again and sends only the difference:

| op | meaning |
|---|---|
| `add` parent, index, node | a new node (with its subtree) |
| `remove` id | the node and its subtree go |
| `move` id, parent, index | reordered |
| `set` id, fields | changed properties (box, colour, text runs, value, scroll offset...) |

A `commit` closes a batch; the client draws only complete batches. Each batch carries a sequence number; a client that
missed one (reconnect) asks for a full list (`resync`).

CSS animations and transitions that only move or fade a node are sent once as a timeline (`anim` id, property, keyframes,
duration, easing) and run on the client, so they cost no traffic per frame.

## Protocol

Frames on the existing connection (length-prefixed CBOR, `proto.go`), only after the client listed `draw` in `hello`:

| frame | dir | content |
|---|---|---|
| `doc` | S->C | view, sequence, URL, title, history (back/forward), viewport size, the full node tree |
| `ops` | S->C | view, sequence, ops (above); ends with `commit` |
| `font` | S->C | font id, family/weight/style for display, glyph data (more may follow for the same id) |
| `img` | S->C | resource id, format, bytes (or a part of a progressive image) |
| `resync` | C->S | view: send `doc` again |
| `input` | C->S | as today (tap, text, key, select), but aimed at a **node id** and a point inside it, not at screen pixels; the client applies field edits locally first and the server confirms or corrects with `set` |
| `scroll` | C->S | view, node (0 = the page), offset: tells the server where the person is, so it can lazy-load and send images there |
| `find` | C->S | (optional) find in page beyond what the client holds; the client searches the text it has first |

Existing frames keep working: `open`, `navigate`, `tab`, `tabs`, `dialog`, `download`, `zoom`, `viewport`, `settings`.

**Versioning.** The node kinds and their fields are numbered (CBOR integer keys, not names: smaller and stable). Unknown
fields are skipped; an unknown node kind is drawn as its box and children. A client lists in `hello` which kinds it
draws (`draw:1` = the set above); the server sends `pixels` in place of any kind the client did not list. Adding kinds or
fields never breaks an older client.

## The engine API (what Symbian and the other clients see)

The renderer is a C library with no platform calls of its own; the host gives it a socket, a clock, memory limits and a
surface. It sits under the existing Phorepoc web-engine seam (`phewebengine.h`, interface `0xE7016B01`), so the browser
front end does not change: the Cloud Companion engine plug-in gains the capabilities `zoom`, `find`, `select`,
`textcopy` and keeps `script`, `tls13`, `snapshot`, `suspend`, `state`.

```c
/* one per view; all calls on the view's own thread except phd_post_* (queue only, any thread) */
phd_view *phd_new(const phd_host *host, const phd_limits *lim);   /* host: send(), now(), log(); limits: bytes for fonts,
                                                                     images, nodes */
int  phd_feed(phd_view *v, const uint8_t *frame, size_t n);         /* one received frame (doc, ops, font, img) */
int  phd_draw(phd_view *v, phd_surface *s, phd_rect dirty);         /* draws the visible part into a BGRA/RGB565 surface */
void phd_scroll(phd_view *v, int32_t dx, int32_t dy);               /* local; reports to the server when it settles */
void phd_zoom(phd_view *v, int32_t pct, int32_t cx, int32_t cy);    /* local re-layout-free zoom (vector text) */
int  phd_hit(phd_view *v, int32_t x, int32_t y, phd_hit_info *out); /* node, link URL, field: for the status line and
                                                                        long-press */
void phd_post_tap(phd_view *v, int32_t x, int32_t y);
void phd_post_text(phd_view *v, const char *utf8, int submit);
void phd_post_key(phd_view *v, int key);
int  phd_find(phd_view *v, const char *utf8, int dir, phd_rect *out);
int  phd_selection_text(phd_view *v, char *buf, size_t n);
void phd_memory(phd_view *v, int level);                            /* LOW: drop decoded images; CRITICAL: drop fonts too
                                                                       (re-asked on demand) */
void phd_free(phd_view *v);
```

Rules for the API: no global state; every buffer has a caller-given limit; integers in fixed point (no floating point
needed on old ARM); all strings UTF-8; errors are negative return codes with a `phd_strerror`; the header is versioned
(`PHD_API_VERSION`) and only grows. The same header is the WebAssembly export list and the JNI boundary.

## Memory budget (Symbian, x32)

A view must fit in about 3 MB beside the browser front end: node tree (a typical page: 5,000 nodes x ~40 bytes = 200 KB),
fonts (subsets, ~30 KB per face, at most 8 faces), images (encoded ~300 KB, decoded only when visible, an LRU of ~1.5 MB),
the surface. Over the limits, the client tells the server (`settings`) and gets smaller images or system-font mode.

## Order of work

1. Full Chromium on a virtual display, no automation flags (fixes Google and other bot checks; everything else needs a
   real browser underneath).
2. Capture and the drawing list on the server, with a dump tool and tests against the test site (`web/test/site`):
   text, boxes, images, links, fields, scroll boxes, fixed parts.
3. The C renderer (`client/phc/draw`), tested by drawing the dump and comparing with Chromium's own screenshot.
4. The protocol frames and the web client (WebAssembly) behind a setting; then the Symbian engine plug-in.
5. Updates (ops), client-side animations, `pixels` parts for video and canvas.
6. Drop tiles where `draw` works; keep them for old clients.

## Open questions

- Fonts: send outlines (exact, more bytes) or rely on system fonts with server advances (smaller, less exact)? Proposal:
  outlines by default, system fonts as the low-memory mode.
- Scripts that read layout on every frame (infinite scrollers, sticky headers driven by script): rate-limit updates per
  client budget; measure on real sites before tuning.
- Accessibility: the node tree is already what a screen reader needs; expose it through the host later.
