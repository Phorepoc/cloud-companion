#!/bin/bash -e
# SPDX-License-Identifier: AGPL-3.0-only
# Docker, the host manager, NetworkManager's setup hotspot; Cloud Companion is downloaded on the setup page. No login.
sh /var/tmp/cloud/install.sh --from /var/tmp/cloud --board --no-start
rm -rf /var/tmp/cloud
passwd -l cloud
apt-get clean
