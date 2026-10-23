#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
# Android stream test on the Deskmini: a Redroid container and the pheandroid bridge in their OWN network (phepa, containers phe-redroid-t / phe-android-t, so a
# running stack's phe-redroid is not touched), capped, then a Playwright browser. A small loop runs the adb hooks the browser test asks for (settings, rotate, ...).
# Needs: image phe-redroid-native:13 (deploy/android/Dockerfile.redroid-native, see docs/android.md), the scrcpy server jar (JAR=/path), an APK for the upload test
# (APK=/path, any small app), the repo root as cwd.
#   QUERY='&codec=vp8' selects the codec (Playwright's Chromium has no H.264; real browsers do)
set -eu
JAR=${JAR:?path of the scrcpy server jar}
APK=${APK:?path of a small APK for the upload test}
ROOT=$(pwd)
OUT=$ROOT/web/test/out
mkdir -p "$OUT"
rm -f "$OUT"/hook-*
cleanup() { [ -n "${HOOKPID:-}" ] && kill "$HOOKPID" 2>/dev/null || true; docker rm -f phe-redroid-t phe-android-t phe-cloud-t phe-caddy-t >/dev/null 2>&1 || true; docker network rm phepa >/dev/null 2>&1 || true; rm -f "$ROOT/web/test/markor.apk" "$ROOT/web/test/out/Caddyfile.t"; }
cleanup; trap cleanup EXIT
cp "$APK" "$ROOT/web/test/markor.apk"
docker network create phepa >/dev/null
docker run -d --name phe-redroid-t --network phepa --privileged --cpus 2 --memory 3g phe-redroid-native:13 \
    androidboot.redroid_width=720 androidboot.redroid_height=1280 androidboot.redroid_dpi=320 androidboot.use_memfd=1 >/dev/null
docker build -q -f cmd/pheandroid/Dockerfile -t phe-android-bridge:dev . >/dev/null
docker run -d --name phe-android-t --network phepa --cpus 1 --memory 256m -e PHE_ANDROID_SECRET=testsecret -e PHE_ANDROID_SERIAL=phe-redroid-t:5555 \
    -v "$JAR":/scrcpy-server.jar:ro phe-android-bridge:dev >/dev/null
until docker logs phe-android-t 2>&1 | grep -q "server jar pushed"; do sleep 2; done
(
  while true; do
    for f in "$OUT"/hook-*.req; do
      [ -e "$f" ] || continue
      n=${f##*/hook-}; n=${n%.req}
      case $n in
        settings) c='am start -a android.settings.SETTINGS' ;;
        rot1) c='wm user-rotation lock 1' ;;
        rot0) c='wm user-rotation lock 0' ;;
        home) c='input keyevent 3' ;;
        dump) c='uiautomator dump /sdcard/u.xml >/dev/null 2>&1; cat /sdcard/u.xml' ;;
        selectall) c='input keycombination CTRL_LEFT A' ;;
        pkgs) c='pm list packages -3' ;;
        tone) c='input keyevent 24; input keyevent 25' ;;
        *) c='true' ;;
      esac
      rm -f "$f"
      docker exec phe-android-t adb -s phe-redroid-t:5555 shell "$c" > "$OUT/hook-$n.out" 2>&1 || true
      : > "$OUT/hook-$n.done"
    done
    sleep 0.3
  done
) &
HOOKPID=$!
EXP=$(( $(date +%s) + 3000 ))
SIG=$(printf "android.$EXP" | openssl dgst -sha256 -hmac testsecret -hex | sed 's/.*= //')
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network container:phe-android-t --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8090 -e TICKET="$EXP.$SIG" -e QUERY="${QUERY:-}" -e OUT=/out \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node android.mjs' || { docker logs phe-android-t 2>&1 | tail -8; exit 1; }
docker stats --no-stream --format 'redroid {{.CPUPerc}} {{.MemUsage}}' phe-redroid-t
# the web app's own page: phecloud (with the web app) + Caddy in front, as in the hub; the standalone run above has finished, so the single viewer slot is free
docker build -q -t phe-cloud-t:dev . >/dev/null
docker run -d --name phe-cloud-t --network phepa --cpus 1 --memory 512m -e PHECLOUD_ANDROID_SECRET=testsecret phe-cloud-t:dev >/dev/null
cat > "$OUT/Caddyfile.t" <<'EOC'
:8080 {
	handle_path /android/* {
		reverse_proxy phe-android-t:8090 {
			flush_interval -1
		}
	}
	handle {
		reverse_proxy phe-cloud-t:8080
	}
}
EOC
docker run -d --name phe-caddy-t --network phepa --cpus 0.5 --memory 128m -v "$OUT/Caddyfile.t":/etc/caddy/Caddyfile:ro caddy:2 >/dev/null
sleep 8
CODE=$(docker exec phe-cloud-t phecloud paircode --admin 2>/dev/null)
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network container:phe-caddy-t --ipc=host     -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$CODE" -e OUT=/out     mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node androidapp.mjs' || { docker logs phe-android-t 2>&1 | tail -5; docker logs phe-cloud-t 2>&1 | tail -5; exit 1; }
