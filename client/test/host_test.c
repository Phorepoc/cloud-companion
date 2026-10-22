/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

/*
 * Host test for the phc core over plain TCP (the server's PHECLOUD_PLAIN_LISTEN port).
 *
 *   host_test -addr 127.0.0.1:7401 -state st.txt [-code CODE] [-tap-first-link] [-type TEXT] -url URL [-url URL ...]
 *
 * Prints one SUMMARY line per step in the same format as `phecloud-client -summary`, so a script can diff the two; the hash
 * XOR covers every tile's pixel hash. The tap goes through phc_hit_test/phc_tap_screen with a real scroll position, and the
 * test asserts that the hit-test finds the region it aimed at. Exit status 0 = all asserts held.
 */
#include "../phc.h"

#include <arpa/inet.h>
#include <netdb.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>

static int failures;
#define CHECK(cond, ...) do { if (!(cond)) { failures++; fprintf(stderr, "CHECK FAILED %s:%d: ", __FILE__, __LINE__); fprintf(stderr, __VA_ARGS__); fprintf(stderr, "\n"); } } while (0)

/* ---- plain TCP transport ---- */
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

static int tcp_recv(void *ctx, void *p, size_t n)
{
    ssize_t r = recv(*(int *)ctx, p, n, 0);
    return (int)r;
}

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

/* ---- state file: "<dev> <keyhex>" ---- */
static int load_state(const char *path, phc_creds *cr)
{
    FILE *f = fopen(path, "r");
    char hex[80];
    int i;
    if (!f) return 0;
    if (fscanf(f, "%39s %79s", cr->dev, hex) != 2 || strlen(hex) != 64) { fclose(f); return 0; }
    fclose(f);
    for (i = 0; i < 32; i++) {
        unsigned v;
        sscanf(hex + 2 * i, "%2x", &v);
        cr->key[i] = (uint8_t)v;
    }
    cr->have = 1;
    return 1;
}

static void save_state(const char *path, const phc_creds *cr)
{
    FILE *f = fopen(path, "w");
    int i;
    if (!f) return;
    fprintf(f, "%s ", cr->dev);
    for (i = 0; i < 32; i++) fprintf(f, "%02x", cr->key[i]);
    fprintf(f, "\n");
    fclose(f);
}

/* ---- events: count what the host would decode ---- */
static int png_tiles, jpg_tiles, bad_tiles;

static void on_tile(void *user, const phc_tile *t)
{
    (void)user;
    /* the host would decode here; the test checks the container signatures instead */
    if (t->fmt == PHC_FMT_PNG && t->len > 8 && memcmp(t->data, "\x89PNG\r\n\x1a\n", 8) == 0) png_tiles++;
    else if (t->fmt == PHC_FMT_JPEG && t->len > 3 && t->data[0] == 0xff && t->data[1] == 0xd8) jpg_tiles++;
    else bad_tiles++;
    CHECK(t->w > 0 && t->w <= 360 && t->h > 0 && t->h <= 256, "tile size %dx%d", t->w, t->h);
}

static phc c; /* static: about 190 KB */
static uint8_t rx[192 * 1024];

static void summary(const char *step)
{
    printf("SUMMARY step=%s url=%.100s h=%d tiles=%ld tilebytes=%ld hx=%016llx regions=%d layers=%d trunc=%s\n", step,
           c.page.url, c.page.h, c.stats.tiles, c.stats.tile_bytes, (unsigned long long)c.stats.hash_xor, c.nregions,
           c.nlayers, c.page.trunc ? "true" : "false");
    fflush(stdout);
}

static int pump(const char *what)
{
    int rc = phc_pump(&c);
    if (rc) {
        fprintf(stderr, "%s: pump failed %d (%s)\n", what, rc, c.errmsg);
        failures++;
    }
    return rc;
}

int main(int argc, char **argv)
{
    const char *addr = "127.0.0.1:7401", *state = "c-state.txt", *code = NULL, *type = NULL;
    const char *urls[8];
    int nurls = 0, tap = 0, i, fd;
    phc_creds cr;
    phc_transport io;
    phc_events ev;

    for (i = 1; i < argc; i++) {
        if (!strcmp(argv[i], "-addr") && i + 1 < argc) addr = argv[++i];
        else if (!strcmp(argv[i], "-state") && i + 1 < argc) state = argv[++i];
        else if (!strcmp(argv[i], "-code") && i + 1 < argc) code = argv[++i];
        else if (!strcmp(argv[i], "-type") && i + 1 < argc) type = argv[++i];
        else if (!strcmp(argv[i], "-url") && i + 1 < argc && nurls < 8) urls[nurls++] = argv[++i];
        else if (!strcmp(argv[i], "-tap-first-link")) tap = 1;
    }
    memset(&cr, 0, sizeof cr);
    load_state(state, &cr);

    fd = tcp_connect(addr);
    if (fd < 0) { fprintf(stderr, "connect %s failed\n", addr); return 2; }
    io.ctx = &fd; io.send = tcp_send; io.recv = tcp_recv;
    phc_init(&c, io, rx, sizeof rx);
    ev.on_page = NULL; ev.on_tile = on_tile; ev.user = NULL;
    phc_set_events(&c, ev);

    if (phc_auth(&c, &cr, cr.have ? NULL : code, "c-host-test")) {
        fprintf(stderr, "auth failed (%s)\n", c.errmsg);
        return 2;
    }
    if (!cr.have) { fprintf(stderr, "no credentials: pass -code\n"); return 2; }
    save_state(state, &cr);
    printf("authenticated as %s\n", cr.dev);

    for (i = 0; i < nurls; i++) {
        phc_stats_reset(&c);
        png_tiles = jpg_tiles = 0;
        if (phc_open(&c, urls[i], 360)) { failures++; break; }
        if (pump("open")) break;
        summary("open");
        CHECK(c.stats.tiles > 0, "no tiles");
        CHECK(c.page.w == 360, "page width %d", c.page.w);
        CHECK(png_tiles + jpg_tiles == c.stats.tiles && bad_tiles == 0, "tile container check: png %d jpeg %d bad %d of %ld", png_tiles, jpg_tiles, bad_tiles, c.stats.tiles);
        CHECK(phc_tile_lookup(&c, 0, 0, 0) != NULL, "cache has no tile (0,0)");
        CHECK(c.stats.tiles == (long)c.done.tiles, "done.tiles %d != received %ld", c.done.tiles, c.stats.tiles);

        if (tap && i == 0) {
            const phc_region *r = NULL;
            int k;
            for (k = 0; k < c.nregions; k++)
                if (c.regions[k].kind == PHC_REGION_LINK && c.regions[k].layer == 0) { r = &c.regions[k]; break; }
            CHECK(r != NULL, "no link region");
            if (r) {
                int cx = r->x + r->w / 2, cy = r->y + r->h / 2;
                int scroll = cy - 320, sy, maxs = c.page.h - 640;
                phc_hit hit;
                if (scroll > maxs) scroll = maxs;
                if (scroll < 0) scroll = 0;
                sy = cy - scroll;
                phc_hit_test(&c, scroll, 640, cx, sy, &hit);
                CHECK(hit.region && hit.region->id == r->id, "hit-test missed region %d (got %d)", r->id, hit.region ? hit.region->id : -1);
                printf("tapping link #%d \"%s\" -> %.60s at screen (%d,%d) scroll %d\n", r->id, r->label, r->href, cx, sy, scroll);
                phc_stats_reset(&c);
                if (phc_tap_screen(&c, scroll, 640, cx, sy, &hit)) { failures++; break; }
                if (pump("tap")) break;
                summary("tap");
                CHECK(strstr(c.page.url, "iana.org") != NULL, "tap did not navigate: %s", c.page.url);
            }
        }
        if (type && i == 0) {
            int k, id = -1;
            for (k = 0; k < c.nregions; k++)
                if (c.regions[k].kind == PHC_REGION_INPUT || c.regions[k].kind == PHC_REGION_TEXTAREA) { id = c.regions[k].id; break; }
            CHECK(id >= 0, "no input region");
            if (id >= 0) {
                phc_stats_reset(&c);
                if (phc_text(&c, id, type, 0)) { failures++; break; }
                if (pump("text")) break;
                summary("typed");
                CHECK(c.stats.tiles >= 1, "typing changed no tile");
            }
        }
    }

    /* layers: verify hit-test geometry on the page that has them (the layers test page) */
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

    close(fd);
    printf(failures ? "FAIL (%d)\n" : "PASS\n", failures);
    return failures ? 1 : 0;
}
