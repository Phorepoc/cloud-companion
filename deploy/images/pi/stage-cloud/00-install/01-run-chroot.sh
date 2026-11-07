#!/bin/bash -e
# SPDX-License-Identifier: Apache-2.0
# Docker, the host manager, NetworkManager's setup hotspot; Cloud Companion is downloaded on the setup page. No login.
sh /var/tmp/cloud/install.sh --from /var/tmp/cloud --board --no-start
rm -rf /var/tmp/cloud
passwd -l cloud
apt-get clean
