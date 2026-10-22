# SPDX-License-Identifier: Apache-2.0
# TLS 1.0-1.2 terminator with RSA key exchange and CBC/SHA-1 ciphers for old phones (OpenSSL 0.9.8-era stacks), in front of
# Z-Push only. Not for the internet: reach it over the LAN or a tunnel. Caddy (Go crypto/tls) cannot offer TLS 1.0.
FROM nginx:1.28-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends openssl && rm -rf /var/lib/apt/lists/*
COPY openssl-legacy.cnf /etc/nginx/openssl-legacy.cnf
COPY nginx.conf /etc/nginx/nginx.conf
COPY entrypoint.sh /entrypoint.sh
ENV OPENSSL_CONF=/etc/nginx/openssl-legacy.cnf
VOLUME /data
ENTRYPOINT ["/entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
