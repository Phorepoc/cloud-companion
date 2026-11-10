#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# The phone link's cloud browser on the browser container's Firefox (Docker, repo root): the test client pairs over the phone
# port, opens the test site's phone page and taps, types, selects, answers a dialog, follows a link. Tiles: web/test/out/phone-tiles
set -eu
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
export PHECLOUD_BROWSER_PASSWORD=test-browser-password PHECLOUD_BROWSER_SECRET=test-browser-secret
export PHE_BROWSER_IP=${PHE_BROWSER_IP:-$(hostname -I | cut -d' ' -f1)}
DC="docker compose -p phebr -f docker-compose.yml -f docker-compose.build.yml -f web/test/compose.passkey.yml -f docker-compose.browser.yml -f web/test/compose.browser.yml"
$DC down -v >/dev/null 2>&1 || true
$DC up -d ${PHE_UP:---build} phecloud browser site >/dev/null 2>&1
trap '$DC down -v >/dev/null 2>&1' EXIT
for i in $(seq 60); do docker logs phe-cloud-pk 2>&1 | grep -q 'browser' && docker exec phe-browser true 2>/dev/null && break; sleep 1; done
# Firefox up, its extension connected: a view opens (a fresh profile's first start can take a while)
for i in $(seq 40); do
  C=$(docker exec phe-cloud-pk phecloud paircode 2>/dev/null)
  docker exec phe-cloud-pk phecloud-client -addr 127.0.0.1:7400 -code "$C" -state /tmp/ready.json -out /tmp/ready -url http://site/two.html 2>&1 | grep -q 'tiles' && break
  sleep 3
done
CODE=$(docker exec phe-cloud-pk phecloud paircode 2>/dev/null)
docker exec phe-cloud-pk phecloud-client -addr 127.0.0.1:7400 -code "$CODE" -state /tmp/phone.json -out /tmp/tiles \
  -url http://site/phone.html -scenario gaps -tap-first-link -summary | tee "$OUT/phone.log"
# while a phone has views open, the Browser page's picture shows only its own window (the views stay behind it)
CODE=$(docker exec phe-cloud-pk phecloud paircode 2>/dev/null); U="-url http://site/phone.html -url http://site/index.html"
docker exec phe-cloud-pk phecloud-client -addr 127.0.0.1:7400 -code "$CODE" -state /tmp/p2.json -out /tmp/t2 $U $U $U $U $U $U >/dev/null 2>&1 &
sleep 3
docker run --rm -u root --network phebr_default -v "$OUT":/o curlimages/curl:8.11.1 sh -c '
T=$(curl -s -X POST http://browser:8080/api/login -H "Content-Type: application/json" -d "{\"username\":\"t\",\"password\":\"$0\"}" | sed -n "s/.*\"token\":\"\([^\"]*\)\".*/\1/p")
curl -s -o /o/phone-stream.jpg -H "Authorization: Bearer $T" http://browser:8080/api/room/screen/shot.jpg; chmod 644 /o/phone-stream.jpg' "$PHECLOUD_BROWSER_PASSWORD"
wait
rm -rf "$OUT/phone-tiles"; docker cp phe-cloud-pk:/tmp/tiles "$OUT/phone-tiles" >/dev/null
docker logs phe-cloud-pk 2>&1 | grep -iE 'update|error|warn' | tail -25
# a client of its own device (state file $1, tiles $2, log $3), more arguments after
client() {
  st=$1; ot=$2; lg=$3; shift 3
  CODE=$(docker exec phe-cloud-pk phecloud paircode 2>/dev/null)
  docker exec phe-cloud-pk phecloud-client -addr 127.0.0.1:7400 -code "$CODE" -state "$st" -out "$ot" -summary "$@" > "$lg" 2>&1 || true
}
# cookies are per device: alice's device sets one, bob's device does not see it, alice's still does
client /tmp/pa.json /tmp/ta "$OUT/cookie-a.log" -url http://site/cookie.html?alice
client /tmp/pb.json /tmp/tb "$OUT/cookie-b.log" -url http://site/cookie.html
client /tmp/pa.json /tmp/ta2 "$OUT/cookie-a2.log" -url http://site/cookie.html
# a certificate Firefox refuses: the phone is asked, and says continue anyway
closed() { docker logs phe-cloud-pk 2>&1 | grep -c 'device views closed' || true; }
n=$(closed)
client /tmp/pc.json /tmp/tc "$OUT/cert.log" -accept-cert -url https://site-bad/phone.html
# the exception goes with the view: once the server has closed that device's views, opening the page again asks again
for i in $(seq 30); do [ "$(closed)" -gt "$n" ] && break; sleep 1; done
client /tmp/pc.json /tmp/tc2 "$OUT/cert2.log" -accept-cert -url https://site-bad/phone.html
# what the phone saw, step by step (SUMMARY lines of the test client)
fail=0
check() { if grep "^SUMMARY step=$1 " "$OUT/phone.log" | grep -q -- "$2"; then echo "ok - $3"; else echo "not ok - $3"; fail=1; fi; }
check open 'url=https://site/phone.html .*layers=3' 'the page comes as tiles with its fixed, sticky and scroll layers'
check tap 'url=https://site/two.html' 'tapping a link opens it'
check back 'url=https://site/phone.html .*fwd=true' 'Back, with Forward offered'
check text 'fvalue=abc ' 'typing goes into the focused field'
check key 'fvalue=ab ' 'Backspace'
check select 'sel=2 ' 'a select option'
check dialog 'dlgkind=confirm ' "a page's confirm is asked on the phone"
check trust 'title=tap:true' 'a tap is a trusted event (isTrusted)'
check download 'dl=1 dlname=hello.txt' 'a download is reported to the phone'
for f in cookie-a cookie-b cookie-a2 cert cert2; do grep -q '^SUMMARY' "$OUT/$f.log" || { echo "not ok - no summary in $f.log"; fail=1; }; done
if grep '^SUMMARY step=open ' "$OUT/cookie-a.log" | grep -q 'title=cookie:who=alice'; then echo 'ok - alice sets a cookie'; else echo 'not ok - alice sets a cookie'; fail=1; fi
if grep '^SUMMARY step=open ' "$OUT/cookie-b.log" | grep -q 'title=cookie: '; then echo 'ok - another device does not see it'; else echo 'not ok - another device does not see it'; fail=1; fi
if grep '^SUMMARY step=open ' "$OUT/cookie-a2.log" | grep -q 'title=cookie:who=alice'; then echo 'ok - the first device still has it'; else echo 'not ok - the first device still has it'; fail=1; fi
if grep '^SUMMARY step=open ' "$OUT/cert.log" | grep -q 'url=https://site-bad/phone.html .*dlgkind=cert'; then echo 'ok - a refused certificate can be continued'; else echo 'not ok - a refused certificate can be continued'; fail=1; fi
if grep '^SUMMARY step=open ' "$OUT/cert2.log" | grep -q 'url=https://site-bad/phone.html .*dlgkind=cert'; then echo 'ok - the exception ends with the view'; else echo 'not ok - the exception ends with the view'; fail=1; fi
if grep '^SUMMARY' "$OUT/phone.log" | grep -qv ' err=-$'; then echo 'not ok - a step failed'; fail=1; else echo 'ok - no step failed'; fi
exit $fail
