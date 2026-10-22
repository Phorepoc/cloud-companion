/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */
#include "phc_cbor.h"
#include <string.h>

/* ---- writer ---- */
void phc_w_init(phc_w *w, uint8_t *buf, size_t cap) { w->p = buf; w->cap = cap; w->len = 0; w->err = 0; }

static void put(phc_w *w, const void *p, size_t n)
{
    if (w->err || w->len + n > w->cap) { w->err = 1; return; }
    memcpy(w->p + w->len, p, n);
    w->len += n;
}

static void head(phc_w *w, unsigned major, uint64_t v)
{
    uint8_t b[9];
    size_t n = 1;
    if (v < 24) b[0] = (uint8_t)(major << 5 | v);
    else if (v <= 0xff) { b[0] = (uint8_t)(major << 5 | 24); b[1] = (uint8_t)v; n = 2; }
    else if (v <= 0xffff) { b[0] = (uint8_t)(major << 5 | 25); b[1] = (uint8_t)(v >> 8); b[2] = (uint8_t)v; n = 3; }
    else if (v <= 0xffffffffu) {
        b[0] = (uint8_t)(major << 5 | 26);
        b[1] = (uint8_t)(v >> 24); b[2] = (uint8_t)(v >> 16); b[3] = (uint8_t)(v >> 8); b[4] = (uint8_t)v; n = 5;
    } else {
        int i;
        b[0] = (uint8_t)(major << 5 | 27);
        for (i = 0; i < 8; i++) b[1 + i] = (uint8_t)(v >> (56 - 8 * i));
        n = 9;
    }
    put(w, b, n);
}

void phc_w_map(phc_w *w, unsigned n) { head(w, 5, n); }
void phc_w_array(phc_w *w, unsigned n) { head(w, 4, n); }
void phc_w_text(phc_w *w, const char *s) { size_t n = strlen(s); head(w, 3, n); put(w, s, n); }
void phc_w_uint(phc_w *w, uint64_t v) { head(w, 0, v); }
void phc_w_int(phc_w *w, int64_t v) { if (v >= 0) head(w, 0, (uint64_t)v); else head(w, 1, (uint64_t)(-1 - v)); }
void phc_w_bytes(phc_w *w, const void *p, size_t n) { head(w, 2, n); put(w, p, n); }
void phc_w_bool(phc_w *w, int v) { uint8_t b = v ? 0xf5 : 0xf4; put(w, &b, 1); }
void phc_w_kv_text(phc_w *w, const char *k, const char *v) { phc_w_text(w, k); phc_w_text(w, v); }
void phc_w_kv_uint(phc_w *w, const char *k, uint64_t v) { phc_w_text(w, k); phc_w_uint(w, v); }
void phc_w_kv_int(phc_w *w, const char *k, int64_t v) { phc_w_text(w, k); phc_w_int(w, v); }
void phc_w_kv_bytes(phc_w *w, const char *k, const void *p, size_t n) { phc_w_text(w, k); phc_w_bytes(w, p, n); }
void phc_w_kv_bool(phc_w *w, const char *k, int v) { phc_w_text(w, k); phc_w_bool(w, v); }

/* ---- reader ---- */
void phc_r_init(phc_r *r, const uint8_t *buf, size_t n) { r->p = buf; r->end = buf + n; r->err = 0; }

static int fail(phc_r *r) { r->err = 1; return -1; }

/* reads one head: *major 0..7, *arg the argument, *info the 5-bit additional info */
static int rhead(phc_r *r, unsigned *major, uint64_t *arg, unsigned *info)
{
    unsigned b, ai, i, n;
    uint64_t v;
    if (r->err || r->p >= r->end) return fail(r);
    b = *r->p++;
    *major = b >> 5;
    ai = b & 31;
    *info = ai;
    if (ai < 24) { *arg = ai; return 0; }
    if (ai > 27) { *arg = 0; return fail(r); } /* indefinite lengths are not supported */
    n = 1u << (ai - 24);
    if ((size_t)(r->end - r->p) < n) return fail(r);
    v = 0;
    for (i = 0; i < n; i++) v = (v << 8) | *r->p++;
    *arg = v;
    return 0;
}

int phc_r_uint(phc_r *r, uint64_t *v)
{
    unsigned m, ai;
    if (rhead(r, &m, v, &ai) || m != 0) return fail(r);
    return 0;
}

int phc_r_int(phc_r *r, int64_t *v)
{
    unsigned m, ai;
    uint64_t a;
    if (rhead(r, &m, &a, &ai)) return -1;
    if (m == 0) { *v = (int64_t)a; return 0; }
    if (m == 1) { *v = -1 - (int64_t)a; return 0; }
    return fail(r);
}

int phc_r_bool(phc_r *r, int *v)
{
    unsigned m, ai;
    uint64_t a;
    if (rhead(r, &m, &a, &ai) || m != 7 || (ai != 20 && ai != 21)) return fail(r);
    *v = (ai == 21);
    return 0;
}

static int rstr(phc_r *r, unsigned want, const uint8_t **p, size_t *len)
{
    unsigned m, ai;
    uint64_t a;
    if (rhead(r, &m, &a, &ai) || m != want) return fail(r);
    if ((uint64_t)(r->end - r->p) < a) return fail(r);
    *p = r->p;
    *len = (size_t)a;
    r->p += a;
    return 0;
}

int phc_r_text(phc_r *r, const char **s, size_t *len)
{
    const uint8_t *p;
    if (rstr(r, 3, &p, len)) return -1;
    *s = (const char *)p;
    return 0;
}

int phc_r_bytes(phc_r *r, const uint8_t **p, size_t *len) { return rstr(r, 2, p, len); }

int phc_r_map(phc_r *r, uint32_t *n)
{
    unsigned m, ai;
    uint64_t a;
    if (rhead(r, &m, &a, &ai) || m != 5 || a > 0xffff) return fail(r);
    *n = (uint32_t)a;
    return 0;
}

int phc_r_array(phc_r *r, uint32_t *n)
{
    unsigned m, ai;
    uint64_t a;
    if (!r->err && r->p < r->end && *r->p == 0xf6) { /* null: Go encodes an empty slice as null */
        r->p++;
        *n = 0;
        return 0;
    }
    if (rhead(r, &m, &a, &ai) || m != 4 || a > 0xffff) return fail(r);
    *n = (uint32_t)a;
    return 0;
}

int phc_r_skip(phc_r *r)
{
    unsigned m, ai;
    uint64_t a, i;
    if (rhead(r, &m, &a, &ai)) return -1;
    switch (m) {
    case 0: case 1: case 7: return 0;
    case 2: case 3:
        if ((uint64_t)(r->end - r->p) < a) return fail(r);
        r->p += a;
        return 0;
    case 4: for (i = 0; i < a; i++) if (phc_r_skip(r)) return -1; return 0;
    case 5: for (i = 0; i < 2 * a; i++) if (phc_r_skip(r)) return -1; return 0;
    case 6: return phc_r_skip(r);
    }
    return fail(r);
}

int phc_text_eq(const char *s, size_t len, const char *lit) { return strlen(lit) == len && memcmp(s, lit, len) == 0; }
