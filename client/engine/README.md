# Phecloud engine plug-in

`phewebengine_phecloud.dll`: the cloud browser as an engine for the Phorepoc browser, one ECom implementation (uid
`0xE7016B21`) of interface `0xE7016B01` (`phewebengine.h`, version 1). Apache-2.0; it links only mbedTLS, libpng, libjpeg
and the Open C libc (no GPL code).

| file | role |
|---|---|
| `phengine_phc.c` | the engine: `PheWebEngineApi` over the `phc` core (`../phc*.c`) and its mbedTLS 1.3 adapter |
| `phewebengine_phecloud.cpp` | the ECom shell, the trace log (`C:\data\phecloud_trace.txt`) and the entropy source (kernel RNG) |
| `phewebengine_phecloud.rss` | the ECom registration (display name "Phecloud", caps `script,tls13`) |
| `group/phewebengine_phecloud.mmp` | the build description (plug-in, user capabilities, Open C libraries) |

## How it works
- **One thread per view** owns that view's `phc` object and its TLS connection. The seam calls (UI thread) only queue a
  request (load, action, pointer, text, key, dialog reply) or read cached state under a mutex. The thread loops: send queued
  requests, `phc_pump` (non-blocking transport), compose when something changed, wait on the socket (20 ms), idle ping.
- **Connection**: Open C sockets, TLS 1.3 only, the server certificate pinned by SHA-256 (no chain validation: the pin is
  the trust anchor), then `phc_auth` (pairing code the first time, device key afterwards) and `phc_open(url, 360)`.
- **Settings** live in `C:\data\phecloud.ini` (`key=value`): `host`, `port` (7400), `pin` (64 hex), `code` (one-time pairing
  code), `name`; the engine writes back `dev` and `key` after pairing and removes the code. With no `pin` the first
  handshake's fingerprint is stored (trust on first use).
- **Tiles**: each tile is kept as received (PNG or JPEG, about 20 KB) and decoded on demand into BGRA; at most 12 decoded
  tiles are kept. Composition = page tiles at `y - scroll_y`, then fixed, sticky and inner-scroll layers in `(z, id)` order,
  then the tap highlight. A view wider than 360 px centres the page.
- **Scrolling is local** (a drag or arrow keys recompose from cached tiles); 300 ms after it stops the engine reports the
  position to the server so lazy content loads.
- **Input**: pointer down = hit test (link URL for the status line, highlight), up without drag = tap frame; text and
  Enter/Backspace/Tab/arrows go to the focused field (`PHE_EV_FOCUS a=2` asks the frontend for the keyboard).
- **Dialogs** (alert, confirm, prompt, HTTP auth) map to `PHE_EV_DIALOG`; refused downloads show an alert.
- **Memory** (per view, x32): `phc` about 230 KB, receive buffer 192 KB, TLS state, the view buffer (w x h x 4), encoded
  tiles; `MAXV` is 2. `memory_pressure(LOW)` drops decoded tiles; `CRITICAL` (and `view_suspend`) close the connection
  and free the tiles of background views, `view_visible` reconnects and reopens the saved URL.
- Capabilities: script (it runs in the cloud), TLS 1.3, snapshot, suspend, state. Not in v1: zoom, find, downloads,
  private views, the "page continues on the desktop" bar when `trunc` is set.

## Build
It is built by the Phorepoc image builder (`emitter/build_mmp_x32.py phewebengine_phecloud`), which supplies the seam
header, the mbedTLS 3.6 headers and archive (with `-DMBEDTLS_CONFIG_FILE`, TLS 1.3 and PSA on), and the libpng and libjpeg
headers and archives. Result: 512 KB of code, 39 KB bss, 90 imports (euser, efsrv, ecom, libc, libm, libpthread, libz), no
unresolved symbols. Not booted yet.

## Boot test (Phorepoc x86, QEMU)
ROM with the browser frontend and this plug-in only (`NET=1`, the other engines left out), a copy of a fresh disk holding
`C:\data\phecloud.ini` (host, port, pin, one-time pairing code), a phecloud server on the LAN. Results:
- the frontend lists the engine and loads it (`load engine e7016b21 -> 0`);
- TLS 1.3 handshake against the pinned certificate, pairing, then the server logs `device connected` and four tile updates
  (`url=https://example.com/ h=918 tiles=3 + 1, 22 + 10 KB`);
- the page appears in the browser (`docs/shots/engine-example-com.png`), composed from PNG tiles decoded on the guest;
- fixed on the way: a progressive load repeats the `page` frame with the same page id, and the engine must keep the tiles
  then (it cleared them, and the view stayed white).
Not yet exercised in the guest: dragging/scrolling, text input, taps (the regress driver has no input; use the qmon keys
or touch with `make_ekern.sh run`).

Memory per view (x32, computed from the structures; not yet measured in a live heap): `phc` object about 230 KB + receive
buffer 192 KB + TLS state about 40 KB + the view buffer (360x584x4 = 841 KB) + encoded tiles (about 32 KB for
example.com) + up to 12 decoded tiles (368 KB each, 3 were live here = 1.1 MB): about 2.4 MB for a short page, at most
about 7 MB for a long one (12 decoded tiles) before the view buffer. The DLL itself is 512 KB of code, 39 KB bss.

## Input and heap, driven in the guest (Phorepoc x86, QEMU; test site `web/test/site`)
Scripted with the QEMU monitor (absolute pointer drags and taps, `sendkey`), screenshots in `docs/shots/engine-s*.png`:
- **scroll-drag**: a 400 px drag moves the page locally from cached tiles (`engine-s1_scrolled_down.png`), and back;
- **tap on a link** that navigates: "Go to page two" opens page two (`engine-s3_link_page_two.png`);
- **Back** (right soft key): returns to the first page;
- **text input**: a tap on the search field makes the engine raise the focus event, the browser opens its editor
  (`engine-s5_field_focused.png`), "symbian" typed (`engine-s6_typed.png`) and Enter submits the form: the result page
  shows the query (`engine-s7_search_result.png`), Back returns to the form (`engine-s8_back_to_form.png`).

Live heap (`User::AllocSize` on the process heap, logged by the engine as `mem ...`; the heap is shared with the browser):
| moment | allocated |
|---|---|
| view created (browser + view buffer + tile table) | 0.99 MB |
| after TLS + pairing (phc object, receive buffer, TLS state) | 1.48 MB (+0.49 MB) |
| first screen composed (3 decoded tiles) | 2.61 MB (+1.13 MB) |
| page complete (4 tiles) | 2.99 MB |
So one view costs about 2.0 MB over the browser for a short page: 0.49 MB fixed (connection) + 0.37 MB per decoded tile
(at most 12 kept: about 4.4 MB more on a long page) + the encoded tiles (about 10-20 KB each) + the 0.84 MB view buffer
(already in the 0.99 MB above).
