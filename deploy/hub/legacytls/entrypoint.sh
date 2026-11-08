#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# First start: a small CA and an RSA-2048 / SHA-256 server certificate for $LEGACY_TLS_CN (install /data/ca.crt on the phone;
# it is also served at https://<host>:8443/ca.crt). Plain RSA, because old stacks have no EC.
set -e
d=/data
cn="${LEGACY_TLS_CN:-localhost}"
if [ ! -f $d/server.crt ]; then
  openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -sha256 -subj "/CN=phecloud hub CA" -keyout $d/ca.key -out $d/ca.crt \
    -addext "basicConstraints=critical,CA:TRUE" -addext "keyUsage=critical,keyCertSign,cRLSign"
  openssl req -newkey rsa:2048 -nodes -subj "/CN=$cn" -keyout $d/server.key -out $d/server.csr
  case "$cn" in *[!0-9.]*) san="DNS:$cn";; *) san="IP:$cn";; esac
  openssl x509 -in $d/ca.crt -outform der -out $d/ca.cer
  printf "subjectAltName=%s\nbasicConstraints=CA:FALSE\nkeyUsage=digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\n" "$san" > $d/ext.cnf
  openssl x509 -req -in $d/server.csr -CA $d/ca.crt -CAkey $d/ca.key -CAcreateserial -days 825 -sha256 -extfile $d/ext.cnf -out $d/server.crt
fi
exec "$@"
