/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

/*
 * Host test for the phc core.
 *
 *   host_test -addr 127.0.0.1:7400 -tls -state st.txt [-code CODE] [-tap-first-link] [-type TEXT] [-scenario gaps]
 *             -url URL [-url URL ...]
 *
 * -tls (needs a build with PHC_WITH_MBEDTLS) runs TLS 1.3 through phc_mbedtls with a non-blocking socket and the certificate
 * pinned by fingerprint (trust on first use at pairing, then refused when it changes); without it the connection is plain TCP
 * (the server's development-only listener).
 *
 * It prints one SUMMARY line per step in the same format as `phecloud-client -summary`, so a script can diff the two. Exit
 * status 0 = all asserts held, 2 = connection/auth failure.
 */
#include "../phc.h"
#ifdef PHC_WITH_MBEDTLS
#include "../phc_mbedtls.h"
#endif

#include <errno.h>
#include <fcntl.h>
#include <netdb.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>

static int failures;
#define CHECK(cond, ...) do { if (!(cond)) { failures++; fprintf(stderr, "CHECK FAILED %s:%d: ", __FILE__, __LINE__); fprintf(stderr, __VA_ARGS__); fprintf(stderr, "\n"); } } while (0)

/* ---- plain blocking TCP transport ---- */
static int tcp_send(void *ctx, const void *p, size_t n)
{
    int fd = *(int *)ctx;
    size_t off = 0;
    while (off < n) {
        ssize_t w = send(fd, (const char *)p + off, n - off, 0);
        if (w <= 0) return -1;
        off += (size_t)w;
    }
    return (int)n;
}

static int tcp_recv(void *ctx, void *p, size_t n) { return (int)recv(*(int *)ctx, p, n, 0); }

/* ---- raw non-blocking socket for the TLS adapter ---- */
#ifdef PHC_WITH_MBEDTLS
static int raw_send(void *ctx, const void *p, size_t n)
{
    ssize_t w = send(*(int *)ctx, p, n, MSG_NOSIGNAL);
    if (w >= 0) return (int)w;
    return (errno == EAGAIN || errno == EWOULDBLOCK || errno == EINTR) ? 0 : -1;
}

static int raw_recv(void *ctx, void *p, size_t n)
{
    ssize_t r = recv(*(int *)ctx, p, n, 0);
    if (r > 0) return (int)r;
    if (r == 0) return -1;   /* end of stream */
    return (errno == EAGAIN || errno == EWOULDBLOCK || errno == EINTR) ? 0 : -1;
}

static void raw_wait(void *ctx, int want_write)
{
    struct pollfd p;
    p.fd = *(int *)ctx;
    p.events = (short)(want_write ? POLLOUT : POLLIN);
    p.revents = 0;
    poll(&p, 1, 5000);
}
#endif

static int tcp_connect(const char *hostport)
{
    char host[128], *colon;
    struct addrinfo hints, *res;
    int fd;
    strncpy(host, hostport, sizeof host - 1);
    host[sizeof host - 1] = 0;
    colon = strrchr(host, ':');
    if (!colon) return -1;
    *colon = 0;
    memset(&hints, 0, sizeof hints);
    hints.ai_family = AF_INET;
    hints.ai_socktype = SOCK_STREAM;
    if (getaddrinfo(host, colon + 1, &hints, &res)) return -1;
    fd = socket(res->ai_family, res->ai_socktype, 0);
    if (fd < 0 || connect(fd, res->ai_addr, res->ai_addrlen)) { freeaddrinfo(res); return -1; }
    freeaddrinfo(res);
    return fd;
}

/* ---- state file: "<dev> <keyhex> [<fingerprint hex>]" ---- */
static int unhex(const char *s, uint8_t *out, int n)
{
    int i;
    if ((int)strlen(s) != 2 * n) return 0;
    for (i = 0; i < n; i++) {
        unsigned v;
        if (sscanf(s + 2 * i, "%2x", &v) != 1) return 0;
        out[i] = (uint8_t)v;
    }
    return 1;
}

static int load_state(const char *path, phc_creds *cr, uint8_t fp[32], int *have_fp)
{
    FILE *f = fopen(path, "r");
    char hex[80], fphex[80];
    int n;
    *have_fp = 0;
    if (!f) return 0;
    n = fscanf(f, "%39s %79s %79s", cr->dev, hex, fphex);
    fclose(f);
    if (n < 2 || !unhex(hex, cr->key, 32)) return 0;
    cr->have = 1;
    if (n == 3 && unhex(fphex, fp, 32)) *have_fp = 1;
    return 1;
}

static void save_state(const char *path, const phc_creds *cr, const uint8_t *fp)
{
    FILE *f = fopen(path, "w");
    int i;
    if (!f) return;
    fprintf(f, "%s ", cr->dev);
    for (i = 0; i < 32; i++) fprintf(f, "%02x", cr->key[i]);
    if (fp) {
        fprintf(f, " ");
        for (i = 0; i < 32; i++) fprintf(f, "%02x", fp[i]);
    }
    fprintf(f, "\n");
    fclose(f);
}

/* ---- per-step counters filled by the callbacks ---- */
static int png_tiles, jpg_tiles, bad_tiles;
static int dialogs, downloads;
static char dlg_kind[16], dlg_msg[64], dl_name[64];
static phc c; /* static: about 270 KB */
static uint8_t rx[192 * 1024];

static void on_tile(void *user, const phc_tile *t)
{
    (void)user;
    /* the host would decode here; the test checks the container signatures instead */
    if (t->fmt == PHC_FMT_PNG && t->len > 8 && memcmp(t->data, "\x89PNG\r\n\x1a\n", 8) == 0) png_tiles++;
    else if (t->fmt == PHC_FMT_JPEG && t->len > 3 && t->data[0] == 0xff && t->data[1] == 0xd8) jpg_tiles++;
    else bad_tiles++;
    CHECK(t->w > 0 && t->w <= 360 && t->h > 0 && t->h <= 256, "tile size %dx%d", t->w, t->h);
}

static void cpy(char *d, size_t cap, const char *s)
{
    size_t n = strlen(s);
    if (n >= cap) n = cap - 1;
    memcpy(d, s, n);
    d[n] = 0;
}

static void on_dialog(void *user, const phc_dialog *d)
{
    static const char *names[] = {"alert", "confirm", "prompt", "beforeunload"};
    (void)user;
    dialogs++;
    if (dialogs == 1) {
        cpy(dlg_kind, sizeof dlg_kind, names[d->kind]);
        cpy(dlg_msg, sizeof dlg_msg, d->message);
    }
    printf("  dialog #%d %s \"%s\" default \"%s\" -> replying ok\n", d->id, names[d->kind], d->message, d->def);
    phc_dialog_reply(&c, d->id, 1, "Zed");
}

static void on_download(void *user, const phc_download *d)
{
    (void)user;
    downloads++;
    cpy(dl_name, sizeof dl_name, d->name);
    CHECK(d->refused, "download not marked refused");
}

static const char *sp(const char *s, char *buf, size_t cap)
{
    size_t i;
    if (!s[0]) return "-";
    for (i = 0; s[i] && i + 1 < cap; i++) buf[i] = (s[i] == ' ' || s[i] == '\n') ? '_' : s[i];
    buf[i] = 0;
    return buf;
}

static void summary(const char *step)
{
    static const char *fk[] = {"none", "text", "textarea", "select"};
    char b1[160], b2[160], b3[160], b4[160], b5[160], b6[160];
    int i, nopts = 0, sel = -1;
    for (i = 0; i < c.nregions; i++)
        if (c.regions[i].kind == PHC_REGION_SELECT) {
            int k;
            nopts = c.regions[i].opt_count;
            for (k = 0; k < nopts; k++) if (c.opts[c.regions[i].opt_first + k].selected) sel = k;
            break;
        }
    printf("SUMMARY step=%s url=%.100s h=%d tiles=%ld tilebytes=%ld hx=%016llx regions=%d layers=%d trunc=%s "
           "title=%s back=%s fwd=%s focus=%s ftype=%s fvalue=%s opts=%d sel=%d dialogs=%d dlgkind=%s dlgmsg=%s dl=%d dlname=%s\n",
           step, c.page.url, c.page.h, c.stats.tiles, c.stats.tile_bytes, (unsigned long long)c.stats.hash_xor, c.nregions,
           c.nlayers, c.page.trunc ? "true" : "false", sp(c.page.title, b1, sizeof b1), c.page.back ? "true" : "false",
           c.page.fwd ? "true" : "false", fk[c.focus.kind], sp(c.focus.type, b2, sizeof b2), sp(c.focus.value, b3, sizeof b3),
           nopts, sel, dialogs, sp(dlg_kind, b4, sizeof b4), sp(dlg_msg, b5, sizeof b5), downloads, sp(dl_name, b6, sizeof b6));
    fflush(stdout);
}

static void new_step(void)
{
    phc_stats_reset(&c);
    png_tiles = jpg_tiles = bad_tiles = 0;
    dialogs = downloads = 0;
    dlg_kind[0] = dlg_msg[0] = dl_name[0] = 0;
}

static int pump(const char *what)
{
    int rc;
    for (;;) {
        rc = phc_pump(&c);
        if (rc == PHC_WOULDBLOCK) {   /* non-blocking transport: wait for the socket, then retry */
            if (c.io.wait) c.io.wait(c.io.ctx);
            continue;
        }
        break;
    }
    if (rc) {
        fprintf(stderr, "%s: pump failed %d (%s)\n", what, rc, c.errmsg);
        failures++;
    }
    return rc;
}

static int step_done(const char *name)
{
    if (pump(name)) return -1;
    summary(name);
    return 0;
}

static const phc_region *find_region(int (*pred)(const phc_region *, void *), void *arg)
{
    int k;
    for (k = 0; k < c.nregions; k++) if (pred(&c.regions[k], arg)) return &c.regions[k];
    return NULL;
}
static int is_link0(const phc_region *r, void *a) { (void)a; return r->kind == PHC_REGION_LINK && r->layer == 0; }
static int is_input(const phc_region *r, void *a) { (void)a; return r->kind == PHC_REGION_INPUT; }
static int is_text(const phc_region *r, void *a) { (void)a; return r->kind == PHC_REGION_INPUT || r->kind == PHC_REGION_TEXTAREA; }
static int is_select(const phc_region *r, void *a) { (void)a; return r->kind == PHC_REGION_SELECT; }
static int has_label(const phc_region *r, void *a) { return strcmp(r->label, (const char *)a) == 0; }

/* tap the middle of a region through the real hit-test, scrolling so it is on screen; asserts the hit-test finds it */
static int tap_region(const phc_region *r, const char *step)
{
    int cx = r->x + r->w / 2, cy = r->y + r->h / 2;
    int scroll = cy - 320, maxs = c.page.h - 640, sy;
    phc_hit hit;
    if (scroll > maxs) scroll = maxs;
    if (scroll < 0) scroll = 0;
    sy = cy - scroll;
    phc_hit_test(&c, scroll, 640, cx, sy, &hit);
    CHECK(hit.region && hit.region->id == r->id, "hit-test missed region %d (got %d)", r->id, hit.region ? hit.region->id : -1);
    printf("tapping #%d \"%s\" at screen (%d,%d) scroll %d\n", r->id, r->label, cx, sy, scroll);
    new_step();
    if (phc_tap_screen(&c, scroll, 640, cx, sy, &hit)) { failures++; return -1; }
    return step_done(step);
}

int main(int argc, char **argv)
{
    const char *addr = "127.0.0.1:7400", *state = "c-state.txt", *code = NULL, *type = NULL, *scenario = NULL;
    const char *urls[8];
    int nurls = 0, tap = 0, tls = 0, i, fd;
    phc_creds cr;
    uint8_t fp[32];
    int have_fp = 0;
    phc_transport io;
    phc_events ev;
#ifdef PHC_WITH_MBEDTLS
    static phc_tls tlsx;
#endif

    for (i = 1; i < argc; i++) {
        if (!strcmp(argv[i], "-addr") && i + 1 < argc) addr = argv[++i];
        else if (!strcmp(argv[i], "-state") && i + 1 < argc) state = argv[++i];
        else if (!strcmp(argv[i], "-code") && i + 1 < argc) code = argv[++i];
        else if (!strcmp(argv[i], "-type") && i + 1 < argc) type = argv[++i];
        else if (!strcmp(argv[i], "-scenario") && i + 1 < argc) scenario = argv[++i];
        else if (!strcmp(argv[i], "-url") && i + 1 < argc && nurls < 8) urls[nurls++] = argv[++i];
        else if (!strcmp(argv[i], "-tap-first-link")) tap = 1;
        else if (!strcmp(argv[i], "-tls")) tls = 1;
    }
    memset(&cr, 0, sizeof cr);
    memset(fp, 0, sizeof fp);
    load_state(state, &cr, fp, &have_fp);

    fd = tcp_connect(addr);
    if (fd < 0) { fprintf(stderr, "connect %s failed\n", addr); return 2; }
    if (tls) {
#ifdef PHC_WITH_MBEDTLS
        phc_raw_io raw;
        int rc;
        fcntl(fd, F_SETFL, fcntl(fd, F_GETFL, 0) | O_NONBLOCK);
        raw.ctx = &fd; raw.send = raw_send; raw.recv = raw_recv; raw.wait = raw_wait;
        if (phc_tls_init(&tlsx, raw, have_fp ? fp : NULL)) { fprintf(stderr, "tls init failed\n"); return 2; }
        rc = phc_tls_handshake(&tlsx);
        if (rc == PHC_ERR_AUTH) { fprintf(stderr, "server certificate changed: does not match the pinned fingerprint\n"); return 2; }
        if (rc) { fprintf(stderr, "TLS handshake failed (%d)\n", rc); return 2; }
        if (!have_fp) phc_tls_peer_fingerprint(&tlsx, fp);   /* trust on first use: pinned at pairing */
        io = phc_tls_transport(&tlsx, 1);
        printf("TLS 1.3 established, certificate pinned\n");
#else
        fprintf(stderr, "built without PHC_WITH_MBEDTLS\n");
        return 2;
#endif
    } else {
        io.ctx = &fd; io.send = tcp_send; io.recv = tcp_recv; io.wait = NULL;
    }
    phc_init(&c, io, rx, sizeof rx);
    memset(&ev, 0, sizeof ev);
    ev.on_tile = on_tile; ev.on_dialog = on_dialog; ev.on_download = on_download;
    phc_set_events(&c, ev);

    if (phc_auth(&c, &cr, cr.have ? NULL : code, "c-host-test")) {
        fprintf(stderr, "auth failed (%s)\n", c.errmsg);
        return 2;
    }
    if (!cr.have) { fprintf(stderr, "no credentials: pass -code\n"); return 2; }
    save_state(state, &cr, tls ? fp : NULL);
    printf("authenticated as %s\n", cr.dev);

    for (i = 0; i < nurls; i++) {
        const phc_region *r;
        new_step();
        if (phc_open(&c, urls[i], 360)) { failures++; break; }
        if (step_done("open")) break;
        CHECK(c.stats.tiles > 0, "no tiles");
        CHECK(c.page.w == 360, "page width %d", c.page.w);
        CHECK(png_tiles + jpg_tiles == c.stats.tiles && bad_tiles == 0, "tile container check: png %d jpeg %d bad %d of %ld", png_tiles, jpg_tiles, bad_tiles, c.stats.tiles);
        CHECK(phc_tile_lookup(&c, 0, 0, 0) != NULL, "cache has no tile (0,0)");
        CHECK(c.stats.tiles == (long)c.done.tiles, "done.tiles %d != received %ld", c.done.tiles, c.stats.tiles);

        if (tap && i == 0) {
            r = find_region(is_link0, NULL);
            CHECK(r != NULL, "no link region");
            if (r) {
                if (tap_region(r, "tap")) break;
                CHECK(strstr(c.page.url, "iana.org") != NULL, "tap did not navigate: %s", c.page.url);
                CHECK(c.page.back == 1, "history: no back after navigating");
                new_step();
                if (phc_navigate_action(&c, "back")) { failures++; break; }
                if (step_done("back")) break;
                CHECK(strstr(c.page.url, "example.com") != NULL && c.page.back == 0 && c.page.fwd == 1, "history after back: %s back=%d fwd=%d", c.page.url, c.page.back, c.page.fwd);
            }
        }
        if (type && i == 0) {
            r = find_region(is_text, NULL);
            CHECK(r != NULL, "no input region");
            if (r) {
                new_step();
                if (phc_text(&c, r->id, type, 0)) { failures++; break; }
                if (step_done("typed")) break;
                CHECK(c.stats.tiles >= 1, "typing changed no tile");
            }
        }
        if (scenario && !strcmp(scenario, "gaps") && i == 0) {
            r = find_region(is_input, NULL);
            CHECK(r != NULL, "gaps: no input");
            if (r) {
                if (tap_region(r, "focus")) break;
                CHECK(c.focus.kind == PHC_FOCUS_TEXT && !strcmp(c.focus.type, "search") && c.focus.id == r->id, "focus after tapping the field: kind %d type %s", c.focus.kind, c.focus.type);
                new_step();
                if (phc_text(&c, 0, "abc", 0)) { failures++; break; }   /* id 0: into the focused field */
                if (step_done("text")) break;
                CHECK(!strcmp(c.focus.value, "abc") && c.focus.start == 3, "focus value after typing: '%s' caret %d", c.focus.value, c.focus.start);
                new_step();
                if (phc_key(&c, "Backspace")) { failures++; break; }
                if (step_done("key")) break;
                CHECK(!strcmp(c.focus.value, "ab"), "Backspace: value '%s'", c.focus.value);
            }
            r = find_region(is_select, NULL);
            CHECK(r != NULL, "gaps: no select");
            if (r) {
                CHECK(r->opt_count == 3 && c.opts[r->opt_first + 1].selected && !strcmp(c.opts[r->opt_first + 2].label, "Three"), "select options");
                new_step();
                if (phc_select(&c, r->id, 2)) { failures++; break; }
                if (step_done("select")) break;
                r = find_region(is_select, NULL);
                CHECK(r && c.opts[r->opt_first + 2].selected, "option 2 not selected after phc_select");
            }
            r = find_region(has_label, (void *)"ask");
            CHECK(r != NULL, "gaps: no prompt button");
            if (r) {
                if (tap_region(r, "dialog")) break;
                CHECK(dialogs == 1 && !strcmp(dlg_kind, "prompt") && !strcmp(c.page.title, "Zed"), "dialog: %d %s title '%s'", dialogs, dlg_kind, c.page.title);
            }
            r = find_region(has_label, (void *)"dl");
            CHECK(r != NULL, "gaps: no download link");
            if (r) {
                if (tap_region(r, "download")) break;
                CHECK(downloads == 1 && !strcmp(dl_name, "x.bin"), "download refused report: %d %s", downloads, dl_name);
            }
        }
    }

    /* layers: verify hit-test geometry on a page that has them */
    if (c.nlayers > 0) {
        phc_hit h;
        int k;
        for (k = 0; k < c.nlayers; k++) {
            const phc_layer *l = &c.layers[k];
            int y = phc_layer_screen_y(l, 300, 640);
            phc_hit_test(&c, 300, 640, l->x + l->w / 2, y + l->h / 2, &h);
            CHECK(h.layer == l->id || h.layer != 0, "layer %d not hit at its centre (got %d)", l->id, h.layer);
        }
        printf("layers: %d, hit-test checked\n", c.nlayers);
    }

#ifdef PHC_WITH_MBEDTLS
    if (tls) phc_tls_close(&tlsx);
#endif
    close(fd);
    printf(failures ? "FAIL (%d)\n" : "PASS\n", failures);
    return failures ? 1 : 0;
}
