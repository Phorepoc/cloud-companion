#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
set -e
cfg=/usr/share/z-push/config.php
if ! grep -q z-push-extra "$cfg"; then
  sed -i "s#define('TIMEZONE', '');#define('TIMEZONE', '${ZPUSH_TIMEZONE}');#; \
          s#define('BACKEND_PROVIDER', '');#define('BACKEND_PROVIDER', 'BackendCombined');#; \
          s#define('LOGLEVEL', LOGLEVEL_INFO);#define('LOGLEVEL', LOGLEVEL_${ZPUSH_LOGLEVEL});#" "$cfg"
  echo "require_once __DIR__ . '/z-push-extra.php';" >> "$cfg"
fi
chown -R www-data:www-data /var/lib/z-push /var/log/z-push
exec docker-php-entrypoint "$@"
