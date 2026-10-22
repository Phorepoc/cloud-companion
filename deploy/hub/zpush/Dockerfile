# SPDX-License-Identifier: Apache-2.0
# Z-Push (AGPL-3.0, https://github.com/Z-Hub/Z-Push) in its own container: an Exchange ActiveSync 14.1 bridge in front of
# Stalwart (IMAP + CalDAV + CardDAV through Z-Push's "combined" backend). Never linked into phecloud; spoken to over HTTP only.
FROM php:8.3-apache-bookworm
ARG ZPUSH_VERSION=2.7.7
ARG ZPUSH_SHA256=36afda54d8ad5ed0c87e2e0648007425d73ec13f33c866a5e28a82771d285105
RUN apt-get update \
 && apt-get install -y --no-install-recommends libc-client-dev libkrb5-dev libssl-dev libxml2-dev libxslt1-dev libicu-dev curl ca-certificates libawl-php \
 && docker-php-ext-configure imap --with-kerberos --with-imap-ssl \
 && docker-php-ext-install -j2 imap sysvsem sysvshm sysvmsg pcntl soap xsl intl \
 && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL -o /tmp/zp.tgz "https://github.com/Z-Hub/Z-Push/archive/refs/tags/${ZPUSH_VERSION}.tar.gz" \
 && echo "${ZPUSH_SHA256}  /tmp/zp.tgz" | sha256sum -c - \
 && mkdir -p /usr/share/z-push /var/lib/z-push /var/log/z-push \
 && tar xzf /tmp/zp.tgz -C /usr/share/z-push --strip-components=2 "Z-Push-${ZPUSH_VERSION}/src" \
 && rm /tmp/zp.tgz && chown -R www-data:www-data /var/lib/z-push /var/log/z-push
# Backends -> Stalwart (service name "stalwart"): IMAPS 993 and SMTPS 465 with an unverified internal certificate,
# CalDAV/CardDAV over plain HTTP 8080 (all on the compose network only).
RUN cd /usr/share/z-push/backend  && sed -i "s#define('IMAP_SERVER', 'localhost');#define('IMAP_SERVER', 'stalwart');#;   s#define('IMAP_PORT', 143);#define('IMAP_PORT', 993);#;   s#define('IMAP_OPTIONS', '/notls/norsh');#define('IMAP_OPTIONS', '/ssl/novalidate-cert/norsh');#;   s#define('IMAP_FOLDER_CONFIGURED', false);#define('IMAP_FOLDER_CONFIGURED', true);#;   s#define('IMAP_FOLDER_SENT', 'SENT');#define('IMAP_FOLDER_SENT', 'Sent Items');#;   s#define('IMAP_FOLDER_DRAFT', 'DRAFTS');#define('IMAP_FOLDER_DRAFT', 'Drafts');#;   s#define('IMAP_FOLDER_TRASH', 'TRASH');#define('IMAP_FOLDER_TRASH', 'Deleted Items');#;   s#define('IMAP_FOLDER_SPAM', 'SPAM');#define('IMAP_FOLDER_SPAM', 'Junk Mail');#;   s#define('IMAP_FOLDER_ARCHIVE', 'ARCHIVE');#define('IMAP_FOLDER_ARCHIVE', 'Archive');#;   s#define('IMAP_SMTP_METHOD', 'mail');#define('IMAP_SMTP_METHOD', 'smtp');#;   s#^\$imap_smtp_params = array();#\$imap_smtp_params = array('host' => 'ssl://stalwart', 'port' => 465, 'auth' => true, 'username' => 'imap_username', 'password' => 'imap_password', 'verify_peer' => false, 'verify_peer_name' => false, 'allow_self_signed' => true);#" imap/config.php  && sed -i "s#define('CALDAV_PROTOCOL', 'https');#define('CALDAV_PROTOCOL', 'http');#;   s#define('CALDAV_SERVER', 'caldavserver.domain.com');#define('CALDAV_SERVER', 'stalwart');#;   s#define('CALDAV_PORT', '443');#define('CALDAV_PORT', '8080');#;   s#define('CALDAV_PATH', '/caldav.php/%u/');#define('CALDAV_PATH', '/dav/cal/%u/');#;   s#define('CALDAV_PERSONAL', 'PRINCIPAL');#define('CALDAV_PERSONAL', 'default');#;   s#define('CALDAV_SUPPORTS_SYNC', false);#define('CALDAV_SUPPORTS_SYNC', true);#" caldav/config.php  && sed -i "s#define('CARDDAV_PROTOCOL', 'https');#define('CARDDAV_PROTOCOL', 'http');#;   s#define('CARDDAV_SERVER', 'localhost');#define('CARDDAV_SERVER', 'stalwart');#;   s#define('CARDDAV_PORT', '443');#define('CARDDAV_PORT', '8080');#;   s#define('CARDDAV_PATH', '/caldav.php/%u/');#define('CARDDAV_PATH', '/dav/card/%u/');#;   s#define('CARDDAV_DEFAULT_PATH', '/caldav.php/%u/addresses/');#define('CARDDAV_DEFAULT_PATH', '/dav/card/%u/default/');#;   s#define('CARDDAV_SUPPORTS_SYNC', false);#define('CARDDAV_SUPPORTS_SYNC', true);#" carddav/config.php  && grep -c stalwart imap/config.php caldav/config.php carddav/config.php
RUN a2enmod remoteip
COPY backend-config/ /usr/share/z-push/backend/
COPY z-push-extra.php /usr/share/z-push/z-push-extra.php
COPY z-push.conf /etc/apache2/conf-enabled/z-push.conf
COPY entrypoint.sh /entrypoint.sh
ENV ZPUSH_TIMEZONE=UTC ZPUSH_LOGLEVEL=INFO
VOLUME /var/lib/z-push
ENTRYPOINT ["/entrypoint.sh"]
CMD ["apache2-foreground"]
