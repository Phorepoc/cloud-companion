#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
#
# Cross-check the C client core against the Go test client on a live server (Docker, run from the repo root):
#   sh client/test/run.sh
# Fetches and builds mbedTLS 3.6 into .deps/ (external dependency, not vendored; first run only), builds + self-tests +
# fuzzes the C core in a gcc container, starts the server, runs the Go client and the C host test (TLS 1.3, pinned certificate,
# non-blocking) on the same pages and scenarios, and compares their SUMMARY lines. Stops the server at the end.
set -eu
ROOT=$(pwd)
RUN=$ROOT/.testrun
UIDGID="$(id -u):$(id -g)"
MBEDTLS_VERSION=${MBEDTLS_VERSION:-3.6.4}
mkdir -p "$RUN"
rm -f "$RUN"/*.sum "$RUN"/*.txt

dgcc() { docker run --rm --name "$1" --cpus 2 --memory 1g --user "$UIDGID" -e HOME=/tmp -v "$ROOT":/src -w /src/client gcc:14 sh -c "$2"; }

if [ ! -f .deps/mbedtls/library/libmbedtls.a ]; then
    echo "== mbedTLS $MBEDTLS_VERSION (external dependency, fetched and built once into .deps/)"
    mkdir -p .deps
    docker run --rm --name phe-cdeps --cpus 2 --memory 1g --user "$UIDGID" -e HOME=/tmp -v "$ROOT":/src -w /src/.deps python:3.13-bookworm sh -c "
        set -e
        rm -rf mbedtls mbedtls-$MBEDTLS_VERSION
        curl -fsSL -o mbedtls.tar.bz2 https://github.com/Mbed-TLS/mbedtls/releases/download/mbedtls-$MBEDTLS_VERSION/mbedtls-$MBEDTLS_VERSION.tar.bz2
        sha256sum mbedtls.tar.bz2
        tar xjf mbedtls.tar.bz2 && mv mbedtls-$MBEDTLS_VERSION mbedtls
        pip install --quiet --user jsonschema jinja2 >/dev/null 2>&1
        make -C mbedtls lib -j2 CFLAGS=-O2 >/dev/null"
fi

echo "== build + selftest + fuzz (gcc container)"
dgcc phe-cbuild 'make clean >/dev/null; make MBEDTLS=/src/.deps/mbedtls all check fuzz >/tmp/m.log 2>&1; rc=$?; grep -v "^cc " /tmp/m.log | grep -v "^mkdir"; exit $rc'

echo "== server"
docker compose up -d --build >/dev/null 2>&1
trap 'docker compose down >/dev/null 2>&1' EXIT
sleep 6

code() { docker exec phe-cloud phecloud paircode 2>/dev/null; }
gorun() { docker run --rm --name phe-client --cpus 1 --memory 512m --user "$UIDGID" --network host -v "$RUN":/w -w /w \
    --entrypoint /usr/local/bin/phecloud-client phe-cloud:dev -addr 127.0.0.1:7400 -state go-state.json -summary -out go-tiles "$@"; }
crun() { docker run --rm --name phe-ctest --cpus 1 --memory 512m --user "$UIDGID" --network host -v "$ROOT":/src -v "$RUN":/w -w /w \
    gcc:14 /src/client/build/host_test -addr 127.0.0.1:7400 -tls -state c-state.txt "$@"; }

LAYERS=$(cat testdata/layers.url)
GAPS=$(cat testdata/gaps.url)
GOCODE=$(code); CCODE=$(code)
rm -f "$RUN/go-state.json" "$RUN/c-state.txt"

echo "== Go client"
gorun -code "$GOCODE" -tap-first-link -url https://example.com | tee "$RUN/go1.txt" | grep -E "^(SUMMARY|paired)" | cut -c1-200 || true
gorun -type hello -url "$LAYERS" | tee "$RUN/go2.txt" | grep -E "^SUMMARY" | cut -c1-120 || true
gorun -url https://en.m.wikipedia.org/wiki/Nokia | tee "$RUN/go3.txt" | grep -E "^SUMMARY" | cut -c1-120 || true
gorun -scenario gaps -url "$GAPS" | tee "$RUN/go4.txt" | grep -E "^(SUMMARY|  dialog)" | cut -c1-120 || true
echo "== C client (TLS 1.3, pinned, non-blocking)"
crun -code "$CCODE" -tap-first-link -url https://example.com | tee "$RUN/c1.txt" | grep -E "^(SUMMARY|authent|TLS|tapping|PASS|FAIL)" | cut -c1-120 || true
crun -type hello -url "$LAYERS" | tee "$RUN/c2.txt" | grep -E "^(SUMMARY|layers|PASS|FAIL)" | cut -c1-120 || true
crun -url https://en.m.wikipedia.org/wiki/Nokia | tee "$RUN/c3.txt" | grep -E "^(SUMMARY|PASS|FAIL)" | cut -c1-120 || true
crun -scenario gaps -url "$GAPS" | tee "$RUN/c4.txt" | grep -E "^(SUMMARY|  dialog|PASS|FAIL)" | cut -c1-120 || true

# field(file, step, name)
field() { grep "^SUMMARY step=$2 " "$1" | head -1 | tr ' ' '\n' | grep "^$3=" | cut -d= -f2-; }
bad=0
cmp_fields() { # n step fields...
    n=$1; step=$2; shift 2
    for f in "$@"; do
        g=$(field "$RUN/go$n.txt" "$step" "$f"); c=$(field "$RUN/c$n.txt" "$step" "$f")
        if [ -z "$g" ] || [ "$g" != "$c" ]; then echo "MISMATCH run $n step $step $f: go=[$g] c=[$c]"; bad=1; else echo "ok   run $n $step $f=$g" | cut -c1-110; fi
    done
}

echo "== certificate pin refusal (state files with a wrong fingerprint)"
sed 's/"fp":"../"fp":"zz/' "$RUN/go-state.json" > "$RUN/bad-state.json"
if gorun -state bad-state.json -url https://example.com > "$RUN/pin.txt" 2>&1; then echo "MISMATCH pin: Go client connected with a wrong pinned fingerprint"; bad=1;
elif grep -q "certificate changed" "$RUN/pin.txt"; then echo "ok   Go client refused: $(grep -o 'server certificate changed.*' "$RUN/pin.txt" | cut -c1-60)...";
else echo "MISMATCH pin: Go client failed for another reason:"; tail -2 "$RUN/pin.txt"; bad=1; fi
sed 's/ [0-9a-f]*$/ 00000000000000000000000000000000000000000000000000000000000000aa/' "$RUN/c-state.txt" > "$RUN/c-bad-state.txt"
if crun -state c-bad-state.txt -url https://example.com > "$RUN/cpin.txt" 2>&1; then echo "MISMATCH pin: C client connected with a wrong pinned fingerprint"; bad=1;
elif grep -q "certificate changed" "$RUN/cpin.txt"; then echo "ok   C client refused: $(grep -o 'server certificate changed.*' "$RUN/cpin.txt" | cut -c1-60)...";
else echo "MISMATCH pin: C client failed for another reason:"; tail -2 "$RUN/cpin.txt"; bad=1; fi

echo "== compare"
cmp_fields 1 open url h tiles tilebytes hx regions layers trunc title back fwd
cmp_fields 1 tap url h regions layers title back fwd
cmp_fields 1 back url regions back fwd
cmp_fields 2 open url h tiles tilebytes hx regions layers trunc
cmp_fields 2 typed url tiles regions layers
cmp_fields 3 open url h tiles tilebytes hx regions layers trunc
cmp_fields 4 open url h tiles tilebytes hx regions layers title back fwd focus opts sel
cmp_fields 4 focus focus ftype fvalue
cmp_fields 4 text focus ftype fvalue
cmp_fields 4 key fvalue
cmp_fields 4 select sel opts
cmp_fields 4 dialog title dialogs dlgkind dlgmsg focus
cmp_fields 4 download dl dlname title
for n in 1 2 3 4; do grep -q "^PASS" "$RUN/c$n.txt" || { echo "C test $n did not PASS"; bad=1; }; done
if [ $bad = 0 ]; then echo "ALL MATCH"; else echo "DIFFERENCES FOUND"; exit 1; fi
