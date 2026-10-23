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
- Not done: audio, clipboard, rotation, multiple viewers, scroll wheel (drag instead).

## Test
`JAR=/path/to/scrcpy-server-v5.0.1 QUERY='&codec=vp8' sh web/test/android.sh` (own Docker network `phepa`, capped containers, cleaned up afterwards): probes the H.264 stream, decodes VP8 on a canvas,
swipes and presses Home through pointer events and checks that the screen changes, prints latency numbers.
