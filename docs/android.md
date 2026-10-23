# Android in the browser (optional)

An Android instance (Redroid, in a container on your server) shown in a page of the web app, with touch and keyboard going back. Apps keep running when the page is closed.

```
browser (WebCodecs) <-- WebSocket: video + input --> pheandroid (bridge) <-- adb --> scrcpy server in the Android container
```

## What you need (nothing of this is shipped in this repository)
- **A Redroid image without any ARM translation layer.** Pull the upstream Redroid image yourself and build ours on top with `deploy/android/Dockerfile.redroid-native`
  (`docker build -t phe-redroid-native:13 -f deploy/android/Dockerfile.redroid-native deploy/android`): it removes the translation libraries and sets the ABI list to `x86_64`, so only
  apps with x86_64 code install. The licence of what the upstream image contains is the operator's to check.
- **The scrcpy server jar** (Apache-2.0): `scrcpy-server-v<version>` from <https://github.com/Genymobile/scrcpy/releases>. The bridge and the jar must be the exact same version
  (`PHE_SCRCPY_VERSION`, default 5.0.1). The jar is mounted into the bridge container, not baked into an image.
- **The `binder_linux` kernel module** on the host (the same requirement as any Redroid).
- **`adb`** (Apache-2.0) comes from Debian inside the bridge image.

## Run it
```
export PHECLOUD_ANDROID_SECRET=$(openssl rand -hex 32)     # shared by phecloud and the bridge; keep it in your env file
export PHE_SCRCPY_JAR=/path/to/scrcpy-server-v5.0.1
docker compose -f docker-compose.yml -f docker-compose.hub.yml -f docker-compose.android.yml --profile hub --profile tls --profile android up -d --build
```
The hub's Caddyfile sends `/android/*` to the bridge. Open **Android** in the web app (admin devices only).
Without a reverse proxy, publish the bridge's port 8090 yourself and open `/?t=<ticket>`; the ticket comes from `GET /phe/android/ticket` (signed, admin device) and is good for two minutes.

## Security
- The Android container is `privileged` (Redroid needs it). Keep it on the internal Docker network; nothing publishes its adb port.
- Only an admin device can get a ticket; the bridge refuses a stream without a valid ticket (HMAC with the shared secret). One viewer at a time: a new one replaces the old.
- A phone on the server holds whatever you sign in to on it. Treat the container's `redroid-data` volume like the data of a phone.

## Behaviour and numbers (Deskmini, Redroid 13, 720x1280, software rendering, no GPU)
Measured with a headless Chromium (it has no H.264 decoder, so the test used VP8 through `&codec=vp8`; every real browser decodes H.264, the default, which the same test checks on the wire:
codec string `avc1.42c029`, key frames start with the parameter sets).
- Video 404x720 at up to 30 fps (the encoder sends nothing for a still screen: 0 to a few fps idle), 100 to 500 kbit/s while scrolling.
- Input to the next video frame arriving at the bridge: about 32 ms median (one frame time at 30 fps; browser decode and paint come on top). Bridge round trip on the same host about 1 ms.
- CPU: the Android container 18 to 65 % of one core while swiping (cap 2 cores), about 4 % idle; the bridge about 1 %. Memory: Android about 770 MiB, bridge 7 MiB.
- Query options of the page: `size` (longest edge, default 720), `fps` (30), `kbps` (2000), `codec` (`h264`, `vp8`, `vp9`).
- Not done: multiple viewers at once, microphone/camera, file download from the phone, pinch zoom.

## Test
`JAR=/path/to/scrcpy-server-v5.0.1 QUERY='&codec=vp8' sh web/test/android.sh` (own Docker network `phepa`, capped containers, cleaned up afterwards): probes the H.264 stream, decodes VP8 on a canvas,
swipes and presses Home through pointer events and checks that the screen changes, prints latency numbers.

## What the page can do
- **Touch and keys**: mouse or touch on the screen; typed characters go to the focused field; Back / Home / Recent buttons.
- **Scroll wheel**: over the screen, scrolls the view under the pointer.
- **Rotation**: the stream follows the device orientation (the canvas switches between portrait and landscape; touches stay aligned). Rotate the phone from its own quick settings or an app.
- **Sound** (button, off by default: a browser plays sound only after a click): the device's audio is captured by the scrcpy server as Opus, decoded with WebCodecs `AudioDecoder` and played with Web Audio (about 60 ms buffer).
  If a device cannot capture audio (older than Android 11, or no audio device) the stream continues without it. `&audio=0` in the page address turns capture off.
- **Clipboard both ways**: *Paste to phone* (or Ctrl+V on the screen) sets the phone's clipboard and pastes; *Copy from phone* (or Ctrl+C / Ctrl+X on the screen) asks the phone to copy and puts the text on this computer's clipboard.
  Browsers allow writing the clipboard only soon after a click or key press, so a copy the phone makes by itself is announced and the button completes it.
- **Install apps**: *Install an APK file* uploads a file (up to 512 MB) and runs `adb install -r -g`; *Install from F-Droid* takes a package name (for example `org.fossify.notes`), downloads the suggested build from f-droid.org
  (the bridge container needs internet access) and installs it. Apps whose suggested build is ARM-only fail with the install error text, since the image has no translation layer. Installing needs the viewer's session (given by the open stream), not just a ticket.

## In the web app
**Android** (admin devices) is a native page of the app, not an embedded page: `web/src/pages/AndroidPage.vue` imports the stream module `web/src/android/stream.js` (framework-free: WebCodecs video and audio,
the WebSocket, pointer / wheel / keyboard / clipboard events, installs). The app fetches the ticket itself (`/phe/android/ticket`), draws into a canvas inside a phone-shaped frame that takes the stream's own aspect ratio
(so it follows rotation) and is as large as the content area allows (desktop, tablet and phone widths), and offers the controls as app buttons: Back, Home, Recent, Sound, Copy from phone, Paste to phone, Install apps (a dialog for an
APK file or an F-Droid package name). A status chip shows Connecting / Connected / the reason it closed (including "Another viewer took over this Android"), and **Reconnect** fetches a fresh ticket.
The page's address accepts `#/android?codec=vp8&size=720&fps=30&kbps=2000&audio=0`. The bridge's standalone page at `/android/` (plain controls, no app around it) uses the same module: `cmd/pheandroid/stream.js` is an
embedded copy, and a Go test fails if the two differ.

## Verified, and what only a real browser can check
**Verified by the user in a real browser:** H.264 decoding (the default codec) and the ticket path from the web app (the stream played). The framing of the page (iframe) was rejected, hence the native page above.
Automated (headless Chromium, `web/test/android.sh`, which runs `android.mjs` against the bridge directly and `androidapp.mjs` against phecloud + Caddy, as in the hub): scroll wheel changes the screen; rotating the device gives a
landscape canvas and back, and a tap in landscape lands correctly; browser clipboard text arrives in a focused text field on the phone (read back from the UI hierarchy) and the phone's selected text comes back to the browser;
APK upload and F-Droid install end in `Success` and the packages are listed; a bad package name is refused; audio: Opus decoded in the browser. The app page is checked at 1280x800 and 390x844, portrait and landscape, light and dark
(screenshots in `web/test/screenshots/android/androidapp-*.png`): the frame has the stream's aspect ratio, nothing scrolls sideways, the whole screen fits on desktop, the Home / swipe controls change the screen, the install dialog
installs an APK, Reconnect brings the stream back, and a second viewer shows the first one "Another viewer took over this Android".
**Not verified (a real browser is needed):** audible output, the browser's clipboard permission prompts, Safari and Firefox.
