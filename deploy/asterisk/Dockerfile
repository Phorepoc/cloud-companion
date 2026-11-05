# SPDX-License-Identifier: Apache-2.0
# Asterisk (GPL-2.0, a separate container: nothing links it into phecloud) from Alpine's package, for the compose profile "asterisk"
# (docs/messaging.md). The first start writes a starting configuration into the asterisk-etc volume; it is yours to edit after that.
FROM alpine:3.22
RUN apk add --no-cache asterisk asterisk-sample-config
COPY pjsip.conf.in extensions.conf rtp.conf /usr/share/phecloud-asterisk/
COPY entrypoint.sh /usr/local/bin/phecloud-asterisk
EXPOSE 5060/udp 5060/tcp 10000-10099/udp
ENTRYPOINT ["/usr/local/bin/phecloud-asterisk"]
