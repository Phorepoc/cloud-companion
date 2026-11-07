#!/bin/bash -e
# SPDX-License-Identifier: Apache-2.0
# the board files (deploy/board/bundle.sh, put in files/ by build.sh) into the image, and an example setup code on the boot
# partition
install -d "${ROOTFS_DIR}/var/tmp/cloud"
install -m 0644 files/cloud-board.tar.gz "${ROOTFS_DIR}/var/tmp/cloud/"
install -m 0755 files/phehost-linux-arm64 files/install.sh "${ROOTFS_DIR}/var/tmp/cloud/"
install -m 0644 files/cloud-setup.txt.example "${ROOTFS_DIR}/boot/firmware/cloud-setup.txt.example"
