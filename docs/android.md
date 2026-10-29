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

## Behaviour and numbers (a small x86 test server, Redroid 13, 720x1280, software rendering, no GPU)
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

## Parking (saves memory when nobody is looking)
The bridge stops the Android container when no viewer has been connected for `PHE_ANDROID_PARK_AFTER` (default `15m`, `0` turns parking off) and starts it again when the next viewer connects: the page shows
**Starting Android…** while the bridge starts the container and waits for `sys.boot_completed` (it polls, no fixed sleep), pushes the scrcpy server and begins streaming. The data volume is kept, so installed apps and their data
survive; running apps and their in-memory state do not (a stopped phone), so nothing keeps running in the background while parked (no music, no downloads). `PHE_ANDROID_PARK=pause` freezes the container instead
(resume is instant, but its memory stays in use).
The bridge never gets the Docker socket. `docker-compose.android.yml` runs `tecnativa/docker-socket-proxy` (Apache-2.0) that allows container inspect / start / stop / pause and nothing else (no create, exec, images or volumes),
and the bridge talks to that (`PHE_ANDROID_DOCKER`, `PHE_ANDROID_CONTAINER`). Without `PHE_ANDROID_DOCKER` there is no parking and the container runs all the time.
Measured on a small x86 test server (Redroid 13, 2 cores, one run): the container held about 730 MiB while a viewer was connected and 0 once stopped (the host's overall "used" figure moved by more than that because other things were running; the
container figure is the one to trust); it was parked 46 s after the viewer left with `PHE_ANDROID_PARK_AFTER=30s` (the check runs every 10 s and the stop takes a few seconds); the cold start, from opening the page to the first picture, took 13.8 s,
and the page said "Starting Android…" for it. Test: `web/test/androidpark.ts` (run by `web/test/android.sh`).

## The display follows the viewing device
The page tells the bridge the size of its window and what kind of device it is (`kind=phone` when the screen's short edge is under 600 CSS pixels, else `large`; `&kind=phone|large` in the address overrides). The bridge sets the main display with
`wm size` / `wm density` before it starts streaming, and again, debounced (700 ms, only for changes of more than 5 %), when the window is resized; the scrcpy server follows the change (new session packet and video configuration), and the page
resizes its canvas and decoder. **Phone**: short edge 720, long edge from the window's aspect ratio (1.4 to 2.2), 320 dpi. **Large** (tablet, laptop, desktop): long edge up to `PHE_ANDROID_MAX_DISPLAY` (1600), aspect from the window (1.2 to 1.9),
213 dpi (about 1200 dp wide, so apps choose their tablet layouts), streamed at up to 1280 on the long edge unless `&size=` says more. Measured (Redroid 13): a 1280x800 window gives a 1600x1008 display, a 390x844 window a 720x1552 one, and back,
each within a few seconds; the stream shape follows. A larger display costs more CPU (software rendering and encoding): lower `PHE_ANDROID_MAX_DISPLAY` on a small host. The override stays in Android until the next viewer sets its own.
`wm size` was chosen over scrcpy's virtual display (`new_display`, 3.x): it is one adb command, needs no extra windowing, and rotation and apps behave as on a real tablet; a virtual display would let several viewers each have their own size
(not needed: one viewer at a time) but starts with an empty display that needs its own launcher.

## Context menu (right click)
Right-clicking the screen opens the page's own menu (not the browser's): **Copy** and **Cut** when something is selected, **Paste** and **Select all** when a text field has focus, always **Back**, **Home**, **Recent apps**. Android's state
comes from `dumpsys input_method` on the bridge (about 0.2 s, asked for when the menu opens): the served input connection is an `EditableInputConnection` while a text field is focused, and `mCursorSelStart` / `mCursorSelEnd` give the selection.
Cheaper signals (scrcpy's clipboard messages) do not say whether anything is selected; uiautomator dumps are slower and do not show selections. Cut and Copy use scrcpy's copy / cut keys; Select all sends Ctrl+A.
Long-pressing on a touch screen still works as on a phone (it is sent through as a touch). Test: `web/test/androidview.ts` (`ONLY=view sh web/test/android.sh`).

## Desktop mode (freeform windows and a taskbar)
A **large** viewer (a tablet, laptop or desktop window; see "The display follows the viewing device") gets a desktop: [Taskbar](https://github.com/farmerbb/Taskbar) as the home app, which draws a taskbar with a start menu, a clock
and app buttons, and apps opening in **freeform windows** (movable, overlapping, with maximize and close buttons). A **phone** viewer gets the stock launcher back. The switch follows the viewer, also when the window is resized
across the phone / large boundary (`web/test/screenshots/android/androiddesktop-*.png`).
- **Licence:** Taskbar is Apache-2.0 (checked on F-Droid and GitHub: Braden Farmer, https://github.com/farmerbb/Taskbar). It is not shipped here: the bridge installs it from f-droid.org the first time a desktop viewer connects.
- **What it needs:** an Android image that declares the freeform window feature (`deploy/android/Dockerfile.redroid-native`, `FREEFORM=1`, now the default: one xml file in `/system/etc/permissions`) and a rootable (userdebug) build, which Redroid is.
  Without either, the display still follows the viewer and the stock launcher stays (the bridge logs why).
- **How the bridge does it** (`cmd/pheandroid/desktop.go`): `adb root`; install Taskbar if missing; `settings put global enable_freeform_support 1` and `force_resizable_activities 1`; grant it the overlay and usage-access app ops;
  write its preference file `shared_prefs/com.farmerbb.taskbar_preferences.xml` as root (`launcher`, `freeform_hack`, `taskbar_active`, ...) with the app's owner; `pm enable` its `HomeActivity`; `cmd package set-home-activity` to Taskbar,
  start it, press Home. For a phone viewer: broadcast `com.farmerbb.taskbar.QUIT`, force-stop it, set the stock launcher (`com.android.launcher3/.uioverrides.QuickstepLauncher`) as home. The provisioning runs once per device start.
- **Apps open freeform** from the Taskbar start menu; from adb use `am start --windowingMode 5 -n <activity>`. Measured on a throwaway container: Taskbar is home about 2 s after the viewer's display is set
  (after the first-time install from F-Droid, a few more seconds), the first freeform window opens at once. Test: `web/test/androiddesktop.ts` (`ONLY=desktop sh web/test/android.sh`).
- Redroid 14 and 15 images exist (`redroid/redroid:14.0.0-latest`, `15.0.0-latest`) but were not needed: Android 13 already provides the windowing.
- **Limits:** the phone's own launcher state (widgets, home layout) is separate from Taskbar's; a window left open in desktop mode stays in the task list when a phone viewer connects; one viewer at a time, so two viewers of different kinds take turns.

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
**Verified by the user in a real browser:** H.264 decoding (the default codec), the ticket path from the web app (the stream played), **audible sound** through the Sound button, and a YouTube video playing smoothly in Android's browser through the stream. **Copying text from Android to the host's clipboard works** (checked by the user). The framing of the page (iframe) was rejected, hence the native page above.
Automated (headless Chromium, `web/test/android.sh`, which runs `android.ts` against the bridge directly and `androidapp.ts` against phecloud + Caddy, as in the hub): scroll wheel changes the screen; rotating the device gives a
landscape canvas and back, and a tap in landscape lands correctly; browser clipboard text arrives in a focused text field on the phone (read back from the UI hierarchy) and the phone's selected text comes back to the browser;
APK upload and F-Droid install end in `Success` and the packages are listed; a bad package name is refused; audio: Opus decoded in the browser. The app page is checked at 1280x800 and 390x844, portrait and landscape, light and dark
(screenshots in `web/test/screenshots/android/androidapp-*.png`): the frame has the stream's aspect ratio, nothing scrolls sideways, the whole screen fits on desktop, the Home / swipe controls change the screen, the install dialog
installs an APK, Reconnect brings the stream back, and a second viewer shows the first one "Another viewer took over this Android".
**Not verified (a real browser is needed):** pasting from the host into Android (the prompts of the browser's clipboard permission), Safari and Firefox.
