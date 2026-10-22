/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

/*
 * phc -- the portable phecloud client core (C99, no malloc, no OS calls).
 *
 * It speaks the phone protocol (internal/proto in the server repo): 4-byte big-endian length + a CBOR map per frame.
 * It does pairing and HMAC authentication, keeps a tile cache (layer, tile x/y, hash), the fixed/sticky layer list and the
 * tap-region list of the current page, hit-tests a screen point, and builds the input frames.
 *
 * It never draws and never decodes images: each received tile is handed to the host through the on_tile callback (the data
 * pointer is valid only during the call), so the host decodes PNG/JPEG into its own bitmap store.
 *
 * Transport: the host supplies send/recv (plain TCP on a PC, mbedTLS 1.3 on the phone). recv blocks until at least one byte
 * is available, returns the number of bytes read, 0 on end of stream, < 0 on error, or PHC_WOULDBLOCK when the transport is
 * non-blocking and nothing is available (honoured only between frames).
 *
 * Memory: a `phc` object holds every table (about 230 KB on a 32-bit target, 267 KB on x86-64, with the defaults below); put it in static storage or the heap once.
 * The caller supplies the receive buffer (it must hold the largest frame: a 360x256 tile is at most about 120 KB; use 192 KB).
 */
#ifndef PHC_H
#define PHC_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

#ifndef PHC_MAX_TILES
#define PHC_MAX_TILES 1024    /* cached tile entries (page 8192 px / 256 = 32 rows, plus layers) */
#endif
#ifndef PHC_MAX_LAYERS
#define PHC_MAX_LAYERS 8
#endif
#ifndef PHC_MAX_REGIONS
#define PHC_MAX_REGIONS 1536
#endif
#ifndef PHC_POOL_SIZE
#define PHC_POOL_SIZE (96 * 1024)  /* region strings (href, label, ...) */
#endif
#define PHC_URL_MAX 512
#define PHC_TITLE_MAX 128
#define PHC_TX_MAX 4096

#define PHC_OK 0
#define PHC_ERR_IO (-1)
#define PHC_ERR_PROTO (-2)
#define PHC_ERR_SERVER (-3)   /* an `error` frame: see phc.errmsg */
#define PHC_ERR_AUTH (-4)
#define PHC_ERR_BUF (-5)      /* frame larger than the receive buffer */
#define PHC_WOULDBLOCK (-100)

typedef struct {
    void *ctx;
    int (*send)(void *ctx, const void *p, size_t n);   /* sends all n bytes; returns n or < 0 */
    int (*recv)(void *ctx, void *p, size_t n);
} phc_transport;

typedef struct {
    char dev[40];        /* device id (hex text), set by pairing */
    uint8_t key[32];     /* device key */
    int have;            /* 1 when dev/key are valid */
} phc_creds;

enum { PHC_FMT_PNG = 0, PHC_FMT_JPEG = 1 };

typedef struct {
    int pid, layer, x, y, w, h;   /* x,y relative to the layer (layer 0 = the page) */
    int fmt;
    uint64_t hash;
    const uint8_t *data;
    size_t len;
} phc_tile;

typedef struct {
    int pid, seq, w, h, tw, th, trunc;
    char url[PHC_URL_MAX];
    char title[PHC_TITLE_MAX];
} phc_page;

enum { PHC_LAYER_FIXED = 0, PHC_LAYER_STICKY = 1 };

typedef struct {
    int id, kind, x, y, w, h, z;
    int has_top, top, has_bottom, bottom;
} phc_layer;

enum { PHC_REGION_LINK = 0, PHC_REGION_BUTTON, PHC_REGION_INPUT, PHC_REGION_SELECT, PHC_REGION_TEXTAREA };

typedef struct {
    int id, layer, x, y, w, h, kind;    /* x,y relative to the layer (layer 0: document coordinates) */
    const char *href, *type, *name, *placeholder, *value, *label;   /* NUL-terminated, in the pool; "" when absent */
} phc_region;

typedef struct {
    int pid, seq, ms, tiles, bytes;     /* the last `done` frame */
} phc_done;

typedef struct {
    long tiles, tile_bytes;
    uint64_t hash_xor;
} phc_stats;

typedef struct {
    int layer;                          /* layer under the point (0 = page) */
    int tap_layer, tap_x, tap_y;        /* what the `input tap` frame carries */
    const phc_region *region;           /* NULL when the point is on no tap region */
} phc_hit;

typedef struct {
    void (*on_page)(void *user, const phc_page *p);
    void (*on_tile)(void *user, const phc_tile *t);
    void *user;
} phc_events;

typedef struct {
    int pid, layer, x, y, w, h;
    uint64_t hash;
    int used;
} phc_tile_entry;

typedef struct phc {
    phc_transport io;
    phc_events ev;
    uint8_t *rx;
    size_t rx_cap;
    uint8_t tx[PHC_TX_MAX];
    uint8_t nonce[32];
    size_t nonce_len;

    phc_page page;
    phc_done done;
    phc_stats stats;
    char errmsg[96];

    phc_tile_entry tiles[PHC_MAX_TILES];
    int ntiles;
    phc_layer layers[PHC_MAX_LAYERS];
    int nlayers;
    phc_region regions[PHC_MAX_REGIONS];
    int nregions, regions_dropped;
    char pool[PHC_POOL_SIZE];
    size_t pool_used;
} phc;

void phc_init(phc *c, phc_transport io, uint8_t *rxbuf, size_t rxcap);
void phc_set_events(phc *c, phc_events ev);
void phc_stats_reset(phc *c);

/* Pair (code != NULL: consumes the code, fills cr) or authenticate with cr (code == NULL). Returns PHC_OK. */
int phc_auth(phc *c, phc_creds *cr, const char *code, const char *devname);

/* requests; the answer arrives through phc_pump */
int phc_open(phc *c, const char *url, int width);
int phc_navigate(phc *c, const char *url);               /* url */
int phc_navigate_action(phc *c, const char *action);     /* "back" | "forward" | "reload" */
int phc_scroll(phc *c, int y);
int phc_text(phc *c, int region_id, const char *text, int submit);
int phc_ping(phc *c);
/* hit-test a screen point (scroll_y = the client's scroll, view_h = viewport height) and send a tap input frame */
int phc_tap_screen(phc *c, int scroll_y, int view_h, int sx, int sy, phc_hit *out);

/* read frames until a `done` (returns PHC_OK, c->done set) or an error / PHC_WOULDBLOCK */
int phc_pump(phc *c);

/* the tile cache / hit-test, usable without the network */
int phc_hit_test(const phc *c, int scroll_y, int view_h, int sx, int sy, phc_hit *out);
int phc_layer_screen_y(const phc_layer *l, int scroll_y, int view_h);
const phc_tile_entry *phc_tile_lookup(const phc *c, int layer, int x, int y);

#ifdef __cplusplus
}
#endif
#endif
