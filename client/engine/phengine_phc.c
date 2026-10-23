/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

/*
 * phengine_phc.c -- the Phecloud web engine for the Phorepoc browser: phewebengine.h implemented over the portable phc client
 * core (../phc*.c) and its mbedTLS 1.3 adapter. The page is rendered by the user's own phecloud server (Chromium); this engine
 * receives tiles, keeps them, composes the visible part into a BGRA view buffer, scrolls locally and sends taps and text.
 *
 * Licence: Apache-2.0. Links only permissive code: mbedTLS (Apache-2.0), libpng (libpng licence), libjpeg (IJG), Open C.
 *
 * Threads: the seam calls (UI thread) only queue a request or read cached state under a mutex. Each view has ONE network
 * thread that owns its phc object: connect, TLS 1.3 + pin, pair/authenticate, then loop { send queued requests; phc_pump
 * (non-blocking); compose when something changed; wait on the socket }.
 *
 * Settings: C:\data\phecloud.ini, lines "key=value":  host, port (default 7400), pin (64 hex = SHA-256 of the server
 * certificate), code (a pairing code: used once, then replaced by dev= and key= which the engine writes back), dev, key, name.
 * Pixels: BGRA 8888 premultiplied (opaque), view_w x view_h. The page is 360 px wide (the server's tile grid); a wider view
 * centres it.
 */
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <setjmp.h>
#include <unistd.h>
#include <fcntl.h>
#include <errno.h>
#include <pthread.h>
#include <sys/types.h>
#include <sys/time.h>
#include <sys/select.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <netdb.h>

#include <png.h>
#include <jpeglib.h>

#include <phorepoc/phewebengine.h>
#include "../phc.h"
#include "../phc_mbedtls.h"

#define MAXV 2                      /* views: one connection each (phc object + 192 KB receive buffer + TLS state) */
#define RXCAP (192 * 1024)
#define PAGE_W 360
#define INI_PATH "C:\\data\\phecloud.ini"
#define MAXDEC 12                   /* decoded tiles kept (360x256x4 = 368 KB each) */
#define NREQ 24
#define EVQ 128

#ifdef PHC_TRACE_TILES                  /* -DPHC_TRACE_TILES: log every tile, decode and compose */
#define PHC_TRACE(x) x
#define PHC_TRACE_ON 1
#else
#define PHC_TRACE(x)
#define PHC_TRACE_ON 0
#endif
extern void phc_log(const char *msg);
extern void phc_mem(const char *tag);       /* heap use of this thread, logged */       /* phewebengine_phecloud.cpp: RDebug + C:\data\phecloud_trace.txt */

enum { RQ_LOAD = 1, RQ_ACTION, RQ_DOWN, RQ_TAP, RQ_CANCEL, RQ_TEXT, RQ_KEY, RQ_SCROLL, RQ_DIALOG, RQ_SUSPEND, RQ_STOP };

typedef struct { int kind, a, b, ok; char s[PHC_URL_MAX]; char s2[64]; } req_t;

typedef struct {
    int layer, x, y, w, h, fmt;
    uint8_t *enc; size_t enclen;      /* the encoded tile as received (kept: re-decode is cheaper than re-download) */
    uint32_t *dec; unsigned stamp;    /* decoded BGRA, or NULL */
    int used;
} tile_t;

typedef struct view {
    int used;
    PheView id;
    int w, h, visible;
    pthread_t thr;
    int thr_on;
    volatile int quit;
    pthread_mutex_t mu;               /* requests + cached state */
    pthread_mutex_t pixmu;            /* pixel buffer + tile store while composing */
    req_t rq[NREQ];
    int rqh, rqt;
    /* cached state for get_state / get_str */
    char url[PHC_URL_MAX], title[PHC_TITLE_MAX], err[160], focus_url[PHC_URL_MAX];
    char dlg_text[256], dlg_def[128];
    int dlg_id, dlg_kind;
    int loading, progress, can_back, can_fwd, sec, scroll_y, page_h, page_trunc, focus, dirty;
    int discarded, pid;
    int down, down_x, down_y, moved, hl_active, hl_x, hl_y, hl_w, hl_h;
    long scroll_sent_ms; int scroll_unsent;
    /* pixels */
    uint32_t *px;
    /* tiles */
    tile_t *tiles; int ntiles; unsigned stamp;
    /* thread-owned */
    phc *c; uint8_t *rx; phc_tls tls; int sock; phc_creds cr;
} view_t;

static view_t views[MAXV];
static PheNotify notify_fn;
static void *notify_ctx;
static pthread_mutex_t gmu = PTHREAD_MUTEX_INITIALIZER;
static PheEvent evq[EVQ];
static int evh, evt;
static PheView nextid = 1;
static char useragent_unused;

/* ---- small helpers -------------------------------------------------------------------------------------------------- */
static long now_ms(void)
{
    struct timeval tv;
    gettimeofday(&tv, NULL);
    return (long)(tv.tv_sec * 1000L + tv.tv_usec / 1000);
}

static void trace(const char *a, const char *b)
{
    char buf[300];
    snprintf(buf, sizeof buf, "%s %s", a, b ? b : "");
    phc_log(buf);
}

static void post(view_t *v, int kind, int a, int b, unsigned id)
{
    int merged = 0;
    pthread_mutex_lock(&gmu);
    if (kind == PHE_EV_REPAINT || kind == PHE_EV_PROGRESS) {
        int i;
        for (i = evh; i != evt; i = (i + 1) % EVQ)
            if (evq[i].kind == kind && evq[i].view == v->id) { evq[i].a = a; evq[i].b = b; merged = 1; break; }
    }
    if (!merged && (evt + 1) % EVQ != evh) {
        evq[evt].kind = kind; evq[evt].view = v->id; evq[evt].a = a; evq[evt].b = b; evq[evt].id = id;
        evt = (evt + 1) % EVQ;
    }
    pthread_mutex_unlock(&gmu);
    if (notify_fn) notify_fn(notify_ctx, v->id);
}

static void u16to8(const unsigned short *s, int len, char *o, size_t cap)
{
    size_t n = 0;
    int i;
    for (i = 0; i < len && n + 5 < cap; i++) {
        unsigned c = s[i];
        if (c >= 0xD800 && c < 0xDC00 && i + 1 < len && s[i + 1] >= 0xDC00 && s[i + 1] < 0xE000) {
            c = 0x10000 + ((c - 0xD800) << 10) + (unsigned)(s[i + 1] - 0xDC00); i++;
        }
        if (c < 0x80) o[n++] = (char)c;
        else if (c < 0x800) { o[n++] = (char)(0xC0 | (c >> 6)); o[n++] = (char)(0x80 | (c & 63)); }
        else if (c < 0x10000) { o[n++] = (char)(0xE0 | (c >> 12)); o[n++] = (char)(0x80 | ((c >> 6) & 63)); o[n++] = (char)(0x80 | (c & 63)); }
        else { o[n++] = (char)(0xF0 | (c >> 18)); o[n++] = (char)(0x80 | ((c >> 12) & 63)); o[n++] = (char)(0x80 | ((c >> 6) & 63)); o[n++] = (char)(0x80 | (c & 63)); }
    }
    o[n] = 0;
}

/* UTF-8 -> UTF-16 into buf (cap in units); returns the full length in units */
static int u8to16(const char *s, unsigned short *buf, int cap)
{
    const unsigned char *p = (const unsigned char *)s;
    int n = 0;
    while (*p) {
        unsigned c = *p++;
        if (c >= 0xF0 && p[0] && p[1] && p[2]) { c = ((c & 7) << 18) | ((unsigned)(p[0] & 63) << 12) | ((unsigned)(p[1] & 63) << 6) | (unsigned)(p[2] & 63); p += 3; }
        else if (c >= 0xE0 && p[0] && p[1]) { c = ((c & 15) << 12) | ((unsigned)(p[0] & 63) << 6) | (unsigned)(p[1] & 63); p += 2; }
        else if (c >= 0xC0 && p[0]) { c = ((c & 31) << 6) | (unsigned)(p[0] & 63); p += 1; }
        if (c >= 0x10000) {
            c -= 0x10000;
            if (buf && n + 1 < cap) { buf[n] = (unsigned short)(0xD800 + (c >> 10)); buf[n + 1] = (unsigned short)(0xDC00 + (c & 1023)); }
            n += 2;
        } else {
            if (buf && n < cap) buf[n] = (unsigned short)c;
            n++;
        }
    }
    return n;
}

static void copy_str(char *dst, size_t cap, const char *src)
{
    strncpy(dst, src ? src : "", cap - 1);
    dst[cap - 1] = 0;
}

/* ---- configuration -------------------------------------------------------------------------------------------------- */
typedef struct { char host[128], port[8], pin[80], code[32], dev[48], key[80], name[48]; } cfg_t;

static int hexval(int c)
{
    if (c >= '0' && c <= '9') return c - '0';
    if (c >= 'a' && c <= 'f') return c - 'a' + 10;
    if (c >= 'A' && c <= 'F') return c - 'A' + 10;
    return -1;
}

static int unhex(const char *s, uint8_t *out, size_t n)
{
    size_t i;
    if (strlen(s) != n * 2) return -1;
    for (i = 0; i < n; i++) {
        int h = hexval(s[2 * i]), l = hexval(s[2 * i + 1]);
        if (h < 0 || l < 0) return -1;
        out[i] = (uint8_t)(h * 16 + l);
    }
    return 0;
}

static void tohex(const uint8_t *b, size_t n, char *out)
{
    static const char d[] = "0123456789abcdef";
    size_t i;
    for (i = 0; i < n; i++) { out[2 * i] = d[b[i] >> 4]; out[2 * i + 1] = d[b[i] & 15]; }
    out[2 * n] = 0;
}

static int cfg_read(cfg_t *c)
{
    FILE *f = fopen(INI_PATH, "r");
    char line[256];
    memset(c, 0, sizeof *c);
    copy_str(c->port, sizeof c->port, "7400");
    copy_str(c->name, sizeof c->name, "Phorepoc");
    if (!f) return -1;
    while (fgets(line, sizeof line, f)) {
        char *eq = strchr(line, '='), *e;
        if (!eq) continue;
        *eq++ = 0;
        for (e = eq + strlen(eq); e > eq && (e[-1] == '\n' || e[-1] == '\r' || e[-1] == ' '); ) *--e = 0;
        if (!strcmp(line, "host")) copy_str(c->host, sizeof c->host, eq);
        else if (!strcmp(line, "port")) copy_str(c->port, sizeof c->port, eq);
        else if (!strcmp(line, "pin")) copy_str(c->pin, sizeof c->pin, eq);
        else if (!strcmp(line, "code")) copy_str(c->code, sizeof c->code, eq);
        else if (!strcmp(line, "dev")) copy_str(c->dev, sizeof c->dev, eq);
        else if (!strcmp(line, "key")) copy_str(c->key, sizeof c->key, eq);
        else if (!strcmp(line, "name")) copy_str(c->name, sizeof c->name, eq);
    }
    fclose(f);
    return c->host[0] ? 0 : -1;
}

static void cfg_write(const cfg_t *c)
{
    FILE *f = fopen(INI_PATH, "w");
    if (!f) return;
    fprintf(f, "host=%s\nport=%s\npin=%s\nname=%s\ndev=%s\nkey=%s\n", c->host, c->port, c->pin, c->name, c->dev, c->key);
    fclose(f);
}

/* ---- decoders (libpng, libjpeg) ------------------------------------------------------------------------------------- */
typedef struct { const uint8_t *p; size_t n, off; } membuf;

static void png_mem_read(png_structp png, png_bytep out, png_size_t n)
{
    membuf *m = (membuf *)png_get_io_ptr(png);
    if (m->off + n > m->n) png_error(png, "short");
    memcpy(out, m->p + m->off, n);
    m->off += n;
}

/* decode into a new w*h BGRA buffer (w, h = the tile size announced by the server); NULL on failure */
static void png_err_cb(png_structp png, png_const_charp msg)
{
    phc_log(msg);
    longjmp(png_jmpbuf(png), 1);
}
static void png_warn_cb(png_structp png, png_const_charp msg) { (void)png; (void)msg; }

static uint32_t *decode_png(const uint8_t *data, size_t len, int w, int h)
{
    png_structp png = png_create_read_struct(PNG_LIBPNG_VER_STRING, NULL, png_err_cb, png_warn_cb);
    png_infop info;
    membuf m;
    uint32_t *volatile out = NULL;
    png_bytep *volatile rows = NULL;
    int y;
    if (!png) return NULL;
    info = png_create_info_struct(png);
    if (!info) { png_destroy_read_struct(&png, NULL, NULL); return NULL; }
    if (setjmp(png_jmpbuf(png))) {
        png_destroy_read_struct(&png, &info, NULL);
        free(rows); free(out);
        return NULL;
    }
    m.p = data; m.n = len; m.off = 0;
    png_set_read_fn(png, &m, png_mem_read);
    png_read_info(png, info);
    if ((int)png_get_image_width(png, info) > w) png_error(png, "too wide");   /* rows would overrun */
    png_set_expand(png);
    png_set_strip_16(png);
    png_set_gray_to_rgb(png);
    png_set_bgr(png);
    png_set_filler(png, 0xFF, PNG_FILLER_AFTER);
    png_read_update_info(png, info);
    out = (uint32_t *)calloc((size_t)w * (size_t)h, 4);
    rows = (png_bytep *)malloc(sizeof(png_bytep) * (size_t)png_get_image_height(png, info));
    if (!out || !rows) png_error(png, "oom");
    for (y = 0; y < (int)png_get_image_height(png, info); y++) rows[y] = (png_bytep)(out + (size_t)(y < h ? y : h - 1) * (size_t)w);
    png_read_image(png, rows);
    png_destroy_read_struct(&png, &info, NULL);
    free(rows);
    return out;
}

typedef struct { struct jpeg_error_mgr pub; jmp_buf jb; } jerr_t;
static void jerr_exit(j_common_ptr cinfo) { longjmp(((jerr_t *)cinfo->err)->jb, 1); }
static void jsrc_init(j_decompress_ptr c) { (void)c; }
static boolean jsrc_fill(j_decompress_ptr c) { (void)c; return FALSE; }
static void jsrc_skip(j_decompress_ptr c, long n)
{
    if (n > 0) {
        if ((size_t)n > c->src->bytes_in_buffer) n = (long)c->src->bytes_in_buffer;
        c->src->next_input_byte += n; c->src->bytes_in_buffer -= (size_t)n;
    }
}
static void jsrc_term(j_decompress_ptr c) { (void)c; }

static uint32_t *decode_jpeg(const uint8_t *data, size_t len, int w, int h)
{
    struct jpeg_decompress_struct ci;
    struct jpeg_source_mgr src;
    jerr_t je;
    uint32_t *volatile out = NULL;
    uint8_t *volatile row = NULL;
    memset(&ci, 0, sizeof ci);
    ci.err = jpeg_std_error(&je.pub);
    je.pub.error_exit = jerr_exit;
    if (setjmp(je.jb)) { jpeg_destroy_decompress(&ci); free(row); free(out); return NULL; }
    jpeg_create_decompress(&ci);
    src.next_input_byte = data; src.bytes_in_buffer = len;
    src.init_source = jsrc_init; src.fill_input_buffer = jsrc_fill; src.skip_input_data = jsrc_skip;
    src.resync_to_restart = jpeg_resync_to_restart; src.term_source = jsrc_term;
    ci.src = &src;
    jpeg_read_header(&ci, TRUE);
    ci.out_color_space = JCS_RGB;
    jpeg_start_decompress(&ci);
    if ((int)ci.output_width > w || ci.output_components != 3) longjmp(je.jb, 1);
    out = (uint32_t *)calloc((size_t)w * (size_t)h, 4);
    row = (uint8_t *)malloc((size_t)ci.output_width * 3);
    if (!out || !row) longjmp(je.jb, 1);
    while (ci.output_scanline < ci.output_height) {
        JSAMPROW rp = row;
        unsigned y = ci.output_scanline;
        unsigned x;
        jpeg_read_scanlines(&ci, &rp, 1);
        if ((int)y >= h) continue;
        for (x = 0; x < ci.output_width; x++)
            out[(size_t)y * (size_t)w + x] = 0xFF000000u | ((uint32_t)row[3 * x] << 16) | ((uint32_t)row[3 * x + 1] << 8) | row[3 * x + 2];
    }
    jpeg_finish_decompress(&ci);
    jpeg_destroy_decompress(&ci);
    free(row);
    return out;
}

/* ---- tile store ----------------------------------------------------------------------------------------------------- */
static void tile_free(tile_t *t)
{
    free(t->enc); free(t->dec);
    memset(t, 0, sizeof *t);
}

static void tiles_clear(view_t *v)     /* pixmu held */
{
    int i;
    for (i = 0; i < v->ntiles; i++) if (v->tiles[i].used) tile_free(&v->tiles[i]);
    v->ntiles = 0;
}

static void tiles_drop_decoded(view_t *v)   /* pixmu held */
{
    int i;
    for (i = 0; i < v->ntiles; i++) { free(v->tiles[i].dec); v->tiles[i].dec = NULL; }
}

static tile_t *tile_find(view_t *v, int layer, int x, int y)
{
    int i;
    for (i = 0; i < v->ntiles; i++) {
        tile_t *t = &v->tiles[i];
        if (t->used && t->layer == layer && t->x == x && t->y == y) return t;
    }
    return NULL;
}

static void on_tile(void *user, const phc_tile *pt)
{
    view_t *v = (view_t *)user;
    tile_t *t;
    uint8_t *copy = (uint8_t *)malloc(pt->len ? pt->len : 1);
    int i;
    if (!copy) return;
    memcpy(copy, pt->data, pt->len);
    PHC_TRACE({ char m[96]; snprintf(m, sizeof m, "tile layer=%d at %d,%d %dx%d fmt=%d len=%d", pt->layer, pt->x, pt->y, pt->w, pt->h, pt->fmt, (int)pt->len); phc_log(m); })
    pthread_mutex_lock(&v->pixmu);
    t = tile_find(v, pt->layer, pt->x, pt->y);
    if (!t) {
        for (i = 0; i < v->ntiles && v->tiles[i].used; i++) {}
        if (i == v->ntiles) {
            if (v->ntiles >= PHC_MAX_TILES) { pthread_mutex_unlock(&v->pixmu); free(copy); return; }
            v->ntiles++;
        }
        t = &v->tiles[i];
        memset(t, 0, sizeof *t);
        t->used = 1;
    } else {
        free(t->enc); free(t->dec); t->dec = NULL;
    }
    t->layer = pt->layer; t->x = pt->x; t->y = pt->y; t->w = pt->w; t->h = pt->h; t->fmt = pt->fmt;
    t->enc = copy; t->enclen = pt->len;
    pthread_mutex_lock(&v->mu); v->dirty = 1; pthread_mutex_unlock(&v->mu);
    pthread_mutex_unlock(&v->pixmu);
}

/* ---- composition ---------------------------------------------------------------------------------------------------- */
static uint32_t *tile_pixels(tile_t *t, unsigned stamp)    /* pixmu held */
{
    if (!t->dec) {
        if (t->w <= 0 || t->h <= 0 || t->w > 2048 || t->h > 2048) return NULL;
        t->dec = t->fmt == PHC_FMT_JPEG ? decode_jpeg(t->enc, t->enclen, t->w, t->h) : decode_png(t->enc, t->enclen, t->w, t->h);
        if (!t->dec || PHC_TRACE_ON) { char m[96]; snprintf(m, sizeof m, "decode %s fmt=%d %dx%d len=%d", t->dec ? "ok" : "FAILED", t->fmt, t->w, t->h, (int)t->enclen); phc_log(m); }
    }
    t->stamp = stamp;
    return t->dec;
}

static void blit(view_t *v, const uint32_t *src, int sw, int sh, int dx, int dy, int c0x, int c0y, int c1x, int c1y)
{
    int x0 = dx, y0 = dy, x1 = dx + sw, y1 = dy + sh, y;
    if (x0 < c0x) x0 = c0x;
    if (y0 < c0y) y0 = c0y;
    if (x1 > c1x) x1 = c1x;
    if (y1 > c1y) y1 = c1y;
    if (x1 > v->w) x1 = v->w;
    if (y1 > v->h) y1 = v->h;
    if (x0 < 0) x0 = 0;
    if (y0 < 0) y0 = 0;
    for (y = y0; y < y1; y++)
        memcpy(v->px + (size_t)y * (size_t)v->w + x0, src + (size_t)(y - dy) * (size_t)sw + (x0 - dx), (size_t)(x1 - x0) * 4);
}

static void draw_layer_tiles(view_t *v, int layer, int ox, int oy, int c0x, int c0y, int c1x, int c1y)
{
    int i;
    for (i = 0; i < v->ntiles; i++) {
        tile_t *t = &v->tiles[i];
        uint32_t *p;
        if (!t->used || t->layer != layer) continue;
        if (ox + t->x >= c1x || ox + t->x + t->w <= c0x || oy + t->y >= c1y || oy + t->y + t->h <= c0y) continue;
        p = tile_pixels(t, v->stamp);
        if (p) blit(v, p, t->w, t->h, ox + t->x, oy + t->y, c0x, c0y, c1x, c1y);
    }
}

static void tint(view_t *v, int x, int y, int w, int h)      /* tap feedback: a translucent blue box */
{
    int xx, yy;
    if (x < 0) { w += x; x = 0; }
    if (y < 0) { h += y; y = 0; }
    if (x + w > v->w) w = v->w - x;
    if (y + h > v->h) h = v->h - y;
    for (yy = y; yy < y + h; yy++)
        for (xx = x; xx < x + w; xx++) {
            uint32_t *q = &v->px[(size_t)yy * (size_t)v->w + xx];
            uint32_t p = *q;
            uint32_t r = (((p >> 16) & 255) * 3 + 0x30) / 4, g = (((p >> 8) & 255) * 3 + 0x60) / 4, b = ((p & 255) * 3 + 0xFF) / 4;
            *q = 0xFF000000u | (r << 16) | (g << 8) | b;
        }
}

/* thread-owned phc state is read here (layers); called only from the view's thread */
static void compose(view_t *v)
{
    phc *c = v->c;
    int sy, ox, i, n, order[PHC_MAX_LAYERS];
    pthread_mutex_lock(&v->mu);
    sy = v->scroll_y;
    v->dirty = 0;
    pthread_mutex_unlock(&v->mu);
    pthread_mutex_lock(&v->pixmu);
    if (!v->px) { pthread_mutex_unlock(&v->pixmu); return; }
    v->stamp++;
    PHC_TRACE({ char m[96]; snprintf(m, sizeof m, "compose sy=%d tiles=%d layers=%d view=%dx%d", sy, v->ntiles, c->nlayers, v->w, v->h); phc_log(m); })
    for (i = 0; i < v->w * v->h; i++) v->px[i] = 0xFFFFFFFFu;
    ox = v->w > PAGE_W ? (v->w - PAGE_W) / 2 : 0;
    draw_layer_tiles(v, 0, ox, -sy, 0, 0, v->w, v->h);
    /* layers above the page, in (z, id) order */
    n = 0;
    for (i = 0; i < c->nlayers && n < PHC_MAX_LAYERS; i++) {
        int j = n++;
        while (j > 0 && (c->layers[order[j - 1]].z > c->layers[i].z)) { order[j] = order[j - 1]; j--; }
        order[j] = i;
    }
    for (i = 0; i < n; i++) {
        const phc_layer *l = &c->layers[order[i]];
        if (l->kind == PHC_LAYER_FIXED)
            draw_layer_tiles(v, l->id, ox + l->x, l->y, ox + l->x, l->y, ox + l->x + l->w, l->y + l->h);
        else if (l->kind == PHC_LAYER_STICKY) {
            int yy = phc_layer_screen_y(l, sy, v->h);
            draw_layer_tiles(v, l->id, ox + l->x, yy, ox + l->x, yy, ox + l->x + l->w, yy + l->h);
        } else {   /* an inner scroll box: the visible window onto a larger tile area */
            int by = l->y - sy;
            draw_layer_tiles(v, l->id, ox + l->x - l->scroll_x, by - l->scroll_y, ox + l->x, by, ox + l->x + l->w, by + l->h);
        }
    }
    pthread_mutex_lock(&v->mu);
    if (v->hl_active) tint(v, ox + v->hl_x, v->hl_y - sy, v->hl_w, v->hl_h);
    pthread_mutex_unlock(&v->mu);
    /* keep a handful of decoded tiles: the ones drawn this pass, then the newest */
    {
        int kept = 0;
        for (i = 0; i < v->ntiles; i++) if (v->tiles[i].used && v->tiles[i].dec) kept++;
        for (i = 0; i < v->ntiles && kept > MAXDEC; i++) {
            tile_t *t = &v->tiles[i];
            if (t->used && t->dec && t->stamp != v->stamp) { free(t->dec); t->dec = NULL; kept--; }
        }
    }
    pthread_mutex_unlock(&v->pixmu);
    post(v, PHE_EV_REPAINT, 0, 0, 0);
}

/* ---- phc callbacks (view thread) ------------------------------------------------------------------------------------ */
static void on_page(void *user, const phc_page *p)
{
    view_t *v = (view_t *)user;
    int changed;
    if (p->pid != v->pid || p->reset) {   /* a new page, or the same page at another scale: phc dropped its hashes too (a progressive load repeats the page frame: keep the tiles) */
        pthread_mutex_lock(&v->pixmu);
        tiles_clear(v);
        pthread_mutex_unlock(&v->pixmu);
        v->pid = p->pid;
    }
    pthread_mutex_lock(&v->mu);
    changed = strcmp(v->url, p->url) != 0;
    copy_str(v->url, sizeof v->url, p->url);
    copy_str(v->title, sizeof v->title, p->title);
    v->page_h = p->h; v->page_trunc = p->trunc; v->can_back = p->back; v->can_fwd = p->fwd;
    v->sec = strncmp(p->url, "https:", 6) == 0 ? PHE_SEC_SECURE : PHE_SEC_NONE;
    if (changed) v->scroll_y = 0;
    if (v->scroll_y > p->h) v->scroll_y = p->h > v->h ? p->h - v->h : 0;
    v->dirty = 1;
    pthread_mutex_unlock(&v->mu);
    post(v, PHE_EV_COMMIT, 0, v->sec, 0);
    post(v, PHE_EV_TITLE, 0, 0, 0);
    post(v, PHE_EV_HISTORY, 0, 0, 0);
}

static void on_focus(void *user, const phc_focus *f)
{
    view_t *v = (view_t *)user;
    int kind = f->kind == PHC_FOCUS_NONE ? 0 : 2;
    int changed;
    pthread_mutex_lock(&v->mu);
    changed = (v->focus != kind);
    v->focus = kind;
    pthread_mutex_unlock(&v->mu);
    if (changed) post(v, PHE_EV_FOCUS, kind, 0, 0);
}

static void on_dialog(void *user, const phc_dialog *d)
{
    view_t *v = (view_t *)user;
    int kind = PHE_DLG_ALERT;
    switch (d->kind) {
    case PHC_DLG_CONFIRM: case PHC_DLG_BEFOREUNLOAD: kind = PHE_DLG_CONFIRM; break;
    case PHC_DLG_PROMPT: kind = PHE_DLG_PROMPT; break;
    case PHC_DLG_AUTH: kind = PHE_DLG_AUTH; break;
    case PHC_DLG_CERT: kind = PHE_DLG_CERT; break;
    default: break;
    }
    pthread_mutex_lock(&v->mu);
    v->dlg_id = d->id; v->dlg_kind = d->kind;
    copy_str(v->dlg_text, sizeof v->dlg_text, d->message);
    copy_str(v->dlg_def, sizeof v->dlg_def, d->def);
    pthread_mutex_unlock(&v->mu);
    post(v, PHE_EV_DIALOG, kind, 0, (unsigned)d->id);
}

static void on_download(void *user, const phc_download *d)
{
    view_t *v = (view_t *)user;
    pthread_mutex_lock(&v->mu);
    v->dlg_id = 0; v->dlg_kind = PHC_DLG_ALERT;
    snprintf(v->dlg_text, sizeof v->dlg_text, "The cloud browser does not download files (%s).", d->name);
    v->dlg_def[0] = 0;
    pthread_mutex_unlock(&v->mu);
    post(v, PHE_EV_DIALOG, PHE_DLG_ALERT, 0, 0);
}

/* ---- network -------------------------------------------------------------------------------------------------------- */
typedef struct { int fd; } rawsock;

static int raw_send(void *ctx, const void *p, size_t n)
{
    rawsock *s = (rawsock *)ctx;
    int r = (int)send(s->fd, p, n, 0);
    if (r >= 0) return r;
    return (errno == EAGAIN || errno == EWOULDBLOCK || errno == EINTR) ? 0 : -1;
}

static int raw_recv(void *ctx, void *p, size_t n)
{
    rawsock *s = (rawsock *)ctx;
    int r = (int)recv(s->fd, p, n, 0);
    if (r > 0) return r;
    if (r == 0) return -1;      /* end of stream */
    return (errno == EAGAIN || errno == EWOULDBLOCK || errno == EINTR) ? 0 : -1;
}

static void wait_fd(int fd, int want_write, int ms)
{
    fd_set rs, ws;
    struct timeval tv;
    FD_ZERO(&rs); FD_ZERO(&ws);
    if (want_write) FD_SET(fd, &ws); else FD_SET(fd, &rs);
    tv.tv_sec = ms / 1000; tv.tv_usec = (ms % 1000) * 1000;
    select(fd + 1, &rs, &ws, NULL, &tv);
}

static rawsock rs_store[MAXV];
static void raw_wait(void *ctx, int want_write) { wait_fd(((rawsock *)ctx)->fd, want_write, 1000); }

static int net_connect(const char *host, const char *port)
{
    struct addrinfo hints, *res, *a;
    int fd = -1;
    memset(&hints, 0, sizeof hints);
    hints.ai_family = AF_UNSPEC; hints.ai_socktype = SOCK_STREAM;
    if (getaddrinfo(host, port, &hints, &res) != 0) return -1;
    for (a = res; a; a = a->ai_next) {
        fd = socket(a->ai_family, a->ai_socktype, a->ai_protocol);
        if (fd < 0) continue;
        if (connect(fd, a->ai_addr, a->ai_addrlen) == 0) break;
        close(fd); fd = -1;
    }
    freeaddrinfo(res);
    if (fd >= 0) fcntl(fd, F_SETFL, fcntl(fd, F_GETFL, 0) | O_NONBLOCK);
    return fd;
}

static void teardown(view_t *v)
{
    if (v->c) {
        phc_tls_close(&v->tls);
        free(v->c); v->c = NULL;
    }
    free(v->rx); v->rx = NULL;
    if (v->sock >= 0) { close(v->sock); v->sock = -1; }
}

static void fail(view_t *v, const char *msg, int code)
{
    pthread_mutex_lock(&v->mu);
    copy_str(v->err, sizeof v->err, msg);
    v->loading = 0;
    pthread_mutex_unlock(&v->mu);
    post(v, PHE_EV_FAILED, code, 0, 0);
    trace("fail", msg);
}

static int session_open(view_t *v, const char *url)
{
    cfg_t cfg;
    uint8_t pin[32];
    int rc, vi = (int)(v - views);
    if (cfg_read(&cfg) != 0) { fail(v, "No server set: put host=, pin= and code= in C:\\data\\phecloud.ini", -1); return -1; }
    v->sock = net_connect(cfg.host, cfg.port);
    if (v->sock < 0) { fail(v, "Cannot reach the phecloud server", -2); return -1; }
    rs_store[vi].fd = v->sock;
    v->c = (phc *)calloc(1, sizeof(phc));
    v->rx = (uint8_t *)malloc(RXCAP);
    if (!v->c || !v->rx) { fail(v, "Out of memory", -3); teardown(v); return -1; }
    {
        phc_raw_io raw;
        raw.ctx = &rs_store[vi]; raw.send = raw_send; raw.recv = raw_recv; raw.wait = raw_wait;
        if (phc_tls_init(&v->tls, raw, cfg.pin[0] && unhex(cfg.pin, pin, 32) == 0 ? pin : NULL) != 0) {
            fail(v, "TLS setup failed", -4); free(v->c); v->c = NULL; free(v->rx); v->rx = NULL; close(v->sock); v->sock = -1; return -1;
        }
    }
    rc = phc_tls_handshake(&v->tls);
    if (rc != PHC_OK) {
        fail(v, rc == PHC_ERR_AUTH ? "The server certificate does not match the pinned fingerprint" : "TLS handshake failed", rc);
        teardown(v);
        return -1;
    }
    if (!cfg.pin[0]) {          /* trust on first use: remember the fingerprint (only reached when no pin was configured) */
        uint8_t seen[32];
        phc_tls_peer_fingerprint(&v->tls, seen);
        tohex(seen, 32, cfg.pin);
        cfg_write(&cfg);
    }
    phc_init(v->c, phc_tls_transport(&v->tls, 0), v->rx, RXCAP);
    {
        phc_events ev;
        memset(&ev, 0, sizeof ev);
        ev.on_page = on_page; ev.on_tile = on_tile; ev.on_focus = on_focus; ev.on_dialog = on_dialog; ev.on_download = on_download;
        ev.user = v;
        phc_set_events(v->c, ev);
    }
    memset(&v->cr, 0, sizeof v->cr);
    if (cfg.dev[0] && cfg.key[0] && unhex(cfg.key, v->cr.key, 32) == 0) { copy_str(v->cr.dev, sizeof v->cr.dev, cfg.dev); v->cr.have = 1; }
    if (v->cr.have) rc = phc_auth(v->c, &v->cr, NULL, cfg.name);
    else if (cfg.code[0]) rc = phc_auth(v->c, &v->cr, cfg.code, cfg.name);
    else { fail(v, "Not paired: add code=<pairing code> to C:\\data\\phecloud.ini", -5); teardown(v); return -1; }
    if (rc != PHC_OK) { fail(v, v->c->errmsg[0] ? v->c->errmsg : "Pairing or authentication failed", rc); teardown(v); return -1; }
    if (!cfg.dev[0] || strcmp(cfg.dev, v->cr.dev) != 0) {     /* paired just now: keep the credentials, drop the one-time code */
        copy_str(cfg.dev, sizeof cfg.dev, v->cr.dev);
        tohex(v->cr.key, 32, cfg.key);
        cfg.code[0] = 0;
        cfg_write(&cfg);
    }
    phc_mem("after pairing");
    v->c->io = phc_tls_transport(&v->tls, 1);     /* from here on recv returns PHC_WOULDBLOCK between frames */
    rc = phc_open(v->c, url, PAGE_W);
    if (rc != PHC_OK) { fail(v, "Cannot open the page", rc); teardown(v); return -1; }
    return 0;
}

/* ---- the view thread ------------------------------------------------------------------------------------------------ */
static int rq_pop(view_t *v, req_t *out)
{
    int ok = 0;
    pthread_mutex_lock(&v->mu);
    if (v->rqh != v->rqt) { *out = v->rq[v->rqh]; v->rqh = (v->rqh + 1) % NREQ; ok = 1; }
    pthread_mutex_unlock(&v->mu);
    return ok;
}

static void rq_push_ok(view_t *v, int kind, int a, int b, int ok, const char *s, const char *s2)
{
    pthread_mutex_lock(&v->mu);
    if ((v->rqt + 1) % NREQ != v->rqh) {
        req_t *r = &v->rq[v->rqt];
        memset(r, 0, sizeof *r);
        r->kind = kind; r->a = a; r->b = b; r->ok = ok;
        if (s) copy_str(r->s, sizeof r->s, s);
        if (s2) copy_str(r->s2, sizeof r->s2, s2);
        v->rqt = (v->rqt + 1) % NREQ;
    }
    pthread_mutex_unlock(&v->mu);
}

static void rq_push(view_t *v, int kind, int a, int b, const char *s, const char *s2)
{
    rq_push_ok(v, kind, a, b, 0, s, s2);
}

static void do_request(view_t *v, const req_t *r)
{
    phc *c = v->c;
    int sy, vh;
    pthread_mutex_lock(&v->mu); sy = v->scroll_y; vh = v->h; pthread_mutex_unlock(&v->mu);
    switch (r->kind) {
    case RQ_LOAD:
        if (!c) { if (session_open(v, r->s) != 0) return; }
        else phc_navigate(c, r->s);
        pthread_mutex_lock(&v->mu); v->loading = 1; v->progress = 10; v->err[0] = 0; pthread_mutex_unlock(&v->mu);
        post(v, PHE_EV_START, 0, 0, 0);
        post(v, PHE_EV_PROGRESS, 10, 0, 0);
        break;
    case RQ_ACTION:
        if (c) { phc_navigate_action(c, r->s); pthread_mutex_lock(&v->mu); v->loading = 1; pthread_mutex_unlock(&v->mu); post(v, PHE_EV_START, 0, 0, 0); }
        break;
    case RQ_DOWN:
        if (c) {
            phc_hit h;
            phc_hit_test(c, sy, vh, r->a - (v->w > PAGE_W ? (v->w - PAGE_W) / 2 : 0), r->b, &h);
            pthread_mutex_lock(&v->mu);
            v->focus_url[0] = 0;
            v->hl_active = 0;
            if (h.region) {
                copy_str(v->focus_url, sizeof v->focus_url, h.region->href);
                if (h.layer == 0) { v->hl_active = 1; v->hl_x = h.region->x; v->hl_y = h.region->y; v->hl_w = h.region->w; v->hl_h = h.region->h; }
            }
            v->dirty = 1;
            pthread_mutex_unlock(&v->mu);
            post(v, PHE_EV_FOCUS, h.region && h.region->href[0] ? 1 : 0, 0, 0);
        }
        break;
    case RQ_TAP:
        if (c) {
            phc_hit h;
            phc_tap_screen(c, sy, vh, r->a - (v->w > PAGE_W ? (v->w - PAGE_W) / 2 : 0), r->b, &h);
            pthread_mutex_lock(&v->mu); v->hl_active = 0; v->dirty = 1; v->loading = 1; v->progress = 10; pthread_mutex_unlock(&v->mu);
            post(v, PHE_EV_PROGRESS, 10, 0, 0);
        }
        break;
    case RQ_CANCEL:
        pthread_mutex_lock(&v->mu); v->hl_active = 0; v->dirty = 1; pthread_mutex_unlock(&v->mu);
        break;
    case RQ_TEXT: if (c) phc_text(c, 0, r->s, 0); break;
    case RQ_KEY: if (c) phc_key(c, r->s); break;
    case RQ_SCROLL: if (c) phc_scroll(c, r->a); break;
    case RQ_DIALOG:
        if (c) {
            if (r->b == PHC_DLG_AUTH) phc_dialog_reply_auth(c, r->a, r->ok, r->s, r->s2);
            else phc_dialog_reply(c, r->a, r->ok, r->s);
        }
        break;
    default: break;
    }
}

static void *view_main(void *arg)
{
    view_t *v = (view_t *)arg;
    req_t r;
    long last_idle = 0;
    phc_mem("thread start");
    while (!v->quit) {
        int rc;
        while (rq_pop(v, &r)) {
            if (r.kind == RQ_STOP) { v->quit = 1; break; }
            if (r.kind == RQ_SUSPEND) {
                teardown(v);
                pthread_mutex_lock(&v->pixmu); tiles_clear(v); pthread_mutex_unlock(&v->pixmu);
                continue;
            }
            do_request(v, &r);
        }
        if (v->quit) break;
        if (!v->c) { usleep(30000); continue; }
        rc = phc_pump(v->c);
        if (rc == PHC_OK) {
            int more = v->c->done.more;
            pthread_mutex_lock(&v->mu);
            v->loading = more ? 1 : 0;
            v->progress = more ? 60 : 100;
            pthread_mutex_unlock(&v->mu);
            compose(v);
            phc_mem(more ? "first screen" : "page done");
            post(v, PHE_EV_PROGRESS, more ? 60 : 100, 0, 0);
            if (!more) post(v, PHE_EV_FINISHED, 0, 0, 0);
            continue;
        }
        if (rc == PHC_WOULDBLOCK) {
            int dirty, unsent, y;
            long t = now_ms();
            pthread_mutex_lock(&v->mu); dirty = v->dirty; unsent = v->scroll_unsent; y = v->scroll_y; pthread_mutex_unlock(&v->mu);
            if (dirty) compose(v);
            if (unsent && t - v->scroll_sent_ms > 300) {       /* scrolling stopped: let the server load lazy content */
                phc_scroll(v->c, y);
                pthread_mutex_lock(&v->mu); v->scroll_unsent = 0; pthread_mutex_unlock(&v->mu);
            }
            if (t - last_idle > 25000) { phc_ping(v->c); last_idle = t; }
            wait_fd(v->sock, 0, 20);
            continue;
        }
        fail(v, rc == PHC_ERR_SERVER && v->c->errmsg[0] ? v->c->errmsg : "The connection to the server was lost", rc);
        teardown(v);
        post(v, PHE_EV_CRASHED, 0, 0, 0);
    }
    teardown(v);
    return NULL;
}

/* ---- the seam ------------------------------------------------------------------------------------------------------- */
static view_t *vget(PheView id)
{
    int i;
    for (i = 0; i < MAXV; i++) if (views[i].used && views[i].id == id) return &views[i];
    return NULL;
}

static int e_init(PheNotify notify, void *ctx)
{
    int i;
    notify_fn = notify; notify_ctx = ctx;
    for (i = 0; i < MAXV; i++) { memset(&views[i], 0, sizeof views[i]); views[i].sock = -1; }
    evh = evt = 0;
    return 0;
}

static void view_stop(view_t *v)
{
    if (v->thr_on) {
        v->quit = 1;
        pthread_join(v->thr, NULL);
        v->thr_on = 0;
    }
}

static void e_shutdown(void)
{
    int i;
    for (i = 0; i < MAXV; i++) if (views[i].used) {
        view_stop(&views[i]);
        tiles_clear(&views[i]);
        free(views[i].tiles); free(views[i].px);
        views[i].used = 0;
    }
}

static void e_set_useragent(const unsigned short *s, int len) { (void)s; (void)len; (void)useragent_unused; }   /* the server's Chromium decides */
static void e_set_iap(const char *name) { (void)name; }

static void e_memory_pressure(int level)
{
    int i;
    for (i = 0; i < MAXV; i++) {
        view_t *v = &views[i];
        if (!v->used) continue;
        if (level >= PHE_MEM_LOW) { pthread_mutex_lock(&v->pixmu); tiles_drop_decoded(v); pthread_mutex_unlock(&v->pixmu); }
        if (level >= PHE_MEM_CRITICAL && !v->visible && v->thr_on) {
            v->discarded = 1;
            rq_push(v, RQ_SUSPEND, 0, 0, NULL, NULL);
            post(v, PHE_EV_DISCARDED, 0, 0, 0);
        }
    }
}

static PheView e_new_view_id(void) { return nextid++; }

static void e_view_create(PheView id, int w, int h, int is_private, const unsigned char *state, int state_len)
{
    int i;
    view_t *v = NULL;
    (void)is_private;
    for (i = 0; i < MAXV; i++) if (!views[i].used) { v = &views[i]; break; }
    if (!v) return;
    memset(v, 0, sizeof *v);
    v->used = 1; v->id = id; v->w = w > 0 ? w : PAGE_W; v->h = h > 0 ? h : 640; v->visible = 1; v->sock = -1;
    pthread_mutex_init(&v->mu, NULL); pthread_mutex_init(&v->pixmu, NULL);
    v->px = (uint32_t *)calloc((size_t)v->w * (size_t)v->h, 4);
    v->tiles = (tile_t *)calloc(PHC_MAX_TILES, sizeof(tile_t));
    phc_mem("view_create (UI thread: view buffer + tile table)");
    if (state && state_len > 5 && !memcmp(state, "PHC1\n", 5)) {       /* "PHC1\n<scroll_y>\n<url>" */
        char buf[PHC_URL_MAX + 24];
        char *nl;
        size_t n = (size_t)state_len - 5 < sizeof buf - 1 ? (size_t)state_len - 5 : sizeof buf - 1;
        memcpy(buf, state + 5, n); buf[n] = 0;
        nl = strchr(buf, '\n');
        if (nl) { *nl = 0; v->scroll_y = atoi(buf); copy_str(v->url, sizeof v->url, nl + 1); }
    }
    {
        pthread_attr_t at;
        pthread_attr_init(&at);
        pthread_attr_setstacksize(&at, 128 * 1024);       /* libpng/libjpeg and the mbedTLS handshake are the deepest users */
        if (pthread_create(&v->thr, &at, view_main, v) == 0) v->thr_on = 1;
        pthread_attr_destroy(&at);
    }
}

static void e_view_destroy(PheView id)
{
    view_t *v = vget(id);
    if (!v) return;
    view_stop(v);
    pthread_mutex_lock(&v->pixmu);
    tiles_clear(v);
    free(v->tiles); v->tiles = NULL;
    free(v->px); v->px = NULL;
    pthread_mutex_unlock(&v->pixmu);
    pthread_mutex_destroy(&v->mu); pthread_mutex_destroy(&v->pixmu);
    v->used = 0;
}

static void e_view_resize(PheView id, int w, int h)
{
    view_t *v = vget(id);
    uint32_t *np;
    if (!v || w <= 0 || h <= 0) return;
    np = (uint32_t *)calloc((size_t)w * (size_t)h, 4);
    if (!np) return;
    pthread_mutex_lock(&v->pixmu);
    free(v->px); v->px = np;
    pthread_mutex_lock(&v->mu); v->w = w; v->h = h; v->dirty = 1; pthread_mutex_unlock(&v->mu);
    pthread_mutex_unlock(&v->pixmu);
}

static void e_view_visible(PheView id, int visible)
{
    view_t *v = vget(id);
    if (!v) return;
    pthread_mutex_lock(&v->mu); v->visible = visible; v->dirty = visible; pthread_mutex_unlock(&v->mu);
    if (visible && v->thr_on && v->url[0] && v->discarded) {   /* back from a discard: reconnect and open the page again (cookies come back from the server's per-device jar) */
        v->discarded = 0;
        rq_push(v, RQ_LOAD, 0, 0, v->url, NULL);
    }
}

static void e_view_suspend(PheView id)
{
    view_t *v = vget(id);
    if (v) { v->discarded = 1; rq_push(v, RQ_SUSPEND, 0, 0, NULL, NULL); post(v, PHE_EV_DISCARDED, 0, 0, 0); }
}

static int e_view_save_state(PheView id, unsigned char *buf, int cap)
{
    view_t *v = vget(id);
    char tmp[PHC_URL_MAX + 24];
    int n;
    if (!v) return 0;
    pthread_mutex_lock(&v->mu);
    n = snprintf(tmp, sizeof tmp, "PHC1\n%d\n%s", v->scroll_y, v->url);
    pthread_mutex_unlock(&v->mu);
    if (n < 0) return 0;
    if (n >= (int)sizeof tmp) n = (int)sizeof tmp - 1;
    if (buf && cap >= n) memcpy(buf, tmp, (size_t)n);
    return n;
}

static void e_load(PheView id, const unsigned short *url, int len)
{
    view_t *v = vget(id);
    char u[PHC_URL_MAX];
    if (!v) return;
    u16to8(url, len, u, sizeof u);
    rq_push(v, RQ_LOAD, 0, 0, u, NULL);
}

static void e_cmd(PheView id, int cmd)
{
    view_t *v = vget(id);
    if (!v) return;
    switch (cmd) {
    case PHE_CMD_RELOAD: case PHE_CMD_RELOAD_BYPASS_CACHE: rq_push(v, RQ_ACTION, 0, 0, "reload", NULL); break;
    case PHE_CMD_BACK: rq_push(v, RQ_ACTION, 0, 0, "back", NULL); break;
    case PHE_CMD_FORWARD: rq_push(v, RQ_ACTION, 0, 0, "forward", NULL); break;
    case PHE_CMD_ACTIVATE: rq_push(v, RQ_TAP, v->w / 2, v->h / 2, NULL, NULL); break;
    default: break;        /* STOP: the server cannot cancel a render; the next navigation supersedes it */
    }
}

static void e_get_state(PheView id, PheViewState *o)
{
    view_t *v = vget(id);
    memset(o, 0, sizeof *o);
    o->zoom_permille = 1000;
    if (!v) return;
    pthread_mutex_lock(&v->mu);
    o->can_back = v->can_back; o->can_forward = v->can_fwd; o->loading = v->loading; o->progress = v->progress; o->security = v->sec;
    o->scroll_y = v->scroll_y; o->content_w = PAGE_W; o->content_h = v->page_h;
    pthread_mutex_unlock(&v->mu);
}

static int e_get_str(PheView id, int which, unsigned short *buf, int cap)
{
    view_t *v = vget(id);
    char tmp[PHC_URL_MAX];
    if (!v) return 0;
    pthread_mutex_lock(&v->mu);
    switch (which) {
    case PHE_STR_URL: copy_str(tmp, sizeof tmp, v->url); break;
    case PHE_STR_TITLE: copy_str(tmp, sizeof tmp, v->title); break;
    case PHE_STR_ERROR: copy_str(tmp, sizeof tmp, v->err); break;
    case PHE_STR_FOCUS_URL: copy_str(tmp, sizeof tmp, v->focus_url); break;
    case PHE_STR_DLG_TEXT: copy_str(tmp, sizeof tmp, v->dlg_text); break;
    case PHE_STR_DLG_DEFAULT: copy_str(tmp, sizeof tmp, v->dlg_def); break;
    default: tmp[0] = 0; break;
    }
    pthread_mutex_unlock(&v->mu);
    return u8to16(tmp, buf, cap);
}

static int e_poll(PheView id, PheEvent *o)
{
    int i, found = 0;
    pthread_mutex_lock(&gmu);
    for (i = evh; i != evt; i = (i + 1) % EVQ)
        if (id == 0 || evq[i].view == id) {
            *o = evq[i];
            for (; i != evh; i = (i + EVQ - 1) % EVQ) evq[i] = evq[(i + EVQ - 1) % EVQ];   /* close the gap */
            evh = (evh + 1) % EVQ;
            found = 1;
            break;
        }
    pthread_mutex_unlock(&gmu);
    return found;
}

static void e_dialog_reply(PheView id, unsigned did, int ok, const unsigned short *a, int alen, const unsigned short *b, int blen)
{
    view_t *v = vget(id);
    char s[PHC_URL_MAX / 2], s2[64];
    int kind;
    if (!v) return;
    pthread_mutex_lock(&v->mu); kind = v->dlg_kind; pthread_mutex_unlock(&v->mu);
    s[0] = s2[0] = 0;
    if (a) u16to8(a, alen, s, sizeof s);
    if (b) u16to8(b, blen, s2, sizeof s2);
    rq_push_ok(v, RQ_DIALOG, (int)did, kind, ok, s, s2);
}

#define SLOP 10
static void e_scroll(PheView id, int dx, int dy)
{
    view_t *v = vget(id);
    int max, ny;
    (void)dx;
    if (!v) return;
    pthread_mutex_lock(&v->mu);
    max = v->page_h - v->h;
    if (max < 0) max = 0;
    ny = v->scroll_y + dy;
    if (ny < 0) ny = 0;
    if (ny > max) ny = max;
    if (ny != v->scroll_y) { v->scroll_y = ny; v->dirty = 1; v->scroll_unsent = 1; v->scroll_sent_ms = now_ms(); }
    pthread_mutex_unlock(&v->mu);
}

static void e_pointer(PheView id, int type, int x, int y)
{
    view_t *v = vget(id);
    int dy = 0, moved = 0;
    if (!v) return;
    switch (type) {
    case PHE_PTR_DOWN:
        pthread_mutex_lock(&v->mu); v->down = 1; v->down_x = x; v->down_y = y; v->moved = 0; pthread_mutex_unlock(&v->mu);
        rq_push(v, RQ_DOWN, x, y, NULL, NULL);
        break;
    case PHE_PTR_MOVE:
        pthread_mutex_lock(&v->mu);
        if (v->down) {
            if (!v->moved && (abs(y - v->down_y) > SLOP || abs(x - v->down_x) > SLOP)) { v->moved = 1; moved = 1; }
            if (v->moved) { dy = v->down_y - y; v->down_y = y; v->down_x = x; }
        }
        pthread_mutex_unlock(&v->mu);
        if (moved) rq_push(v, RQ_CANCEL, 0, 0, NULL, NULL);
        if (dy) e_scroll(id, 0, dy);
        break;
    case PHE_PTR_UP:
        pthread_mutex_lock(&v->mu); moved = v->moved; v->down = 0; pthread_mutex_unlock(&v->mu);
        if (!moved) rq_push(v, RQ_TAP, x, y, NULL, NULL);
        break;
    default: break;
    }
}

static void e_key(PheView id, int scan, int unicode, int down)
{
    view_t *v = vget(id);
    const char *name = NULL;
    int focused;
    if (!v || !down) return;
    pthread_mutex_lock(&v->mu); focused = v->focus == 2; pthread_mutex_unlock(&v->mu);
    switch (scan) {              /* TStdScanCode */
    case 0x01: name = "Backspace"; break;
    case 0x02: name = "Tab"; break;
    case 0x03: name = "Enter"; break;
    case 0x04: name = "Escape"; break;
    case 0x0e: name = "ArrowLeft"; break;
    case 0x0f: name = "ArrowRight"; break;
    case 0x10: name = "ArrowUp"; break;
    case 0x11: name = "ArrowDown"; break;
    default: break;
    }
    if (!name) { if (unicode == 8) name = "Backspace"; else if (unicode == 13) name = "Enter"; else return; }
    if (focused) { rq_push(v, RQ_KEY, 0, 0, name, NULL); return; }
    if (!strcmp(name, "ArrowUp")) e_scroll(id, 0, -60);
    else if (!strcmp(name, "ArrowDown")) e_scroll(id, 0, 60);
}

static void e_text(PheView id, const unsigned short *s, int len)
{
    view_t *v = vget(id);
    char u[PHC_URL_MAX];
    if (!v) return;
    u16to8(s, len, u, sizeof u);
    rq_push(v, RQ_TEXT, 0, 0, u, NULL);
}

static void e_setting(PheView id, int sid, int value) { (void)id; (void)sid; (void)value; }   /* the server renders: scripts and images are its policy */

static int e_lock_pixels(PheView id, const unsigned int **px, int *w, int *h, int *stride)
{
    view_t *v = vget(id);
    if (!v) return -1;
    pthread_mutex_lock(&v->pixmu);
    if (!v->px) { pthread_mutex_unlock(&v->pixmu); return -1; }
    *px = v->px; *w = v->w; *h = v->h; *stride = v->w * 4;
    return 0;
}

static void e_unlock_pixels(PheView id)
{
    view_t *v = vget(id);
    if (v) pthread_mutex_unlock(&v->pixmu);
}

static int e_snapshot(PheView id, unsigned int *dst, int w, int h, int stride)
{
    view_t *v = vget(id);
    int x, y;
    if (!v || w <= 0 || h <= 0) return -1;
    pthread_mutex_lock(&v->pixmu);
    if (!v->px) { pthread_mutex_unlock(&v->pixmu); return -1; }
    for (y = 0; y < h; y++) {
        unsigned int *d = (unsigned int *)((unsigned char *)dst + (size_t)y * (size_t)stride);
        const uint32_t *s = v->px + (size_t)(y * v->h / h) * (size_t)v->w;
        for (x = 0; x < w; x++) d[x] = s[x * v->w / w];
    }
    pthread_mutex_unlock(&v->pixmu);
    return 0;
}

static void e_clear_data(int bits) { (void)bits; }       /* cookies live in the server's per-device jar: cleared from the phecloud web app */

static const PheWebEngineApi api = {
    sizeof(PheWebEngineApi), PHE_WEBENGINE_VERSION, "Phecloud", "1.0",
    PHE_CAP_SCRIPT | PHE_CAP_TLS13 | PHE_CAP_SNAPSHOT | PHE_CAP_SUSPEND | PHE_CAP_STATE, MAXV,
    e_init, e_shutdown, e_set_useragent, e_set_iap, e_memory_pressure,
    e_new_view_id, e_view_create, e_view_destroy, e_view_resize, e_view_visible, e_view_suspend, e_view_save_state,
    e_load, e_cmd, e_get_state, e_get_str, e_poll, e_dialog_reply,
    e_scroll, NULL /* zoom */, e_pointer, e_key, e_text, e_setting,
    e_lock_pixels, e_unlock_pixels, e_snapshot,
    NULL, NULL, NULL /* find */, NULL, NULL /* downloads */, e_clear_data
};

const PheWebEngineApi *PhePhecloudGetApi(unsigned wanted)
{
    return wanted == PHE_WEBENGINE_VERSION ? &api : NULL;
}
