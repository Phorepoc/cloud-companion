#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# The WHOLE app on an external database: phecloud with PHECLOUD_DB=postgres://... or mysql://... (own compose project phedb, db container from
# docker-compose.db.yml), then the browser tests that touch the database most: pairing, passkeys, SMS conversations (chats.ts, passkey.ts).
# Usage: sh web/test/dbstack.sh postgres|mariadb
set -eu
KIND=${1:?postgres or mariadb}
ROOT=$(pwd); OUT=$ROOT/web/test/out; mkdir -p "$OUT"
export PHECLOUD_DB_PASSWORD=pw
case $KIND in
  postgres) export PHECLOUD_DB='postgres://phecloud:pw@postgres:5432/phecloud?sslmode=disable'; SVC=postgres ;;
  mariadb)  export PHECLOUD_DB='mysql://phecloud:pw@mariadb:3306/phecloud'; SVC=mariadb ;;
esac
DC="docker compose -p phedb -f docker-compose.yml -f docker-compose.db.yml -f web/test/compose.passkey.yml --profile db-$KIND"
$DC down -v >/dev/null 2>&1 || true
trap '$DC down -v >/dev/null 2>&1' EXIT
$DC up -d --build $SVC >/dev/null 2>&1
for i in $(seq 1 60); do docker logs phe-$SVC 2>&1 | grep -qi "ready to accept connections\|ready for connections" && break; sleep 2; done; sleep 4
$DC up -d phecloud >/dev/null 2>&1
sleep 8
docker logs phe-cloud-pk 2>&1 | tail -3 | cut -c1-200
run() { docker run --rm --name phe-playwright --cpus 2 --memory 2g --network container:phe-cloud-pk --ipc=host -v "$ROOT/web/test":/t -v "$OUT":/out -w /t -e HOME=/tmp -e BASE=http://localhost:8080 -e CODES="$1" -e OUT=/out mcr.microsoft.com/playwright:v1.49.1-noble sh -c "npm install --no-audit --no-fund --silent && node --experimental-strip-types --no-warnings=ExperimentalWarning $2"; }
A=$(docker exec phe-cloud-pk phecloud paircode --admin); P=$(docker exec phe-cloud-pk phecloud paircode)
echo "== chats.ts on $KIND"; run "$A,$P" chats.ts
A=$(docker exec phe-cloud-pk phecloud paircode --admin); P1=$(docker exec phe-cloud-pk phecloud paircode); P2=$(docker exec phe-cloud-pk phecloud paircode)
echo "== passkey.ts on $KIND"; run "$A,$P1,$P2" passkey.ts
