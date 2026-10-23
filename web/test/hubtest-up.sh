#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Bring up the throwaway hub (project phetest: phe-stalwart-t + phe-cloud-t) from a private copy of the repo in ~/phecloud-spike/hubtest
# (so its .env files never touch another stack). Usage: sh web/test/hubtest-up.sh up|down   (run from a repo checkout)
set -eu
SRC=$(pwd); T=${HUBTEST_DIR:-$HOME/phecloud-spike/hubtest}
DC="docker compose -p phetest -f docker-compose.yml -f web/test/compose.hubtest.yml"
case "${1:-up}" in
down) [ -d "$T" ] && (cd "$T" && $DC --profile hub down -v >/dev/null 2>&1) || true; rm -rf "$T"; echo "throwaway hub removed" ;;
up)
  rm -rf "$T"; mkdir -p "$T"; tar --exclude=.git --exclude=node_modules --exclude=out --exclude=screenshots --exclude=.deps -cf - . | (cd "$T" && tar xf -)
  cd "$T"
  # hub-init.sh with this project's names (it hard-codes the default project and the container name phe-stalwart)
  sed -e 's/phe-stalwart/phe-stalwart-t/g' \
      -e 's#docker compose --profile hub#docker compose -p phetest -f docker-compose.yml -f web/test/compose.hubtest.yml --profile hub#' \
      -e '/dc up -d --build zpush/d' deploy/hub/hub-init.sh > /tmp/hub-init-t.sh
  cp /tmp/hub-init-t.sh deploy/hub/hub-init-t.sh
  HUB_DOMAIN=hub.test sh deploy/hub/hub-init-t.sh
  $DC up -d --build phecloud >/dev/null 2>&1
  echo "throwaway hub up: phe-stalwart-t, phe-cloud-t (network phetest_default), env in $T/deploy/hub/.env.phecloud" ;;
esac
