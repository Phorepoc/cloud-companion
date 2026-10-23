# Phecloud SMS for Android

An Android app that makes a phone the SMS connector of its own SIM for a Phecloud hub (hub-design.md section 2, `docs/sms.md`):
it uploads the inbox and sent box, sends what the web app queued, and reports sent / delivered / failed back. Apache-2.0,
Kotlin, **no libraries at run time** (platform JobScheduler, SmsManager, HttpURLConnection, org.json). Not derived from any other SMS sync
app.

## What it does
- **Pairing.** Hub address, the certificate fingerprint and a pairing code from `phecloud paircode`. *Fetch fingerprint* shows what
  the server presents; compare it digit by digit with the fingerprint `paircode` printed, then *Pair* (`POST /sms/pair`, the hub
  answers with the device id and key). Afterwards every request is signed with the device key (`X-Phe-Auth`) and the hub certificate must
  match the pin (the pin replaces the host name check). An `http://` hub (LAN / VPN testing only) needs no pin.
- **Sync.** `POST /sms/sync` with the phone's own message ids (`in:<_id>`, `out:<_id>`); a repeat changes nothing on the hub, so the phone
  can always resend. A periodic job (15 min) plus a one-shot job when a message arrives (`SMS_RECEIVED`), when a send report arrives or
  on *Sync now*. On the first run only the newest 500 messages per box are taken.
- **Offline queue.** Read messages and send reports are kept in the app's preferences until the hub confirms a sync; the marks only
  advance over what was read, so nothing is read twice and nothing is lost when the network or the process dies.
- **Outbound.** The hub's `pending` list is handed to `SmsManager` (multipart for long texts), each hub `out_id` once. The sent and
  delivered intents become acks (`sent`, `delivered`, `failed` + reason) that go out with the next sync.
- **Do not sync list.** One number, short code or sender id per line (`BANK`, `22395`, `+49 171 5550100`; numbers match by their last
  nine digits). Excluded messages are never uploaded, and old ones are not uploaded retroactively when a sender is added.

## Build and test (Docker, no host Android SDK)
```
docker build -t phe-android-build android/docker          # SDK 34 + Gradle 8.7 + JDK 17 (1.7 GB, keep it as the cache)
cd android
docker run --rm -v phe-android-gradle:/opt/gradle-home -v "$PWD":/work phe-android-build gradle --no-daemon test           # 11 JVM unit tests
docker run --rm -v phe-android-gradle:/opt/gradle-home -v "$PWD":/work phe-android-build gradle --no-daemon assembleDebug  # app/build/outputs/apk/debug/app-debug.apk
```
(Use `MSYS_NO_PATHCONV=1` and `$(pwd -W)` from Git Bash on Windows.) `app/src/test/.../CoreTest.kt` runs the sync logic (`Core.kt`) against fake
provider, hub and sender: upload once, offline queue across a restart, 500-message batches, first-run limit, exclude list, outbound
once-only handling, acks across restarts. The Android glue (`Android.kt`, `SyncJob.kt`, `MainActivity.kt`) holds no logic.

## Tested on Redroid (Android 13, no SIM)
2026-10-23, `redroid/redroid:13.0.0_64only-240527` on the shared Linux box with a throw-away hub over its self-signed TLS (pinned):
install, grants, signed sync, the hub's queued message arriving on the phone, `SmsManager` failing (no radio: `send failed (32)`), and the
failure showing in the hub's thread (`status: failed`, with the reason) and conversation list. **Not tested:** the pairing screen (the
device id and key were obtained with `POST /sms/pair` and put into the app's preferences with `run-as`); reading the SMS provider (the
shell cannot insert into `content://sms` and Redroid has no default SMS app); real sent and delivered reports (needs a SIM); the
15-minute periodic job and `SMS_RECEIVED` wake-ups.

## Limits and next steps
- Group messages and MMS are not synced. A message the app sends is not written to the phone's own sent box (only the default SMS app does);
  the hub holds it from the ack. If the user later makes it the default SMS app, the sent copy would need the ack's id mapping.
- The device key is in the app's private preferences; Android Keystore wrapping is a follow-up. Cleartext HTTP is allowed in the manifest
  for LAN/VPN testing; release builds should turn it off.
- WorkManager / a foreground service were left out to keep the app dependency-free; Doze can delay the 15-minute job (a received message still
  wakes it at once).
