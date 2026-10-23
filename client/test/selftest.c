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
    if (out_len + n > sizeof out_buf) return (int)n;   /* fuzzing: drop what does not fit */
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
static int got_dialog_id = -1, got_dialog_kind = -1, got_dl, got_focus;
static char got_dialog_msg[64];
static phc *g_c;

static void on_dialog(void *u, const phc_dialog *d)
{
    (void)u;
    got_dialog_id = d->id; got_dialog_kind = d->kind;
    { size_t n = strlen(d->message); if (n >= sizeof got_dialog_msg) n = sizeof got_dialog_msg - 1; memcpy(got_dialog_msg, d->message, n); got_dialog_msg[n] = 0; }
    phc_dialog_reply(g_c, d->id, 1, "Zed");
}
static void on_download(void *u, const phc_download *d) { (void)u; got_dl = (d->refused && !strcmp(d->name, "x.bin")); }
static void on_focus(void *u, const phc_focus *f) { (void)u; got_focus = (f->kind == PHC_FOCUS_TEXT && !strcmp(f->type, "search") && !strcmp(f->value, "abc") && f->start == 3 && f->end == -1); }


/* ---- fuzz-ish: truncated, mutated and garbage frames must fail cleanly (run under ASan/UBSan: make fuzz) ---- */
static uint8_t conv[8192];
static size_t conv_len;
static uint32_t rng = 12345;
static uint32_t rnd(void) { rng = rng * 1664525u + 1013904223u; return rng >> 8; }
static phc fc;
static uint8_t frx[4096];

static int fuzz_one(const uint8_t *data, size_t n)
{
    phc_transport fio;
    phc_events fev;
    int i, rc = 0;
    memcpy(in_buf, data, n);
    in_len = n; in_pos = 0; out_len = 0;
    fio.ctx = NULL; fio.send = t_send; fio.recv = t_recv; fio.wait = NULL;
    phc_init(&fc, fio, frx, sizeof frx);
    memset(&fev, 0, sizeof fev);
    fev.on_tile = on_tile; fev.on_dialog = on_dialog; fev.on_download = on_download; fev.on_focus = on_focus;
    phc_set_events(&fc, fev);
    g_c = &fc;
    for (i = 0; i < 16 && rc == 0; i++) rc = phc_pump(&fc);   /* until an error or the end of the data */
    for (i = 0; i < fc.nregions; i++) {                      /* the decoded tables must be self-consistent */
        const phc_region *g = &fc.regions[i];
        if (!g->href || !g->label || g->opt_first < 0 || g->opt_first + g->opt_count > fc.nopts) { failures++; fprintf(stderr, "inconsistent region after fuzz\n"); break; }
    }
    return rc;
}

static void fuzz(void)
{
    uint8_t buf[8192];
    size_t i, k;
    int rc;
    long ok = 0, bad = 0;

    for (k = 0; k <= conv_len; k++) { rc = fuzz_one(conv, k); if (rc) bad++; else ok++; }   /* every truncation */
    for (i = 0; i < 6000; i++) {                                                           /* byte flips */
        int flips = 1 + (int)(rnd() % 3);
        memcpy(buf, conv, conv_len);
        while (flips--) buf[rnd() % conv_len] = (uint8_t)rnd();
        rc = fuzz_one(buf, conv_len);
        if (rc) bad++; else ok++;
    }
    for (i = 0; i < 2000; i++) {                                                           /* garbage with a valid length prefix */
        size_t n = 1 + rnd() % 200;
        buf[0] = 0; buf[1] = 0; buf[2] = 0; buf[3] = (uint8_t)n;
        for (k = 0; k < n; k++) buf[4 + k] = (uint8_t)rnd();
        fuzz_one(buf, 4 + n);
    }
    {   /* deep nesting: map { "zz": [[[[...]]]], "t": "page" } */
        size_t p = 4, depth = 200;
        buf[p++] = 0xa2; buf[p++] = 0x62; buf[p++] = 'z'; buf[p++] = 'z';
        for (k = 0; k < depth; k++) buf[p++] = 0x81;
        buf[p++] = 0x00; buf[p++] = 0x61; buf[p++] = 't'; buf[p++] = 0x64; buf[p++] = 'p'; buf[p++] = 'a'; buf[p++] = 'g'; buf[p++] = 'e';
        buf[0] = 0; buf[1] = 0; buf[2] = (uint8_t)((p - 4) >> 8); buf[3] = (uint8_t)(p - 4);
        rc = fuzz_one(buf, p);
        CHECK(rc != 0 || fc.page.pid == 0, "deeply nested frame was accepted");
    }
    {   /* absurd and zero lengths */
        static const uint8_t huge[] = {0x7f, 0xff, 0xff, 0xff, 0xa0};
        static const uint8_t zero[] = {0, 0, 0, 0};
        CHECK(fuzz_one(huge, sizeof huge) == PHC_ERR_BUF, "huge length must be refused");
        CHECK(fuzz_one(zero, sizeof zero) == PHC_ERR_PROTO, "zero length must be refused");
    }
    printf("fuzz: %ld runs, %ld ended cleanly at the end of the data, %ld refused a frame, no crash\n", ok + bad + 2000, ok, bad);
}

static void sms_test(void)
{
    static phc sc;
    static uint8_t srx[4096], tmp[1024];
    phc_transport io;
    phc_sms_msg m[2];
    phc_sms_ack ack;
    phc_sms_pending pend[4];
    int np = -1, acc = -1, rc;
    phc_w w;
    phc_r r;
    uint32_t cnt, i;
    int saw_sms = 0, saw_msgs = 0;
    io.ctx = NULL; io.send = t_send; io.recv = t_recv; io.wait = NULL;
    memset(&sc, 0, sizeof sc);
    phc_init(&sc, io, srx, sizeof srx);
    in_len = in_pos = out_len = 0;
    /* canned answer: smsres with 2 accepted and one pending message */
    w = begin_frame(tmp, sizeof tmp);
    phc_w_map(&w, 4);
    phc_w_kv_text(&w, "t", "smsres");
    phc_w_kv_int(&w, "id", 1);
    phc_w_kv_int(&w, "accepted", 2);
    phc_w_text(&w, "pending");
    phc_w_array(&w, 1);
    phc_w_map(&w, 3);
    phc_w_kv_int(&w, "out_id", 42);
    phc_w_kv_text(&w, "number", "+358407654321");
    phc_w_kv_text(&w, "body", "reply from the web");
    end_frame(&w);
    m[0].id = "1048583"; m[0].number = "+358407654321"; m[0].dir = "in"; m[0].body = "Hello"; m[0].status = NULL; m[0].ts = 1760000000;
    m[1].id = "1048590"; m[1].number = "0407654321"; m[1].dir = "out"; m[1].body = "Hi"; m[1].status = "sent"; m[1].ts = 1760000100;
    ack.out_id = 41; ack.id = "1048600"; ack.status = "failed"; ack.error = "no network";
    rc = phc_sms_sync(&sc, m, 2, &ack, 1, pend, 4, &np, &acc);
    CHECK(rc == PHC_OK && acc == 2 && np == 1, "sms sync answer: rc %d acc %d np %d", rc, acc, np);
    CHECK(np == 1 && pend[0].out_id == 42 && !strcmp(pend[0].number, "+358407654321") && !strcmp(pend[0].body, "reply from the web"), "pending message parsed");
    /* the request frame: t=sms, 2 messages with their ids, 1 ack */
    phc_r_init(&r, out_buf + 4, out_len - 4);
    CHECK(phc_r_map(&r, &cnt) == 0, "request is a map");
    for (i = 0; i < cnt; i++) {
        const char *k, *v;
        size_t kl, vl;
        phc_r_text(&r, &k, &kl);
        if (kl == 1 && k[0] == 't') { phc_r_text(&r, &v, &vl); saw_sms = vl == 3 && !memcmp(v, "sms", 3); }
        else if (kl == 8 && !memcmp(k, "messages", 8)) { uint32_t n; phc_r_array(&r, &n); saw_msgs = (int)n; for (n = 0; n < (uint32_t)saw_msgs; n++) phc_r_skip(&r); }
        else phc_r_skip(&r);
    }
    CHECK(saw_sms && saw_msgs == 2, "request frame: sms=%d messages=%d", saw_sms, saw_msgs);
    /* a server error frame */
    memset(&sc, 0, sizeof sc);
    phc_init(&sc, io, srx, sizeof srx);
    in_len = in_pos = out_len = 0;
    w = begin_frame(tmp, sizeof tmp);
    phc_w_map(&w, 2);
    phc_w_kv_text(&w, "t", "smsres");
    phc_w_kv_text(&w, "err", "at most 500 messages per sync");
    end_frame(&w);
    rc = phc_sms_sync(&sc, NULL, 0, NULL, 0, pend, 4, &np, &acc);
    CHECK(rc == PHC_ERR_SERVER && strstr(sc.errmsg, "500"), "server error surfaces (%d %s)", rc, sc.errmsg);
}

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
    memset(&ev, 0, sizeof ev); ev.on_tile = on_tile; ev.on_dialog = on_dialog; ev.on_download = on_download; ev.on_focus = on_focus;
    g_c = &c;
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
    phc_w_map(&w, 8); phc_w_kv_text(&w, "t", "page"); phc_w_kv_uint(&w, "pid", 1); phc_w_kv_uint(&w, "w", 360);
    phc_w_kv_bool(&w, "back", 1); phc_w_kv_bool(&w, "fwd", 0);
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
    phc_w_map(&w, 8); phc_w_kv_uint(&w, "id", 12); phc_w_kv_uint(&w, "layer", 2); phc_w_kv_uint(&w, "x", 4);
    phc_w_kv_uint(&w, "y", 4); phc_w_kv_uint(&w, "w", 40); phc_w_kv_uint(&w, "h", 20); phc_w_kv_text(&w, "kind", "link");
    phc_w_text(&w, "opts"); phc_w_array(&w, 2);
    phc_w_map(&w, 1); phc_w_kv_text(&w, "l", "One");
    phc_w_map(&w, 2); phc_w_kv_text(&w, "l", "Two"); phc_w_kv_bool(&w, "s", 1);
    end_frame(&w);
    w = begin_frame(tmp, sizeof tmp);
    phc_w_map(&w, 7); phc_w_kv_text(&w, "t", "focus"); phc_w_kv_uint(&w, "id", 10); phc_w_kv_text(&w, "kind", "text");
    phc_w_kv_text(&w, "type", "search"); phc_w_kv_text(&w, "value", "abc"); phc_w_kv_int(&w, "start", 3); phc_w_kv_int(&w, "end", -1); end_frame(&w);
    w = begin_frame(tmp, sizeof tmp);
    phc_w_map(&w, 5); phc_w_kv_text(&w, "t", "dialog"); phc_w_kv_uint(&w, "id", 7); phc_w_kv_text(&w, "kind", "prompt");
    phc_w_kv_text(&w, "msg", "name?"); phc_w_kv_text(&w, "def", "def"); end_frame(&w);
    w = begin_frame(tmp, sizeof tmp);
    phc_w_map(&w, 4); phc_w_kv_text(&w, "t", "download"); phc_w_kv_text(&w, "url", "data:x"); phc_w_kv_text(&w, "name", "x.bin");
    phc_w_kv_bool(&w, "refused", 1); end_frame(&w);
    w = begin_frame(tmp, sizeof tmp);
    phc_w_map(&w, 4); phc_w_kv_text(&w, "t", "done"); phc_w_kv_uint(&w, "pid", 1); phc_w_kv_uint(&w, "ms", 42);
    phc_w_kv_uint(&w, "tiles", 1); end_frame(&w);

    memcpy(conv, in_buf, in_len); conv_len = in_len;
    rc = phc_pump(&c);
    CHECK(rc == PHC_OK, "pump rc %d", rc);
    CHECK(c.page.pid == 1 && c.page.h == 1200 && c.page.trunc == 1 && !strcmp(c.page.url, "https://x/"), "page");
    CHECK(got_tiles == 1 && got_hash == 0xfedcba9876543210ull && c.stats.hash_xor == got_hash, "tile");
    CHECK(phc_tile_lookup(&c, 0, 0, 256) && phc_tile_lookup(&c, 0, 0, 256)->hash == got_hash, "cache");
    CHECK(c.nlayers == 2 && c.layers[1].kind == PHC_LAYER_STICKY && c.layers[1].has_top && c.layers[1].top == 48, "layers");
    CHECK(c.nregions == 3 && !strcmp(c.regions[1].href, "https://x/y") && c.regions[0].href[0] == 0, "regions");
    CHECK(c.done.ms == 42 && c.done.tiles == 1, "done");
    CHECK(c.page.back == 1 && c.page.fwd == 0, "history flags");
    CHECK(c.nopts == 2 && c.regions[2].opt_count == 2 && c.regions[2].opt_first == 0 && !strcmp(c.opts[1].label, "Two") && c.opts[1].selected && !c.opts[0].selected, "select options");
    CHECK(got_focus && c.focus.id == 10, "focus frame");
    CHECK(got_dialog_id == 7 && got_dialog_kind == PHC_DLG_PROMPT && !strcmp(got_dialog_msg, "name?"), "dialog frame");
    CHECK(got_dl, "download frame");
    {   /* the dialog reply we sent while pumping */
        phc_r r3;
        uint32_t m3, q3;
        int found = 0;
        /* out_buf holds the hello frame, then the dialogreply frame */
        size_t hello_len = 4 + (((size_t)out_buf[0] << 24) | ((size_t)out_buf[1] << 16) | ((size_t)out_buf[2] << 8) | out_buf[3]);
        phc_r_init(&r3, out_buf + hello_len + 4, out_len - hello_len - 4);
        if (!phc_r_map(&r3, &m3)) {
            for (q3 = 0; q3 < m3 && !r3.err; q3++) {
                const char *k; size_t kl;
                phc_r_text(&r3, &k, &kl);
                if (phc_text_eq(k, kl, "text")) { const char *v; size_t vl; phc_r_text(&r3, &v, &vl); found = phc_text_eq(v, vl, "Zed"); }
                else phc_r_skip(&r3);
            }
        }
        CHECK(found, "dialogreply frame carries the text");
    }

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

    sms_test();
    fuzz();
    printf(failures ? "selftest FAIL (%d)\n" : "selftest PASS\n", failures);
    return failures ? 1 : 0;
}
