# From the C core to a phewebengine plug-in (design note)

Status: design, written against `phewebengine.h` (interface version 1, `PHE_WEBENGINE_VERSION 1`) and the ECom wrapper
`phewebengine_ecom.h`. The C core (`phc*.c`) is done and tested on a host; nothing here has run on a phone yet.

## What exists
`phc` (C99, no malloc, no OS calls) speaks the phecloud phone protocol and keeps the state of one page:
- framing (4-byte length + CBOR), a tiny CBOR reader/writer, SHA-256/HMAC, pairing and HMAC authentication;
- the tile cache (layer, tile x/y, pixel hash), the fixed/sticky layer list, the tap-region list, the last `page`/`done`;
- `phc_hit_test` (screen point -> layer -> smallest tap region) and the input frames (`tap`, `text`, `scroll`, `navigate`).

It does not draw, decode images, open sockets or run TLS. The host gives it a transport (`send`/`recv`) and two callbacks
(`on_page`, `on_tile`; the tile bytes are valid only during the call). Memory: one `phc` object is about 230 KB on a
32-bit target with the default limits (1024 tiles, 1536 regions, 96 KB string pool; all `#define`s; 267 KB measured on x86-64, where pointers are twice as big), plus a caller-owned
receive buffer (192 KB holds the largest tile frame). Tests: `client/test/selftest.c` (offline vectors and a canned
conversation) and `client/test/run.sh` (host test against a live server, compared with the Go client: same tile count,
tile bytes, XOR of tile pixel hashes, regions and layers).

## The plug-in shape
One ECom DLL under interface `0xE7016B01` (its own implementation UID), `Api()` returns a `PheWebEngineApi` named
`"Cloud Companion"`. Phorepoc Browser stays unchanged; the user picks "Cloud Companion" in the engine list like any other engine, and
the package comes from the app store. Caps: `PHE_CAP_TLS13` (the link is TLS 1.3), `PHE_CAP_SCRIPT` (scripts run in the
cloud, so the page is fully scripted), `PHE_CAP_SNAPSHOT` (the first tile scaled down is a free tab thumbnail),
`PHE_CAP_SUSPEND` and `PHE_CAP_STATE`. Not in v1: zoom, find, downloads, private views (a cloud session is not private
from the server; do not advertise it).

### Threads
The seam says all calls come from the UI thread and must not block, and that `notify` may be called from any thread.
- **UI thread**: the `PheWebEngineApi` functions only put a small request on a mutex-protected queue (load, command,
  scroll report, tap, text) and read cached state; `poll()` drains the event queue.
- **Network thread** (one per engine, one connection per view or one shared connection with a view id in a later protocol
  version; v1 is one connection per view because the server maps a connection to a tab): connects, runs the TLS 1.3 handshake
  (mbedTLS, certificate pinned by SHA-256 fingerprint exactly as the Go client does), `phc_auth`, then loops
  `phc_pump`. Requests from the queue are sent between pumps (`phc_pump` returns after every `done`; with a transport
  that returns `PHC_WOULDBLOCK` between frames, one thread can also wait on a socket and a queue event at once).
- **Decode worker** (optional second thread): turns encoded tiles into bitmaps so a 30 ms JPEG decode never stalls the pump.
`notify(ctx, view)` is called from the network/decode thread when an event is queued or pixels changed.

## Pixels, and local scrolling over cached tiles
`lock_pixels` returns the view buffer (BGRA, premultiplied, `view_w x view_h`); the engine owns it and composes into it.
- **Tile store**: key (layer, x, y), value = the encoded bytes (about 20 KB per 360x256 tile, 446 KB for a long Wikipedia
  page) and, when decoded, a bitmap. A decoded 360x256 tile is 368 KB at 32 bpp, so a 5500 px page would be 8 MB and a
  full 8192 px page 12 MB: keep encoded data always, decode on arrival only for the first three screens and for tiles inside
  the scroll window plus one tile of read-ahead in the scroll direction, LRU for the rest (a handful of bitmaps; 16 bpp
  halves it). M0 measured 84 ms for a 360x640 JPEG decode through the platform decoder, which is about 30 ms per tile, so
  read-ahead has to run one tile ahead of a fling, not on demand. The `codecpack` repo is the place to look for a smaller
  PNG/JPEG decoder if the platform one is too slow.
- **Composition** (on `PHE_EV_REPAINT` and when the scroll position changes): fill the buffer, blit the decoded tiles of
  layer 0 that intersect `[scroll_y, scroll_y + view_h)` at `y - scroll_y` (a checkerboard or the page background where a tile
  has not arrived), then draw layers in `(z, id)` order: a fixed layer at `(x, y)`, a sticky layer at
  `phc_layer_screen_y()` (which clamps to its `top`/`bottom` offset), each from its own tiles in the same store.
  Then the tap-feedback highlight.
- **Scrolling is local**: `scroll(v, dx, dy)` clamps `scroll_y` to `[0, page.h - view_h]` and recomposes; there is no network
  round trip (flings run at the blit rate, about 60 fps full screen on the M0 device). When scrolling stops (debounced
  about 300 ms) the engine sends `phc_scroll(y)` so the server can load lazy content and extend the page; the server then
  sends only the changed tiles. `get_state` reports `scroll_y`, `content_h = page.h`, `zoom_permille = 1000`.
- **Truncation**: when `page.trunc` is set the engine shows a "page continues on the desktop" bar at the end of the content.

## Tap: pointer -> region -> input frame
- `pointer(DOWN)`: `phc_hit_test` at the point; if a region is hit, remember it, draw the highlight at once (this is the
  local feedback that hides the server round trip), set `PHE_STR_FOCUS_URL` to `region->href` for links.
- `pointer(MOVE)` beyond the touch slop: cancel the tap and scroll locally.
- `pointer(UP)` without a cancel: `phc_tap_screen(c, scroll_y, view_h, x, y, &hit)` builds the `input` frame. For a point in a
  fixed layer it carries `(layer, x, y)` in layer coordinates; for the page or a sticky layer it carries document coordinates
  `(0, x, y + scroll_y)` plus `sy`. The server clicks in Chromium and answers with changed tiles, a new `layers`/`regions`
  list, and, if the URL changed, a new `page` (new `pid`, so the cache is dropped).
- Text input: when the hit region is `PHC_REGION_INPUT`/`TEXTAREA`, raise `PHE_EV_FOCUS a = 2` so the frontend shows the
  keyboard and remember the region id; `text(v, utf16, len)` converts to UTF-8 and calls `phc_text(id, text, 0)`; the Enter
  key (`key()`) calls `phc_text(id, "", 1)` to submit. Passwords: the server never sends the value back.
- `PHE_CMD_ACTIVATE` (D-pad focus) = tap at the centre of the focused region.

## Navigation and events
| API | core call |
|---|---|
| `load` | `phc_open(url, 360)` on the first load of a view, `phc_navigate(url)` afterwards (UTF-16 to UTF-8 first) |
| `cmd BACK/FORWARD/RELOAD` | `phc_navigate_action("back" / "forward" / "reload")` |
| `view_create/resize` | store `view_w/h`; `phc_open` width stays 360 in v1 (a different width needs a new `open`) |
| `view_destroy` / `view_suspend` | close the connection (the server closes the tab); `view_save_state` stores url + scroll_y; resume reconnects and re-opens, cookies come back from the per-device jar |
| `memory_pressure` | LOW: drop decoded bitmaps; CRITICAL: also drop the encoded tiles of background views |

Event mapping: request sent -> `PHE_EV_START` (a = 0) + `PHE_EV_PROGRESS` 10; `on_page` -> `PHE_EV_COMMIT` when `pid`
changed, `PHE_EV_TITLE`; `done` -> `PHE_EV_PROGRESS` 100, `PHE_EV_FINISHED`, `PHE_EV_REPAINT`; a server `error` frame or a lost
connection -> `PHE_EV_FAILED` (with text) or `PHE_EV_CRASHED` for a protocol error. The security level is `PHE_SEC_SECURE`
when the page URL is https, which only means the cloud validated the site's certificate; say so in the UI.

## Transport, pairing and storage
- mbedTLS 3.x, TLS 1.3 only, no certificate chain: one callback compares the SHA-256 of the server certificate with the pinned
  fingerprint (the same rule as the Go client: pin at pairing, refuse a changed certificate).
- Pairing UI lives in the phebrowser settings (engine page for Cloud Companion): host:port, the 8-character code from
  `phecloud paircode` (the fingerprint is printed next to it; show it so the user can compare), then `phc_auth(code)`.
- Credentials (device id, 32-byte key, fingerprint, host) go in the engine's private directory on C: (a 100-byte file);
  the key is the only secret on the phone, and `phecloud revoke <id>` kills it server-side.
- The network interface comes from `set_iap` (an RConnection / Open C interface name); WiFi power save showed no first-packet
  penalty in M0, so no keep-alive is needed beyond `phc_ping` every 30 s when idle.

## Build
The core is plain C99 with only `memcpy/memset/memcmp/strlen/strncpy` from libc: build it as a static library with the modern GCC
toolchain (`-std=c99`), link it into the plug-in DLL (C++ glue calls it through `extern "C"`; `phc.h` already has the guards). The
plug-in DLL has no writable static data issue if the `phc` object is heap-allocated once per view (`new` in the glue, 230 KB).
Stack use is small; the one recursion (`phc_r_skip`) now has a depth limit of 16 (see "Robustness" below), which was the
a requirement for the first on-device run.

## Protocol additions (round 4) and how the plug-in uses them
These frames are implemented in the server, the Go client and the C core, and cross-checked by `client/test/run.sh`.
- **History** (`page.back`, `page.fwd`, plus `title`/`url` on every update): drives `PheViewState.can_back/can_forward`
  and `PHE_EV_HISTORY` / `PHE_EV_TITLE`; `phc_navigate_action("back"|"forward"|"reload")` is `PHE_CMD_BACK/FORWARD/RELOAD`.
- **Focus** (`focus` frame after every update: region id, kind none/text/textarea/select, HTML input type, value, caret
  start/end, inputmode, enterkeyhint; the value is empty for password fields): the plug-in raises `PHE_EV_FOCUS a = 2` when
  `kind` is text/textarea so the frontend opens its own keyboard, and picks the keyboard layout from `type`/`inputmode`
  (email, url, tel, number, search). Typing is `phc_text(c, 0, utf8, 0)` (into the focused field, at the caret),
  `phc_key(c, "Backspace" | "Enter" | "Delete" | "Tab" | "Arrow*" | "Escape")` for the editing keys, `phc_text_ex(.., replace=1)`
  to replace the whole content. The caret and the typed text come back as tiles and as `focus.value`, so the phone needs no
  text widget of its own for the page.
- **Select** (region `opts`: label and selected flag, at most 100 per select): the plug-in shows a native list for a tap on a
  `PHC_REGION_SELECT` region and answers with `phc_select(c, region_id, index)`; the page sees `input`/`change` events.
- **Dialogs** (`dialog` frame: id, kind alert/confirm/prompt/beforeunload, message, default; reply `dialogreply`): the page is
  blocked until the reply (the server dismisses after 60 s, or at once if no handler is installed). `on_dialog` maps to
  `PHE_EV_DIALOG` (`PHE_DLG_ALERT/CONFIRM/PROMPT`; beforeunload as CONFIRM) and `dialog_reply` to `phc_dialog_reply`.
  The server reads frames on a separate goroutine so the reply is processed while the request that opened the dialog waits.
- **Downloads** are refused in the cloud (browser-level `Browser.setDownloadBehavior = deny`); the `download` frame
  (`url`, `name`, `refused`) lets the plug-in show "downloads are not supported by the cloud browser". A later version could
  stream the file to the phone as a download (`PHE_EV_DOWNLOAD`) once there is a channel for bulk data.
- **TLS**: `phc_mbedtls.[ch]` is the mbedTLS 3.6 adapter (TLS 1.3 only, certificate pinned by SHA-256, non-blocking with an
  optional wait hook; tested against the real listener). The phone needs `mbedtls_hardware_poll()` for entropy and should set
  `MBEDTLS_MEMORY_BUFFER_ALLOC_C` with a static pool so mbedTLS does not touch the system heap.
- **Robustness**: the CBOR skipper has a nesting limit (16) and rejects array/map counts larger than the remaining bytes;
  `client/test/selftest.c` feeds every truncation of a canned conversation, thousands of byte-flipped copies, random garbage
  frames, a 200-deep nesting and absurd lengths through the decoder under AddressSanitizer + UBSan (`make fuzz`).

## Round 5 additions
- **Progressive load**: `done.more` = 1 means "the first screenful is in, another `done` follows". `phc_pump` returns at every `done`, so the
  plug-in paints after the first one (`PHE_EV_REPAINT`, then `PHE_EV_PROGRESS` 50) and calls `phc_pump` again for the rest (`FINISHED`
  at the last). Measured on the host: Wikipedia's first screen after 1.3 s instead of 5.3 s.
- **Scroll layers** (`PHC_LAYER_SCROLL`): an inner `overflow:auto` box. `x,y,w,h` is the visible box in document coordinates (it moves with
  the page), `cw x ch` the content the layer tiles cover, `scroll_x/scroll_y` the current local scroll (`phc_layer_scroll`). Compose the
  box by blitting its tiles at `(x - scroll_x, y - scroll_y - page_scroll)` clipped to the box; a touch that starts inside it scrolls the
  box, not the page, until the box hits its end. Regions inside it use content coordinates; `phc_hit_test` converts, and the tap goes out as
  `(layer, content x, content y)` (the server scrolls the real box there and clicks).
- **Dialog kinds** `PHC_DLG_AUTH` (reply with `phc_dialog_reply_auth(user, pass)`; map to `PHE_DLG_AUTH`) and `PHC_DLG_CERT` (a certificate
  error on a main-frame load; `ok` = load once anyway; map to `PHE_DLG_CERT`, default to "no").
- **Byte budget**: `phc_settings(jpeg_quality, gray)` (stored per device by the server; send it after connecting, e.g. from a "data saver"
  setting; q35 + grayscale halves the bytes of photo tiles).
- **Layout width** is always exactly 360 css px; wide fixed/sticky layers are clamped to the viewport.

## Still open
1. **Multi-step auth flows** (retry on a wrong password, client certificates).
2. **Date/time pickers and file inputs**: the region reports the input type; the picker and the upload reply are not defined yet.
3. **Zoom**: v1 is fixed 360 px layout. Pinch can map to a new `open`/`resize` with a different width (re-render) or to local
   scaling of the cached tiles for the gesture, committing a re-render afterwards.
4. **Find in page, long-press context menu, stop, favicon, new-view requests, inner scroll containers** (`overflow:scroll` boxes
   scroll inside their own layer; the cloud currently renders them as part of the page).
5. **Several views on one connection**: add a view id to every frame so a browser with five tabs uses one TLS connection.
6. **Bandwidth**: tiles are lossless PNG for text; a paletted-PNG decoder is the cheapest path, and the server may later send a
   zstd/deflate-compressed tile stream if the decoder is a bottleneck.

## Round 8 additions: server-side zoom and views
- **Zoom**: `welcome.features` lists `zoom` (`c->feat_zoom`). `phc_zoom(c, pct)` asks for a render scale (100, 125, 150, 200, 250 or 300); the
  answer is the usual page + tiles + regions with `page.zoom = pct` and `page.reset = 1` (the core drops its tile cache). All geometry is in
  device pixels at that scale, so the plug-in's pinch maps to: scale the cached tiles during the gesture, then `phc_zoom` with the nearest
  ladder value, then keep the window centre (`pct_new / pct_old` times the old centre) when the new tiles arrive. `PHE_CAP_ZOOM` can now be
  advertised when the feature is present; `zoom_permille` = `page.zoom * 10 * local factor`.
- **Views**: `phc_tab(c, "new" | "close" | "switch" | "list", v)` (answer: `phc_pump_tabs`, which returns after a `tabs` frame; `c->tabs[]`,
  `c->active_tab`), `phc_set_view(c, v)` chooses the view later requests are about; frames of other views are ignored. Map views to
  `PheView` ids (one connection, `max_views = 3`); `view_create` = tab new + open, `view_destroy` = tab close, switching the visible tab =
  `switch` (the server re-sends the page; the plug-in can keep the old bitmaps meanwhile and discard them at the first new tile). The server
  sends the `tabs` list before the `done` of an update whose title/url changed, so a step that ends at `done` has already consumed it.
