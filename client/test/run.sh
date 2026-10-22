#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
#
# Cross-check the C client core against the Go test client on a live server (Docker, run from the repo root):
#   sh client/test/run.sh
# Builds + selftests the C core in a gcc container, starts the server with the development-only plain listener (:7401),
# runs the Go client (TLS) and the C host test (plain) on the same pages, and compares their SUMMARY lines
# (tile count, tile bytes, XOR of tile pixel hashes, regions, layers, ...). Stops the server at the end.
set -eu
ROOT=$(pwd)
RUN=$ROOT/.testrun
UIDGID="$(id -u):$(id -g)"
mkdir -p "$RUN"
rm -f "$RUN"/*.sum "$RUN"/*.txt

echo "== build + selftest (gcc container)"
docker run --rm --name phe-cbuild --cpus 2 --memory 1g --user "$UIDGID" -v "$ROOT":/src -w /src/client -e HOME=/tmp \
    gcc:14 sh -c 'make clean >/dev/null && make all check'

echo "== server"
docker compose -f docker-compose.yml -f client/test/compose.plain.yml up -d --build >/dev/null 2>&1
trap 'docker compose -f docker-compose.yml -f client/test/compose.plain.yml down >/dev/null 2>&1' EXIT
sleep 6

code() { docker exec phe-cloud phecloud paircode 2>/dev/null; }
gorun() { docker run --rm --name phe-client --cpus 1 --memory 512m --user "$UIDGID" --network host -v "$RUN":/w -w /w \
    --entrypoint /usr/local/bin/phecloud-client phe-cloud:dev -addr 127.0.0.1:7400 -state go-state.json -summary -out go-tiles "$@"; }
crun() { docker run --rm --name phe-ctest --cpus 1 --memory 512m --user "$UIDGID" --network host -v "$ROOT":/src -v "$RUN":/w -w /w \
    gcc:14 /src/client/build/host_test -addr 127.0.0.1:7401 -state c-state.txt "$@"; }

LAYERS=$(cat testdata/layers.url)
GOCODE=$(code); CCODE=$(code)
rm -f "$RUN/go-state.json" "$RUN/c-state.txt"

echo "== Go client"
gorun -code "$GOCODE" -tap-first-link -url https://example.com | tee "$RUN/go1.txt" | grep -E "^(SUMMARY|paired)" || true
gorun -type hello -url "$LAYERS" | tee "$RUN/go2.txt" | grep -E "^SUMMARY" || true
gorun -url https://en.m.wikipedia.org/wiki/Nokia | tee "$RUN/go3.txt" | grep -E "^SUMMARY" || true
echo "== C client"
crun -code "$CCODE" -tap-first-link -url https://example.com | tee "$RUN/c1.txt" | grep -E "^(SUMMARY|authent|tapping|PASS|FAIL)" || true
crun -type hello -url "$LAYERS" | tee "$RUN/c2.txt" | grep -E "^(SUMMARY|layers|PASS|FAIL)" || true
crun -url https://en.m.wikipedia.org/wiki/Nokia | tee "$RUN/c3.txt" | grep -E "^(SUMMARY|PASS|FAIL)" || true

# field(file, step, name)
field() { grep "^SUMMARY step=$2 " "$1" | head -1 | tr ' ' '\n' | grep "^$3=" | cut -d= -f2-; }
bad=${pinbad:-0}
cmp_fields() { # n step fields...
    n=$1; step=$2; shift 2
    for f in "$@"; do
        g=$(field "$RUN/go$n.txt" "$step" "$f"); c=$(field "$RUN/c$n.txt" "$step" "$f")
        if [ -z "$g" ] || [ "$g" != "$c" ]; then echo "MISMATCH run $n step $step $f: go=[$g] c=[$c]"; bad=1; else echo "ok   run $n $step $f=$g" | cut -c1-110; fi
    done
}
echo "== certificate pin refusal (state file with a wrong fingerprint)"
sed 's/"fp":"../"fp":"zz/' "$RUN/go-state.json" > "$RUN/bad-state.json"
if gorun -state bad-state.json -url https://example.com > "$RUN/pin.txt" 2>&1; then echo "MISMATCH pin: connected with a wrong pinned fingerprint"; pinbad=1;
elif grep -q "certificate changed" "$RUN/pin.txt"; then echo "ok   refused: $(grep -o 'server certificate changed.*' "$RUN/pin.txt" | cut -c1-70)..."; pinbad=0;
else echo "MISMATCH pin: failed for another reason:"; tail -2 "$RUN/pin.txt"; pinbad=1; fi
echo "== compare"
cmp_fields 1 open url h tiles tilebytes hx regions layers trunc
cmp_fields 1 tap url h regions layers
cmp_fields 2 open url h tiles tilebytes hx regions layers trunc
cmp_fields 2 typed url tiles regions layers
cmp_fields 3 open url h tiles tilebytes hx regions layers trunc
for n in 1 2 3; do grep -q "^PASS" "$RUN/c$n.txt" || { echo "C test $n did not PASS"; bad=1; }; done
if [ $bad = 0 ]; then echo "ALL MATCH"; else echo "DIFFERENCES FOUND"; exit 1; fi
