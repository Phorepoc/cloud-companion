# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
#
# Host build of the phc core and its test (gcc or clang, C99). The core itself has no OS dependency.
CC ?= gcc
CFLAGS ?= -std=c99 -O2 -Wall -Wextra -Wpedantic -Wshadow -Wconversion -Wno-sign-conversion
SRC = phc.c phc_cbor.c phc_sha256.c

all: build/host_test build/selftest

build:
	mkdir -p build

build/host_test: $(SRC) test/host_test.c phc.h phc_cbor.h phc_sha256.h | build
	$(CC) $(CFLAGS) -D_POSIX_C_SOURCE=200809L -o $@ $(SRC) test/host_test.c

build/selftest: $(SRC) test/selftest.c phc.h phc_cbor.h phc_sha256.h | build
	$(CC) $(CFLAGS) -o $@ $(SRC) test/selftest.c

check: build/selftest
	./build/selftest

clean:
	rm -rf build
