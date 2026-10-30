#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
# Android stream test on a small x86 server: a Redroid container and the pheandroid bridge in their OWN network (phepa, containers phe-redroid-t / phe-android-t, so a
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
cleanup() { [ -n "${HOOKPID:-}" ] && kill "$HOOKPID" 2>/dev/null || true; docker rm -f phe-redroid-t phe-android-t phe-cloud-t phe-caddy-t phe-dockerproxy-t >/dev/null 2>&1 || true; docker network rm phepa >/dev/null 2>&1 || true; rm -f "$ROOT/web/test/markor.apk" "$ROOT/web/test/out/Caddyfile.t"; }
cleanup; trap cleanup EXIT
cp "$APK" "$ROOT/web/test/markor.apk"
docker network create phepa >/dev/null
docker run -d --name phe-redroid-t --network phepa --privileged --cpus 2 --memory 3g ${REDROID_IMAGE:-phe-redroid-native:13-ff} \
    androidboot.redroid_width=720 androidboot.redroid_height=1280 androidboot.redroid_dpi=320 androidboot.use_memfd=1 >/dev/null
docker run -d --name phe-dockerproxy-t --network phepa --cpus 0.25 --memory 64m -e CONTAINERS=1 -e POST=1 -e ALLOW_START=1 -e ALLOW_STOP=1 -v /var/run/docker.sock:/var/run/docker.sock:ro tecnativa/docker-socket-proxy:0.3.0 >/dev/null
docker build -q -f cmd/pheandroid/Dockerfile -t phe-android-bridge:dev . >/dev/null
docker run -d --name phe-android-t --network phepa --cpus 1 --memory 256m -e PHE_ANDROID_SECRET=testsecret -e PHE_ANDROID_SERIAL=phe-redroid-t:5555 \
    -e PHE_ANDROID_DOCKER=http://phe-dockerproxy-t:2375 -e PHE_ANDROID_CONTAINER=phe-redroid-t -e PHE_ANDROID_PARK_AFTER=${PARK_AFTER:-30s} \
    -v "$JAR":/scrcpy-server.jar:ro phe-android-bridge:dev >/dev/null
until docker logs phe-android-t 2>&1 | grep -q "server jar pushed"; do sleep 2; done
(
  while true; do
    for f in "$OUT"/hook-*.req; do
      [ -e "$f" ] || continue
      n=${f##*/hook-}; n=${n%.req}
      case $n in
        ctstate) docker inspect -f '{{.State.Status}}' phe-redroid-t > "$OUT/hook-$n.out" 2>&1 || true; rm -f "$f"; : > "$OUT/hook-$n.done"; continue ;;
        ctmem) docker stats --no-stream --format '{{.MemUsage}}' phe-redroid-t > "$OUT/hook-$n.out" 2>&1 || true; rm -f "$f"; : > "$OUT/hook-$n.done"; continue ;;
        hostmem) free -m | awk '/Mem:/{print $3}' > "$OUT/hook-$n.out"; rm -f "$f"; : > "$OUT/hook-$n.done"; continue ;;
        settings) c='am start -a android.settings.SETTINGS' ;;
        rot1) c='wm user-rotation lock 1' ;;
        rot0) c='wm user-rotation lock 0' ;;
        home) c='input keyevent 3' ;;
        dump) c='uiautomator dump /sdcard/u.xml >/dev/null 2>&1; cat /sdcard/u.xml' ;;
        selectall) c='input keycombination CTRL_LEFT A' ;;
        pkgs) c='pm list packages -3' ;;
        tone) c='input keyevent 24; input keyevent 25' ;;
        wmsize) c='wm size; wm density' ;;
        gohome) c='input keyevent 3; sleep 2; dumpsys window | grep mCurrentFocus | head -1' ;;
        settingsff) c='am start --windowingMode 5 -n com.android.settings/.Settings' ;;
        freeformcount) c='dumpsys activity activities | grep -c mode=freeform' ;;
        taskbarwin) c='dumpsys window windows | grep -c "Window{.* Taskbar}"' ;;
        applist) c='am start -a android.settings.MANAGE_ALL_APPLICATIONS_SETTINGS' ;;
        settingssearch) c='am start -a android.settings.SETTINGS >/dev/null; sleep 2; uiautomator dump /sdcard/u.xml >/dev/null 2>&1; b=$(cat /sdcard/u.xml | tr ">" "\n" | grep -i "Search settings" | head -1 | grep -o "bounds=.\[[0-9]*,[0-9]*\]\[[0-9]*,[0-9]*\]." | tr -dc "0-9,[]"); x1=${b#[}; x1=${x1%%,*}; y1=${b#*,}; y1=${y1%%]*}; x2=${b#*][}; x2=${x2%%,*}; y2=${b##*,}; y2=${y2%]}; input tap $(( (x1+x2)/2 )) $(( (y1+y2)/2 ))' ;;
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
# the display follows the viewer, and the page's own context menu (ONLY=view runs just this)
run_desktop() {
EXP=$(( $(date +%s) + 3000 ))
SIG=$(printf "android.$EXP" | openssl dgst -sha256 -hmac testsecret -hex | sed 's/.*= //')
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network container:phe-android-t --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8090 -e TICKET="$EXP.$SIG" -e QUERY="${QUERY:-}" -e OUT=/out \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning androiddesktop.ts' || { docker logs phe-android-t 2>&1 | tail -12; exit 1; }
}
run_view() {
EXP=$(( $(date +%s) + 3000 ))
SIG=$(printf "android.$EXP" | openssl dgst -sha256 -hmac testsecret -hex | sed 's/.*= //')
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network container:phe-android-t --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8090 -e TICKET="$EXP.$SIG" -e QUERY="${QUERY:-}" -e OUT=/out \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning androidview.ts' || { docker logs phe-android-t 2>&1 | tail -8; exit 1; }
}
if [ "${ONLY:-}" = view ]; then run_view; exit $?; fi
if [ "${ONLY:-}" = desktop ]; then run_desktop; exit $?; fi
if [ "${ONLY:-}" != app ]; then
EXP=$(( $(date +%s) + 3000 ))
SIG=$(printf "android.$EXP" | openssl dgst -sha256 -hmac testsecret -hex | sed 's/.*= //')
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network container:phe-android-t --ipc=host \
    -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8090 -e TICKET="$EXP.$SIG" -e QUERY="${QUERY:-}" -e OUT=/out \
    mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning android.ts' || { docker logs phe-android-t 2>&1 | tail -8; exit 1; }
docker stats --no-stream --format 'redroid {{.CPUPerc}} {{.MemUsage}}' phe-redroid-t
fi
[ "${ONLY:-}" = android ] && exit 0
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
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network container:phe-caddy-t --ipc=host     -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODE="$CODE" -e OUT=/out     mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning androidapp.ts' || { docker logs phe-android-t 2>&1 | tail -5; docker logs phe-cloud-t 2>&1 | tail -5; exit 1; }
[ "${ONLY:-}" = app ] && exit 0   # ONLY=app: just the web app's own Android page
# parking: the standalone page against the bridge again (the app run above has finished); parking after $PARK_AFTER (default 30s here, 15m in production)
EXP=$(( $(date +%s) + 3000 ))
SIG=$(printf "android.$EXP" | openssl dgst -sha256 -hmac testsecret -hex | sed 's/.*= //')
docker run --rm --name phe-playwright --cpus 2 --memory 2g --network container:phe-android-t --ipc=host     -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8090 -e TICKET="$EXP.$SIG" -e QUERY="${QUERY:-}" -e OUT=/out     mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning androidpark.ts' || { docker logs phe-android-t 2>&1 | tail -8; exit 1; }
run_view
run_desktop
