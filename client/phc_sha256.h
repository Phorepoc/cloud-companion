/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */
#ifndef PHC_SHA256_H
#define PHC_SHA256_H
#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct { uint32_t h[8]; uint64_t n; uint8_t buf[64]; size_t fill; } phc_sha256;

void phc_sha256_init(phc_sha256 *c);
void phc_sha256_update(phc_sha256 *c, const void *p, size_t n);
void phc_sha256_final(phc_sha256 *c, uint8_t out[32]);
/* HMAC-SHA256 of the concatenation of two parts (b may be NULL) */
void phc_hmac_sha256(const uint8_t *key, size_t klen, const void *a, size_t alen, const void *b, size_t blen, uint8_t out[32]);

#ifdef __cplusplus
}
#endif
#endif
