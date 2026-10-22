/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

/* Offline self-test of the phc core: SHA-256/HMAC vectors, CBOR round trip, a canned server conversation, hit-testing. */
#include "../phc.h"
#include "../phc_cbor.h"
#include "../phc_sha256.h"

#include <stdio.h>
#include <string.h>

static int failures;
#define CHECK(cond, ...) do { if (!(cond)) { failures++; fprintf(stderr, "CHECK FAILED %s:%d: ", __FILE__, __LINE__); fprintf(stderr, __VA_ARGS__); fprintf(stderr, "\n"); } } while (0)

static void hex(const uint8_t *p, size_t n, char *out)
{
    size_t i;
    for (i = 0; i < n; i++) sprintf(out + 2 * i, "%02x", p[i]);
}

/* scripted transport: recv returns bytes of `in`, send appends to `out` */
static uint8_t in_buf[8192], out_buf[8192];
static size_t in_len, in_pos, out_len;

static int t_send(void *ctx, const void *p, size_t n)
{
    (void)ctx;
    memcpy(out_buf + out_len, p, n);
    out_len += n;
    return (int)n;
}

static int t_recv(void *ctx, void *p, size_t n)
{
    size_t k = in_len - in_pos;
    (void)ctx;
    if (k == 0) return 0;
    if (k > n) k = n;
    memcpy(p, in_buf + in_pos, k);
    in_pos += k;
    return (int)k;
}

/* append one frame (length prefix + the CBOR built by fill) to in_buf */
static phc_w begin_frame(uint8_t *tmp, size_t cap) { phc_w w; phc_w_init(&w, tmp, cap); return w; }

static void end_frame(const phc_w *w)
{
    in_buf[in_len++] = (uint8_t)(w->len >> 24); in_buf[in_len++] = (uint8_t)(w->len >> 16);
    in_buf[in_len++] = (uint8_t)(w->len >> 8); in_buf[in_len++] = (uint8_t)w->len;
    memcpy(in_buf + in_len, w->p, w->len);
    in_len += w->len;
}

static phc c;
static uint8_t rx[4096];
static int got_tiles;
static uint64_t got_hash;

static void on_tile(void *u, const phc_tile *t) { (void)u; got_tiles++; got_hash = t->hash; }

int main(void)
{
    uint8_t d[32], tmp[1024];
    char h[80];
    phc_sha256 s;
    phc_w w;
    phc_r r;
    uint32_t n;
    phc_creds cr;
    phc_transport io;
    phc_events ev;
    phc_hit hit;
    int rc;

    /* SHA-256("abc") */
    phc_sha256_init(&s); phc_sha256_update(&s, "abc", 3); phc_sha256_final(&s, d);
    hex(d, 32, h);
    CHECK(!strcmp(h, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"), "sha256 abc: %s", h);
    /* HMAC-SHA256 RFC 4231 case 2 (the data is given in two parts to test the concatenation) */
    phc_hmac_sha256((const uint8_t *)"Jefe", 4, "what do ya want ", 16, "for nothing?", 12, d);
    hex(d, 32, h);
    CHECK(!strcmp(h, "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"), "hmac: %s", h);

    /* CBOR: a Go-encoded vector would be better; here: round trip including a 64-bit unsigned and a negative int */
    phc_w_init(&w, tmp, sizeof tmp);
    phc_w_map(&w, 4);
    phc_w_kv_text(&w, "t", "x");
    phc_w_kv_uint(&w, "big", 0xfedcba9876543210ull);
    phc_w_kv_int(&w, "neg", -300);
    phc_w_kv_bytes(&w, "b", "\x01\x02\x03", 3);
    CHECK(!w.err, "writer error");
    phc_r_init(&r, tmp, w.len);
    CHECK(!phc_r_map(&r, &n) && n == 4, "map");
    {
        const char *k; size_t kl; uint64_t u; int64_t i; const uint8_t *b; size_t bl;
        CHECK(!phc_r_text(&r, &k, &kl) && !phc_r_skip(&r), "t");
        CHECK(!phc_r_text(&r, &k, &kl) && !phc_r_uint(&r, &u) && u == 0xfedcba9876543210ull, "uint64");
        CHECK(!phc_r_text(&r, &k, &kl) && !phc_r_int(&r, &i) && i == -300, "neg int");
        CHECK(!phc_r_text(&r, &k, &kl) && !phc_r_bytes(&r, &b, &bl) && bl == 3 && b[2] == 3, "bytes");
    }

    {
        static const uint8_t nul[] = {0xf6};   /* Go encodes an empty slice as null */
        uint32_t cnt = 9;
        phc_r_init(&r, nul, 1);
        CHECK(!phc_r_array(&r, &cnt) && cnt == 0, "null as empty array");
    }

    /* a canned conversation: challenge -> (we send hello) -> welcome -> page, tile, layers, regions, done */
    memset(&cr, 0, sizeof cr);
    strcpy(cr.dev, "dev1");
    memset(cr.key, 7, 32);
    cr.have = 1;
    w = begin_frame(tmp, sizeof tmp);
    phc_w_map(&w, 3); phc_w_kv_text(&w, "t", "challenge"); phc_w_kv_uint(&w, "proto", 1);
    phc_w_kv_bytes(&w, "nonce", "0123456789abcdef", 16); end_frame(&w);
    w = begin_frame(tmp, sizeof tmp);
    phc_w_map(&w, 3); phc_w_kv_text(&w, "t", "welcome"); phc_w_kv_text(&w, "dev", "dev1"); phc_w_kv_text(&w, "server", "phecloud"); end_frame(&w);

    io.ctx = NULL; io.send = t_send; io.recv = t_recv;
    phc_init(&c, io, rx, sizeof rx);
    ev.on_page = NULL; ev.on_tile = on_tile; ev.user = NULL;
    phc_set_events(&c, ev);
    rc = phc_auth(&c, &cr, NULL, NULL);
    CHECK(rc == PHC_OK, "auth rc %d", rc);
    {
        /* the hello we sent must carry HMAC(key, nonce || "phecloud-v1") */
        uint8_t mac[32];
        phc_r r2;
        uint32_t m, q;
        int found = 0;
        phc_hmac_sha256(cr.key, 32, "0123456789abcdef", 16, "phecloud-v1", 11, mac);
        phc_r_init(&r2, out_buf + 4, out_len - 4);
        CHECK(!phc_r_map(&r2, &m), "hello map");
        for (q = 0; q < m && !r2.err; q++) {
            const char *k; size_t kl;
            phc_r_text(&r2, &k, &kl);
            if (phc_text_eq(k, kl, "mac")) {
                const uint8_t *b; size_t bl;
                phc_r_bytes(&r2, &b, &bl);
                found = (bl == 32 && !memcmp(b, mac, 32));
            } else phc_r_skip(&r2);
        }
        CHECK(found, "hello mac mismatch");
    }

    in_len = in_pos = 0;
    w = begin_frame(tmp, sizeof tmp);
    phc_w_map(&w, 6); phc_w_kv_text(&w, "t", "page"); phc_w_kv_uint(&w, "pid", 1); phc_w_kv_uint(&w, "w", 360);
    phc_w_kv_uint(&w, "h", 1200); phc_w_kv_text(&w, "url", "https://x/"); phc_w_kv_bool(&w, "trunc", 1); end_frame(&w);
    w = begin_frame(tmp, sizeof tmp);
    phc_w_map(&w, 9); phc_w_kv_text(&w, "t", "tile"); phc_w_kv_uint(&w, "pid", 1); phc_w_kv_uint(&w, "layer", 0);
    phc_w_kv_uint(&w, "x", 0); phc_w_kv_uint(&w, "y", 256); phc_w_kv_uint(&w, "w", 360); phc_w_kv_uint(&w, "h", 256);
    phc_w_kv_uint(&w, "hash", 0xfedcba9876543210ull); phc_w_kv_bytes(&w, "data", "\x89PNG", 4); end_frame(&w);
    w = begin_frame(tmp, sizeof tmp);
    phc_w_map(&w, 2); phc_w_kv_text(&w, "t", "layers"); phc_w_text(&w, "list"); phc_w_array(&w, 2);
    phc_w_map(&w, 7); phc_w_kv_uint(&w, "id", 1); phc_w_kv_text(&w, "kind", "fixed"); phc_w_kv_uint(&w, "x", 0);
    phc_w_kv_uint(&w, "y", 0); phc_w_kv_uint(&w, "w", 360); phc_w_kv_uint(&w, "h", 48); phc_w_kv_uint(&w, "z", 0);
    phc_w_map(&w, 9); phc_w_kv_uint(&w, "id", 2); phc_w_kv_text(&w, "kind", "sticky"); phc_w_kv_uint(&w, "x", 12);
    phc_w_kv_uint(&w, "y", 500); phc_w_kv_uint(&w, "w", 336); phc_w_kv_uint(&w, "h", 34); phc_w_kv_uint(&w, "z", 0);
    phc_w_kv_uint(&w, "top", 48); phc_w_kv_text(&w, "future", "ignored"); end_frame(&w);
    w = begin_frame(tmp, sizeof tmp);
    phc_w_map(&w, 2); phc_w_kv_text(&w, "t", "regions"); phc_w_text(&w, "list"); phc_w_array(&w, 3);
    phc_w_map(&w, 7); phc_w_kv_uint(&w, "id", 10); phc_w_kv_uint(&w, "layer", 0); phc_w_kv_uint(&w, "x", 20);
    phc_w_kv_uint(&w, "y", 700); phc_w_kv_uint(&w, "w", 100); phc_w_kv_uint(&w, "h", 30); phc_w_kv_text(&w, "kind", "link");
    phc_w_map(&w, 8); phc_w_kv_uint(&w, "id", 11); phc_w_kv_uint(&w, "layer", 0); phc_w_kv_uint(&w, "x", 0);
    phc_w_kv_uint(&w, "y", 600); phc_w_kv_uint(&w, "w", 360); phc_w_kv_uint(&w, "h", 300); phc_w_kv_text(&w, "kind", "button");
    phc_w_kv_text(&w, "href", "https://x/y");
    phc_w_map(&w, 7); phc_w_kv_uint(&w, "id", 12); phc_w_kv_uint(&w, "layer", 2); phc_w_kv_uint(&w, "x", 4);
    phc_w_kv_uint(&w, "y", 4); phc_w_kv_uint(&w, "w", 40); phc_w_kv_uint(&w, "h", 20); phc_w_kv_text(&w, "kind", "link");
    end_frame(&w);
    w = begin_frame(tmp, sizeof tmp);
    phc_w_map(&w, 4); phc_w_kv_text(&w, "t", "done"); phc_w_kv_uint(&w, "pid", 1); phc_w_kv_uint(&w, "ms", 42);
    phc_w_kv_uint(&w, "tiles", 1); end_frame(&w);

    rc = phc_pump(&c);
    CHECK(rc == PHC_OK, "pump rc %d", rc);
    CHECK(c.page.pid == 1 && c.page.h == 1200 && c.page.trunc == 1 && !strcmp(c.page.url, "https://x/"), "page");
    CHECK(got_tiles == 1 && got_hash == 0xfedcba9876543210ull && c.stats.hash_xor == got_hash, "tile");
    CHECK(phc_tile_lookup(&c, 0, 0, 256) && phc_tile_lookup(&c, 0, 0, 256)->hash == got_hash, "cache");
    CHECK(c.nlayers == 2 && c.layers[1].kind == PHC_LAYER_STICKY && c.layers[1].has_top && c.layers[1].top == 48, "layers");
    CHECK(c.nregions == 3 && !strcmp(c.regions[1].href, "https://x/y") && c.regions[0].href[0] == 0, "regions");
    CHECK(c.done.ms == 42 && c.done.tiles == 1, "done");

    /* hit-test. view 640, scroll 400: the page link (doc y 700..730) is at screen y 300..330 */
    CHECK(phc_hit_test(&c, 400, 640, 50, 310, &hit) && hit.region->id == 10, "smallest region wins");
    CHECK(hit.tap_layer == 0 && hit.tap_x == 50 && hit.tap_y == 710, "tap coordinates (page)");
    CHECK(phc_hit_test(&c, 400, 640, 200, 310, &hit) && hit.region->id == 11, "outer region");
    /* the sticky bar: natural y 500, scroll 400 -> screen 100; scroll 600 -> stuck at top 48 */
    CHECK(phc_layer_screen_y(&c.layers[1], 400, 640) == 100 && phc_layer_screen_y(&c.layers[1], 600, 640) == 48, "sticky");
    CHECK(phc_hit_test(&c, 600, 640, 20, 60, &hit) && hit.region->id == 12 && hit.layer == 2, "sticky layer region");
    CHECK(hit.tap_layer == 0 && hit.tap_y == 660, "sticky tap is sent in page coordinates");
    /* the fixed header covers the page: a point inside it hits the layer, not the page regions */
    phc_hit_test(&c, 400, 640, 100, 10, &hit);
    CHECK(hit.layer == 1 && hit.region == NULL && hit.tap_layer == 1 && hit.tap_y == 10, "fixed layer");

    printf(failures ? "selftest FAIL (%d)\n" : "selftest PASS\n", failures);
    return failures ? 1 : 0;
}
