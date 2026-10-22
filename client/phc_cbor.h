/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

/* A tiny CBOR subset: what the phecloud protocol uses. Definite lengths only (the server never sends others).
 * Writer: maps/arrays with known counts, text, unsigned/signed ints, bytes, bool. Reader: a cursor; every call
 * returns 0 or -1 and latches r->err. No allocation. */
#ifndef PHC_CBOR_H
#define PHC_CBOR_H
#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct { uint8_t *p; size_t cap, len; int err; } phc_w;

void phc_w_init(phc_w *w, uint8_t *buf, size_t cap);
void phc_w_map(phc_w *w, unsigned n);
void phc_w_array(phc_w *w, unsigned n);
void phc_w_text(phc_w *w, const char *s);
void phc_w_uint(phc_w *w, uint64_t v);
void phc_w_int(phc_w *w, int64_t v);
void phc_w_bytes(phc_w *w, const void *p, size_t n);
void phc_w_bool(phc_w *w, int v);
/* key + value helpers */
void phc_w_kv_text(phc_w *w, const char *k, const char *v);
void phc_w_kv_uint(phc_w *w, const char *k, uint64_t v);
void phc_w_kv_int(phc_w *w, const char *k, int64_t v);
void phc_w_kv_bytes(phc_w *w, const char *k, const void *p, size_t n);
void phc_w_kv_bool(phc_w *w, const char *k, int v);

typedef struct { const uint8_t *p, *end; int err; } phc_r;

void phc_r_init(phc_r *r, const uint8_t *buf, size_t n);
int phc_r_map(phc_r *r, uint32_t *n);      /* enter a map of n pairs */
int phc_r_array(phc_r *r, uint32_t *n);    /* enter an array of n items */
int phc_r_text(phc_r *r, const char **s, size_t *len);   /* points into the buffer, not NUL-terminated */
int phc_r_bytes(phc_r *r, const uint8_t **p, size_t *len);
int phc_r_uint(phc_r *r, uint64_t *v);
int phc_r_int(phc_r *r, int64_t *v);
int phc_r_bool(phc_r *r, int *v);
int phc_r_skip(phc_r *r);                  /* skip one whole item */
/* true when the text (s, len) equals the C string lit */
int phc_text_eq(const char *s, size_t len, const char *lit);

#ifdef __cplusplus
}
#endif
#endif
