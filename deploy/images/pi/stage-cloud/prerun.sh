#!/bin/bash -e
# SPDX-License-Identifier: Apache-2.0
# stage-cloud starts from stage2's root file system (Raspberry Pi OS Lite)
if [ ! -d "${ROOTFS_DIR}" ]; then
	copy_previous
fi
