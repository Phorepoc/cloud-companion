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
#ifndef PHC_MAX_OPTS
#define PHC_MAX_OPTS 256      /* <select> options across all regions of a page */
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
    void (*wait)(void *ctx);   /* optional: block until recv may succeed; called instead of spinning on PHC_WOULDBLOCK mid-frame */
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
    int back, fwd;                 /* history: can go back / forward */
    char url[PHC_URL_MAX];
    char title[PHC_TITLE_MAX];
} phc_page;

enum { PHC_LAYER_FIXED = 0, PHC_LAYER_STICKY = 1, PHC_LAYER_SCROLL = 2 };

typedef struct {
    int id, kind, x, y, w, h, z;
    int has_top, top, has_bottom, bottom;
    /* kind SCROLL (an inner overflow box): x,y,w,h = the visible box in document coordinates; the layer tiles cover
       cw x ch of content; scroll_x/scroll_y = the box's current scroll (starts at the server's sx/sy; the host changes it
       with phc_layer_scroll) */
    int cw, ch, sx, sy;
    int scroll_x, scroll_y;
} phc_layer;

enum { PHC_REGION_LINK = 0, PHC_REGION_BUTTON, PHC_REGION_INPUT, PHC_REGION_SELECT, PHC_REGION_TEXTAREA };

typedef struct {
    int id, layer, x, y, w, h, kind;    /* x,y relative to the layer (layer 0: document coordinates) */
    const char *href, *type, *name, *placeholder, *value, *label;   /* NUL-terminated, in the pool; "" when absent */
    int opt_first, opt_count;           /* <select>: the options are c->opts[opt_first .. opt_first + opt_count) */
} phc_region;

typedef struct { const char *label; int selected; } phc_option;

enum { PHC_FOCUS_NONE = 0, PHC_FOCUS_TEXT, PHC_FOCUS_TEXTAREA, PHC_FOCUS_SELECT };

/* what has keyboard focus after the last update: the phone opens its own keyboard when kind != NONE */
typedef struct {
    int id, kind, start, end;           /* id = region id (0 if none); start/end = caret/selection, -1 unknown */
    char type[16];                      /* HTML input type: text, search, email, url, tel, number, password, ... */
    char value[96];                     /* empty for password fields */
    char inputmode[16], enter[16];      /* inputmode and enterkeyhint */
} phc_focus;

/* auth = HTTP authentication (reply with phc_dialog_reply_auth); cert = certificate error (ok = continue anyway) */
enum { PHC_DLG_ALERT = 0, PHC_DLG_CONFIRM, PHC_DLG_PROMPT, PHC_DLG_BEFOREUNLOAD, PHC_DLG_AUTH, PHC_DLG_CERT };

/* a JavaScript dialog: the page is blocked until phc_dialog_reply (the server dismisses it after 60 s) */
typedef struct {
    int id, kind;
    char message[256], def[128];
} phc_dialog;

typedef struct { char url[256], name[64]; int refused; } phc_download;

typedef struct {
    int pid, seq, ms, tiles, bytes;     /* the last `done` frame */
    int more;                           /* progressive load: 1 = the first screenful is in, another `done` follows (pump again) */
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
    void (*on_focus)(void *user, const phc_focus *f);        /* after every update */
    void (*on_dialog)(void *user, const phc_dialog *d);      /* reply with phc_dialog_reply (may be called from here) */
    void (*on_download)(void *user, const phc_download *d);  /* a download was refused by the cloud */
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
    phc_option opts[PHC_MAX_OPTS];
    int nopts;
    phc_focus focus;
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
int phc_text(phc *c, int region_id, const char *text, int submit);   /* insert at the caret of region_id (0: the focused field) */
int phc_text_ex(phc *c, int region_id, const char *text, int submit, int replace);   /* replace = select the content first */
/* press a key in the focused field: Backspace, Enter, Tab, Delete, ArrowLeft/Right/Up/Down, Escape */
int phc_key(phc *c, const char *name);
int phc_select(phc *c, int region_id, int option_index);
int phc_dialog_reply(phc *c, int dialog_id, int ok, const char *text);
/* auth dialogs: ok = provide credentials (user, pass) / cancel */
int phc_dialog_reply_auth(phc *c, int dialog_id, int ok, const char *user, const char *pass);
/* per-device byte budget, stored by the server: JPEG quality 1..100 (0 = default), grayscale tiles */
int phc_settings(phc *c, int jpeg_quality, int gray);
/* scroll an inner scroll layer locally (content coordinates, clamped); the host redraws */
void phc_layer_scroll(phc *c, int layer_id, int dx, int dy);
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
