#!/bin/bash
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
#
# Memory per open tab: starts the server, opens 1, 2, 3 tabs through one connection (client scenario "mem") and samples the
# container after each (cgroup usage and the summed RSS of the Chromium processes). Run from the repo root on a Docker host:
#   bash client/test/memory.sh [url1 url2 url3]      (default: example.com x3; use heavier pages to see the difference)
set -eu
ROOT=$(pwd)
URLS=("$@")
[ ${#URLS[@]} -ge 3 ] || URLS=(https://example.com https://example.com https://example.com)
docker compose up -d --build >/dev/null 2>&1
trap 'docker compose down >/dev/null 2>&1' EXIT
sleep 8
CODE=$(docker exec phe-cloud phecloud paircode 2>/dev/null)
rm -rf "$ROOT/.testrun-mem"; mkdir -p "$ROOT/.testrun-mem"

sample() { # label
    local cg rss n
    cg=$(docker stats --no-stream --format '{{.MemUsage}}' phe-cloud | cut -d/ -f1 | tr -d ' ')
    read -r rss n < <(docker exec phe-cloud sh -c 'for d in /proc/[0-9]*; do awk "/^Name:/{n=\$2} /^VmRSS:/{r=\$2} END{if(r) print n, r}" $d/status 2>/dev/null; done' |
        awk '/headless/{s+=$2;c++} END{print s, c}')
    echo "$1: container $cg, chromium RSS $((rss / 1024)) MB in $n processes"
}

sample "idle server (2 warm tabs)"
coproc CL { docker run -i --rm --name phe-client --cpus 1 --memory 512m --user "$(id -u):$(id -g)" --network host -v "$ROOT/.testrun-mem":/w -w /w \
    --entrypoint /usr/local/bin/phecloud-client phe-cloud:dev -addr 127.0.0.1:7400 -state st.json -code "$CODE" -scenario mem "${URLS[@]/#/-url=}" 2>&1; }
while read -r line <&"${CL[0]}"; do
    case "$line" in
        MEM-READY*) sleep 4; sample "after tab ${line#MEM-READY }"; echo go >&"${CL[1]}" ;;
    esac
done
