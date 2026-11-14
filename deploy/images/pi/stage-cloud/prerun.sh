#!/bin/bash -e
# SPDX-License-Identifier: AGPL-3.0-only
# stage-cloud starts from stage2's root file system (Raspberry Pi OS Lite)
if [ ! -d "${ROOTFS_DIR}" ]; then
	copy_previous
fi
