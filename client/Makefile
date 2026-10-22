# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
#
# Host build of the phc core and its tests (gcc or clang, C99). The core itself has no OS dependency.
#   make all check fuzz                       plain build, offline self-test, sanitizer fuzz run
#   make MBEDTLS=/path/to/mbedtls-3.6 all     also builds the TLS 1.3 adapter and a -tls capable host_test
CC ?= gcc
CFLAGS ?= -std=c99 -O2 -Wall -Wextra -Wpedantic -Wshadow -Wconversion -Wno-sign-conversion
SRC = phc.c phc_cbor.c phc_sha256.c
HDR = phc.h phc_cbor.h phc_sha256.h phc_mbedtls.h

ifneq ($(MBEDTLS),)
TLSFLAGS = -DPHC_WITH_MBEDTLS -isystem $(MBEDTLS)/include
TLSSRC = phc_mbedtls.c
TLSLIBS = $(MBEDTLS)/library/libmbedtls.a $(MBEDTLS)/library/libmbedx509.a $(MBEDTLS)/library/libmbedcrypto.a
endif

all: build/host_test build/selftest

build:
	mkdir -p build

build/host_test: $(SRC) $(TLSSRC) test/host_test.c $(HDR) | build
	$(CC) $(CFLAGS) $(TLSFLAGS) -D_POSIX_C_SOURCE=200809L -o $@ $(SRC) $(TLSSRC) test/host_test.c $(TLSLIBS)

build/selftest: $(SRC) test/selftest.c $(HDR) | build
	$(CC) $(CFLAGS) -o $@ $(SRC) test/selftest.c

check: build/selftest
	./build/selftest

# the same self-test (which includes the truncated/mutated/garbage frame fuzzing) under AddressSanitizer + UBSan
fuzz: $(SRC) test/selftest.c $(HDR) | build
	$(CC) $(CFLAGS) -O1 -g -fsanitize=address,undefined -fno-sanitize-recover=undefined -o build/selftest_asan $(SRC) test/selftest.c
	./build/selftest_asan

clean:
	rm -rf build
