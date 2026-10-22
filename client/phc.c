/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */
#include "phc.h"
#include "phc_cbor.h"
#include "phc_sha256.h"
#include <string.h>

enum { T_UNKNOWN, T_CHALLENGE, T_WELCOME, T_ERROR, T_PAGE, T_TILE, T_LAYERS, T_REGIONS, T_DONE, T_PONG, T_FOCUS, T_DIALOG, T_DOWNLOAD, T_TABS };

void phc_init(phc *c, phc_transport io, uint8_t *rxbuf, size_t rxcap)
{
    memset(c, 0, sizeof *c);
    c->io = io;
    c->rx = rxbuf;
    c->rx_cap = rxcap;
    c->view = 1;
    c->page.zoom = 100;
}

void phc_set_events(phc *c, phc_events ev) { c->ev = ev; }
void phc_stats_reset(phc *c) { memset(&c->stats, 0, sizeof c->stats); }

/* ---- framing ---- */

static int read_exact(phc *c, uint8_t *p, size_t n, int allow_wouldblock)
{
    size_t got = 0;
    while (got < n) {
        int r = c->io.recv(c->io.ctx, p + got, n - got);
        if (r == PHC_WOULDBLOCK) {
            if (got == 0 && allow_wouldblock) return PHC_WOULDBLOCK;
            if (c->io.wait) c->io.wait(c->io.ctx);
            continue;   /* mid-frame: keep waiting */
        }
        if (r <= 0) return PHC_ERR_IO;
        got += (size_t)r;
    }
    return PHC_OK;
}

static int read_frame(phc *c, size_t *len)
{
    uint8_t h[4];
    uint32_t n;
    int rc = read_exact(c, h, 4, 1);
    if (rc) return rc;
    n = ((uint32_t)h[0] << 24) | ((uint32_t)h[1] << 16) | ((uint32_t)h[2] << 8) | h[3];
    if (n == 0) return PHC_ERR_PROTO;
    if (n > c->rx_cap) return PHC_ERR_BUF;
    rc = read_exact(c, c->rx, n, 0);
    if (rc) return rc;
    *len = n;
    return PHC_OK;
}

/* read_frame that waits through PHC_WOULDBLOCK (used while authenticating, where no other work is pending) */
static int read_frame_wait(phc *c, size_t *len)
{
    int rc;
    while ((rc = read_frame(c, len)) == PHC_WOULDBLOCK)
        if (c->io.wait) c->io.wait(c->io.ctx);
    return rc;
}

static int frame_type(const uint8_t *p, size_t n)
{
    phc_r r;
    uint32_t cnt, i;
    phc_r_init(&r, p, n);
    if (phc_r_map(&r, &cnt)) return T_UNKNOWN;
    for (i = 0; i < cnt; i++) {
        const char *k, *v;
        size_t kl, vl;
        if (phc_r_text(&r, &k, &kl)) return T_UNKNOWN;
        if (phc_text_eq(k, kl, "t")) {
            if (phc_r_text(&r, &v, &vl)) return T_UNKNOWN;
            if (phc_text_eq(v, vl, "challenge")) return T_CHALLENGE;
            if (phc_text_eq(v, vl, "welcome")) return T_WELCOME;
            if (phc_text_eq(v, vl, "error")) return T_ERROR;
            if (phc_text_eq(v, vl, "page")) return T_PAGE;
            if (phc_text_eq(v, vl, "tile")) return T_TILE;
            if (phc_text_eq(v, vl, "layers")) return T_LAYERS;
            if (phc_text_eq(v, vl, "regions")) return T_REGIONS;
            if (phc_text_eq(v, vl, "done")) return T_DONE;
            if (phc_text_eq(v, vl, "pong")) return T_PONG;
            if (phc_text_eq(v, vl, "focus")) return T_FOCUS;
            if (phc_text_eq(v, vl, "dialog")) return T_DIALOG;
            if (phc_text_eq(v, vl, "download")) return T_DOWNLOAD;
            if (phc_text_eq(v, vl, "tabs")) return T_TABS;
            return T_UNKNOWN;
        }
        if (phc_r_skip(&r)) return T_UNKNOWN;
    }
    return T_UNKNOWN;
}

/* ---- sending ---- */

static int send_frame(phc *c, const phc_w *w)
{
    uint32_t n = (uint32_t)w->len;
    if (w->err) return PHC_ERR_BUF;
    /* the 4 length bytes were reserved at tx[0..3] */
    c->tx[0] = (uint8_t)(n >> 24); c->tx[1] = (uint8_t)(n >> 16); c->tx[2] = (uint8_t)(n >> 8); c->tx[3] = (uint8_t)n;
    return c->io.send(c->io.ctx, c->tx, 4 + (size_t)n) < 0 ? PHC_ERR_IO : PHC_OK;
}

static void begin(phc *c, phc_w *w, const char *type, unsigned nkeys)
{
    phc_w_init(w, c->tx + 4, sizeof c->tx - 4);
    phc_w_map(w, nkeys + 2);
    phc_w_kv_text(w, "t", type);
    phc_w_kv_int(w, "v", c->view);
}

int phc_open(phc *c, const char *url, int width)
{
    phc_w w;
    begin(c, &w, "open", 2);
    phc_w_kv_text(&w, "url", url);
    phc_w_kv_uint(&w, "width", (uint64_t)width);
    return send_frame(c, &w);
}

int phc_navigate(phc *c, const char *url)
{
    phc_w w;
    begin(c, &w, "navigate", 1);
    phc_w_kv_text(&w, "url", url);
    return send_frame(c, &w);
}

int phc_navigate_action(phc *c, const char *action)
{
    phc_w w;
    begin(c, &w, "navigate", 1);
    phc_w_kv_text(&w, "action", action);
    return send_frame(c, &w);
}

int phc_scroll(phc *c, int y)
{
    phc_w w;
    begin(c, &w, "input", 2);
    phc_w_kv_text(&w, "kind", "scroll");
    phc_w_kv_int(&w, "y", y);
    return send_frame(c, &w);
}

int phc_text_ex(phc *c, int region_id, const char *text, int submit, int replace)
{
    phc_w w;
    begin(c, &w, "input", 5);
    phc_w_kv_text(&w, "kind", "text");
    phc_w_kv_int(&w, "id", region_id);
    phc_w_kv_text(&w, "text", text);
    phc_w_kv_bool(&w, "submit", submit);
    phc_w_kv_bool(&w, "replace", replace);
    return send_frame(c, &w);
}

int phc_text(phc *c, int region_id, const char *text, int submit) { return phc_text_ex(c, region_id, text, submit, 0); }

int phc_key(phc *c, const char *name)
{
    phc_w w;
    begin(c, &w, "input", 2);
    phc_w_kv_text(&w, "kind", "key");
    phc_w_kv_text(&w, "text", name);
    return send_frame(c, &w);
}

int phc_select(phc *c, int region_id, int option_index)
{
    phc_w w;
    begin(c, &w, "input", 3);
    phc_w_kv_text(&w, "kind", "select");
    phc_w_kv_int(&w, "id", region_id);
    phc_w_kv_int(&w, "index", option_index);
    return send_frame(c, &w);
}

int phc_dialog_reply(phc *c, int dialog_id, int ok, const char *text)
{
    phc_w w;
    begin(c, &w, "dialogreply", 3);
    phc_w_kv_int(&w, "id", dialog_id);
    phc_w_kv_bool(&w, "ok", ok);
    phc_w_kv_text(&w, "text", text ? text : "");
    return send_frame(c, &w);
}

int phc_dialog_reply_auth(phc *c, int dialog_id, int ok, const char *user, const char *pass)
{
    phc_w w;
    begin(c, &w, "dialogreply", 4);
    phc_w_kv_int(&w, "id", dialog_id);
    phc_w_kv_bool(&w, "ok", ok);
    phc_w_kv_text(&w, "text", user ? user : "");
    phc_w_kv_text(&w, "pass", pass ? pass : "");
    return send_frame(c, &w);
}

int phc_settings(phc *c, int jpeg_quality, int gray)
{
    phc_w w;
    begin(c, &w, "settings", 2);
    phc_w_kv_int(&w, "q", jpeg_quality);
    phc_w_kv_bool(&w, "gray", gray);
    return send_frame(c, &w);
}

void phc_layer_scroll(phc *c, int layer_id, int dx, int dy)
{
    int i;
    for (i = 0; i < c->nlayers; i++) {
        phc_layer *l = &c->layers[i];
        int maxx, maxy;
        if (l->id != layer_id || l->kind != PHC_LAYER_SCROLL) continue;
        maxx = l->cw > l->w ? l->cw - l->w : 0;
        maxy = l->ch > l->h ? l->ch - l->h : 0;
        l->scroll_x += dx; l->scroll_y += dy;
        if (l->scroll_x < 0) l->scroll_x = 0;
        if (l->scroll_x > maxx) l->scroll_x = maxx;
        if (l->scroll_y < 0) l->scroll_y = 0;
        if (l->scroll_y > maxy) l->scroll_y = maxy;
    }
}

int phc_ping(phc *c)
{
    phc_w w;
    begin(c, &w, "ping", 0);
    return send_frame(c, &w);
}

static int send_tap(phc *c, int layer, int x, int y, int sy)
{
    phc_w w;
    begin(c, &w, "input", 5);
    phc_w_kv_text(&w, "kind", "tap");
    phc_w_kv_int(&w, "layer", layer);
    phc_w_kv_int(&w, "x", x);
    phc_w_kv_int(&w, "y", y);
    phc_w_kv_int(&w, "sy", sy);
    return send_frame(c, &w);
}

/* ---- decoding helpers ---- */

static int get_int(phc_r *r, int *out)
{
    int64_t v;
    if (phc_r_int(r, &v)) return -1;
    *out = (int)v;
    return 0;
}

static int copy_text(phc_r *r, char *dst, size_t cap)
{
    const char *s;
    size_t n;
    if (phc_r_text(r, &s, &n)) return -1;
    if (n >= cap) n = cap - 1;
    memcpy(dst, s, n);
    dst[n] = 0;
    return 0;
}

static const char *pool_text(phc *c, phc_r *r, size_t limit)
{
    const char *s;
    size_t n;
    char *d;
    if (phc_r_text(r, &s, &n)) return "";
    if (n > limit) n = limit;
    if (c->pool_used + n + 1 > sizeof c->pool) return "";
    d = c->pool + c->pool_used;
    memcpy(d, s, n);
    d[n] = 0;
    c->pool_used += n + 1;
    return d;
}

/* ---- frame handlers ---- */

static int on_error(phc *c, phc_r *r)
{
    uint32_t n, i;
    c->errmsg[0] = 0;
    if (phc_r_map(r, &n)) return PHC_ERR_PROTO;
    for (i = 0; i < n; i++) {
        const char *k;
        size_t kl;
        if (phc_r_text(r, &k, &kl)) return PHC_ERR_PROTO;
        if (phc_text_eq(k, kl, "msg")) { if (copy_text(r, c->errmsg, sizeof c->errmsg)) return PHC_ERR_PROTO; }
        else if (phc_r_skip(r)) return PHC_ERR_PROTO;
    }
    return PHC_ERR_SERVER;
}

static int on_page(phc *c, phc_r *r)
{
    uint32_t n, i;
    phc_page p;
    memset(&p, 0, sizeof p);
    if (phc_r_map(r, &n)) return PHC_ERR_PROTO;
    for (i = 0; i < n; i++) {
        const char *k;
        size_t kl;
        int rc;
        if (phc_r_text(r, &k, &kl)) return PHC_ERR_PROTO;
        if (phc_text_eq(k, kl, "pid")) rc = get_int(r, &p.pid);
        else if (phc_text_eq(k, kl, "seq")) rc = get_int(r, &p.seq);
        else if (phc_text_eq(k, kl, "w")) rc = get_int(r, &p.w);
        else if (phc_text_eq(k, kl, "h")) rc = get_int(r, &p.h);
        else if (phc_text_eq(k, kl, "tw")) rc = get_int(r, &p.tw);
        else if (phc_text_eq(k, kl, "th")) rc = get_int(r, &p.th);
        else if (phc_text_eq(k, kl, "trunc")) rc = phc_r_bool(r, &p.trunc);
        else if (phc_text_eq(k, kl, "back")) rc = phc_r_bool(r, &p.back);
        else if (phc_text_eq(k, kl, "fwd")) rc = phc_r_bool(r, &p.fwd);
        else if (phc_text_eq(k, kl, "zoom")) rc = get_int(r, &p.zoom);
        else if (phc_text_eq(k, kl, "reset")) rc = phc_r_bool(r, &p.reset);
        else if (phc_text_eq(k, kl, "url")) rc = copy_text(r, p.url, sizeof p.url);
        else if (phc_text_eq(k, kl, "title")) rc = copy_text(r, p.title, sizeof p.title);
        else rc = phc_r_skip(r);
        if (rc) return PHC_ERR_PROTO;
    }
    if (p.zoom == 0) p.zoom = 100;
    if (p.pid != c->page.pid || p.reset) c->ntiles = 0;   /* a new page, or the same page at another scale: nothing cached applies */
    c->page = p;
    if (c->ev.on_page) c->ev.on_page(c->ev.user, &c->page);
    return PHC_OK;
}

const phc_tile_entry *phc_tile_lookup(const phc *c, int layer, int x, int y)
{
    int i;
    for (i = 0; i < c->ntiles; i++) {
        const phc_tile_entry *e = &c->tiles[i];
        if (e->used && e->pid == c->page.pid && e->layer == layer && e->x == x && e->y == y) return e;
    }
    return NULL;
}

static int on_tile(phc *c, phc_r *r)
{
    uint32_t n, i;
    phc_tile t;
    phc_tile_entry *e = NULL;
    memset(&t, 0, sizeof t);
    if (phc_r_map(r, &n)) return PHC_ERR_PROTO;
    for (i = 0; i < n; i++) {
        const char *k, *s;
        size_t kl, sl;
        int rc;
        if (phc_r_text(r, &k, &kl)) return PHC_ERR_PROTO;
        if (phc_text_eq(k, kl, "pid")) rc = get_int(r, &t.pid);
        else if (phc_text_eq(k, kl, "layer")) rc = get_int(r, &t.layer);
        else if (phc_text_eq(k, kl, "x")) rc = get_int(r, &t.x);
        else if (phc_text_eq(k, kl, "y")) rc = get_int(r, &t.y);
        else if (phc_text_eq(k, kl, "w")) rc = get_int(r, &t.w);
        else if (phc_text_eq(k, kl, "h")) rc = get_int(r, &t.h);
        else if (phc_text_eq(k, kl, "hash")) rc = phc_r_uint(r, &t.hash);
        else if (phc_text_eq(k, kl, "fmt")) {
            rc = phc_r_text(r, &s, &sl);
            if (!rc) t.fmt = phc_text_eq(s, sl, "jpeg") ? PHC_FMT_JPEG : PHC_FMT_PNG;
        } else if (phc_text_eq(k, kl, "data")) rc = phc_r_bytes(r, &t.data, &t.len);
        else rc = phc_r_skip(r);
        if (rc) return PHC_ERR_PROTO;
    }
    /* cache entry: replace the same (layer, x, y) or take a free slot */
    {
        const phc_tile_entry *old = phc_tile_lookup(c, t.layer, t.x, t.y);
        if (old) e = (phc_tile_entry *)old;
        else if (c->ntiles < PHC_MAX_TILES) e = &c->tiles[c->ntiles++];
    }
    if (e) {
        e->used = 1; e->pid = c->page.pid; e->layer = t.layer; e->x = t.x; e->y = t.y; e->w = t.w; e->h = t.h; e->hash = t.hash;
    }
    c->stats.tiles++;
    c->stats.tile_bytes += (long)t.len;
    c->stats.hash_xor ^= t.hash;
    if (c->ev.on_tile) c->ev.on_tile(c->ev.user, &t);
    return PHC_OK;
}

static int on_layers(phc *c, phc_r *r)
{
    uint32_t n, i, cnt, j;
    c->nlayers = 0;
    if (phc_r_map(r, &n)) return PHC_ERR_PROTO;
    for (i = 0; i < n; i++) {
        const char *k;
        size_t kl;
        if (phc_r_text(r, &k, &kl)) return PHC_ERR_PROTO;
        if (!phc_text_eq(k, kl, "list")) { if (phc_r_skip(r)) return PHC_ERR_PROTO; continue; }
        if (phc_r_array(r, &cnt)) return PHC_ERR_PROTO;
        for (j = 0; j < cnt; j++) {
            uint32_t m, q;
            phc_layer l;
            memset(&l, 0, sizeof l);
            if (phc_r_map(r, &m)) return PHC_ERR_PROTO;
            for (q = 0; q < m; q++) {
                const char *kk, *s;
                size_t kkl, sl;
                int rc;
                if (phc_r_text(r, &kk, &kkl)) return PHC_ERR_PROTO;
                if (phc_text_eq(kk, kkl, "id")) rc = get_int(r, &l.id);
                else if (phc_text_eq(kk, kkl, "x")) rc = get_int(r, &l.x);
                else if (phc_text_eq(kk, kkl, "y")) rc = get_int(r, &l.y);
                else if (phc_text_eq(kk, kkl, "w")) rc = get_int(r, &l.w);
                else if (phc_text_eq(kk, kkl, "h")) rc = get_int(r, &l.h);
                else if (phc_text_eq(kk, kkl, "z")) rc = get_int(r, &l.z);
                else if (phc_text_eq(kk, kkl, "cw")) rc = get_int(r, &l.cw);
                else if (phc_text_eq(kk, kkl, "ch")) rc = get_int(r, &l.ch);
                else if (phc_text_eq(kk, kkl, "sx")) rc = get_int(r, &l.sx);
                else if (phc_text_eq(kk, kkl, "sy")) rc = get_int(r, &l.sy);
                else if (phc_text_eq(kk, kkl, "top")) { rc = get_int(r, &l.top); l.has_top = !rc; }
                else if (phc_text_eq(kk, kkl, "bottom")) { rc = get_int(r, &l.bottom); l.has_bottom = !rc; }
                else if (phc_text_eq(kk, kkl, "kind")) {
                    rc = phc_r_text(r, &s, &sl);
                    if (!rc) l.kind = phc_text_eq(s, sl, "sticky") ? PHC_LAYER_STICKY : phc_text_eq(s, sl, "scroll") ? PHC_LAYER_SCROLL : PHC_LAYER_FIXED;
                } else rc = phc_r_skip(r);
                if (rc) return PHC_ERR_PROTO;
            }
            l.scroll_x = l.sx; l.scroll_y = l.sy;
            if (c->nlayers < PHC_MAX_LAYERS) c->layers[c->nlayers++] = l;
        }
    }
    return PHC_OK;
}

static int on_regions(phc *c, phc_r *r)
{
    uint32_t n, i, cnt, j;
    c->nregions = 0;
    c->nopts = 0;
    c->regions_dropped = 0;
    c->pool_used = 0;
    if (phc_r_map(r, &n)) return PHC_ERR_PROTO;
    for (i = 0; i < n; i++) {
        const char *k;
        size_t kl;
        if (phc_r_text(r, &k, &kl)) return PHC_ERR_PROTO;
        if (!phc_text_eq(k, kl, "list")) { if (phc_r_skip(r)) return PHC_ERR_PROTO; continue; }
        if (phc_r_array(r, &cnt)) return PHC_ERR_PROTO;
        for (j = 0; j < cnt; j++) {
            uint32_t m, q;
            phc_region g;
            memset(&g, 0, sizeof g);
            g.href = g.type = g.name = g.placeholder = g.value = g.label = "";
            if (phc_r_map(r, &m)) return PHC_ERR_PROTO;
            for (q = 0; q < m; q++) {
                const char *kk, *s;
                size_t kkl, sl;
                int rc;
                if (phc_r_text(r, &kk, &kkl)) return PHC_ERR_PROTO;
                if (phc_text_eq(kk, kkl, "id")) rc = get_int(r, &g.id);
                else if (phc_text_eq(kk, kkl, "layer")) rc = get_int(r, &g.layer);
                else if (phc_text_eq(kk, kkl, "x")) rc = get_int(r, &g.x);
                else if (phc_text_eq(kk, kkl, "y")) rc = get_int(r, &g.y);
                else if (phc_text_eq(kk, kkl, "w")) rc = get_int(r, &g.w);
                else if (phc_text_eq(kk, kkl, "h")) rc = get_int(r, &g.h);
                else if (phc_text_eq(kk, kkl, "href")) { g.href = pool_text(c, r, 255); rc = r->err; }
                else if (phc_text_eq(kk, kkl, "type")) { g.type = pool_text(c, r, 15); rc = r->err; }
                else if (phc_text_eq(kk, kkl, "name")) { g.name = pool_text(c, r, 31); rc = r->err; }
                else if (phc_text_eq(kk, kkl, "ph")) { g.placeholder = pool_text(c, r, 47); rc = r->err; }
                else if (phc_text_eq(kk, kkl, "value")) { g.value = pool_text(c, r, 63); rc = r->err; }
                else if (phc_text_eq(kk, kkl, "label")) { g.label = pool_text(c, r, 47); rc = r->err; }
                else if (phc_text_eq(kk, kkl, "opts")) {
                    uint32_t no, oi;
                    rc = phc_r_array(r, &no);
                    g.opt_first = c->nopts;
                    for (oi = 0; !rc && oi < no; oi++) {
                        uint32_t om, oq;
                        phc_option o;
                        o.label = ""; o.selected = 0;
                        if (phc_r_map(r, &om)) { rc = -1; break; }
                        for (oq = 0; oq < om; oq++) {
                            const char *ok;
                            size_t okl;
                            if (phc_r_text(r, &ok, &okl)) { rc = -1; break; }
                            if (phc_text_eq(ok, okl, "l")) { o.label = pool_text(c, r, 47); if (r->err) rc = -1; }
                            else if (phc_text_eq(ok, okl, "s")) { if (phc_r_bool(r, &o.selected)) rc = -1; }
                            else if (phc_r_skip(r)) rc = -1;
                            if (rc) break;
                        }
                        if (rc) break;
                        if (c->nopts < PHC_MAX_OPTS) { c->opts[c->nopts++] = o; g.opt_count++; }
                    }
                }
                else if (phc_text_eq(kk, kkl, "kind")) {
                    rc = phc_r_text(r, &s, &sl);
                    if (!rc) {
                        g.kind = phc_text_eq(s, sl, "link") ? PHC_REGION_LINK
                               : phc_text_eq(s, sl, "input") ? PHC_REGION_INPUT
                               : phc_text_eq(s, sl, "select") ? PHC_REGION_SELECT
                               : phc_text_eq(s, sl, "textarea") ? PHC_REGION_TEXTAREA : PHC_REGION_BUTTON;
                    }
                } else rc = phc_r_skip(r);
                if (rc) return PHC_ERR_PROTO;
            }
            if (c->nregions < PHC_MAX_REGIONS) c->regions[c->nregions++] = g;
            else c->regions_dropped++;
        }
    }
    return PHC_OK;
}

static int on_focus(phc *c, phc_r *r)
{
    uint32_t n, i;
    phc_focus f;
    memset(&f, 0, sizeof f);
    f.start = f.end = -1;
    if (phc_r_map(r, &n)) return PHC_ERR_PROTO;
    for (i = 0; i < n; i++) {
        const char *k, *s;
        size_t kl, sl;
        int rc;
        if (phc_r_text(r, &k, &kl)) return PHC_ERR_PROTO;
        if (phc_text_eq(k, kl, "id")) rc = get_int(r, &f.id);
        else if (phc_text_eq(k, kl, "start")) rc = get_int(r, &f.start);
        else if (phc_text_eq(k, kl, "end")) rc = get_int(r, &f.end);
        else if (phc_text_eq(k, kl, "type")) rc = copy_text(r, f.type, sizeof f.type);
        else if (phc_text_eq(k, kl, "value")) rc = copy_text(r, f.value, sizeof f.value);
        else if (phc_text_eq(k, kl, "im")) rc = copy_text(r, f.inputmode, sizeof f.inputmode);
        else if (phc_text_eq(k, kl, "enter")) rc = copy_text(r, f.enter, sizeof f.enter);
        else if (phc_text_eq(k, kl, "kind")) {
            rc = phc_r_text(r, &s, &sl);
            if (!rc) f.kind = phc_text_eq(s, sl, "text") ? PHC_FOCUS_TEXT
                            : phc_text_eq(s, sl, "textarea") ? PHC_FOCUS_TEXTAREA
                            : phc_text_eq(s, sl, "select") ? PHC_FOCUS_SELECT : PHC_FOCUS_NONE;
        } else rc = phc_r_skip(r);
        if (rc) return PHC_ERR_PROTO;
    }
    c->focus = f;
    if (c->ev.on_focus) c->ev.on_focus(c->ev.user, &c->focus);
    return PHC_OK;
}

static int on_dialog(phc *c, phc_r *r)
{
    uint32_t n, i;
    phc_dialog d;
    memset(&d, 0, sizeof d);
    if (phc_r_map(r, &n)) return PHC_ERR_PROTO;
    for (i = 0; i < n; i++) {
        const char *k, *s;
        size_t kl, sl;
        int rc;
        if (phc_r_text(r, &k, &kl)) return PHC_ERR_PROTO;
        if (phc_text_eq(k, kl, "id")) rc = get_int(r, &d.id);
        else if (phc_text_eq(k, kl, "msg")) rc = copy_text(r, d.message, sizeof d.message);
        else if (phc_text_eq(k, kl, "def")) rc = copy_text(r, d.def, sizeof d.def);
        else if (phc_text_eq(k, kl, "kind")) {
            rc = phc_r_text(r, &s, &sl);
            if (!rc) d.kind = phc_text_eq(s, sl, "confirm") ? PHC_DLG_CONFIRM
                            : phc_text_eq(s, sl, "prompt") ? PHC_DLG_PROMPT
                            : phc_text_eq(s, sl, "beforeunload") ? PHC_DLG_BEFOREUNLOAD
                            : phc_text_eq(s, sl, "auth") ? PHC_DLG_AUTH
                            : phc_text_eq(s, sl, "cert") ? PHC_DLG_CERT : PHC_DLG_ALERT;
        } else rc = phc_r_skip(r);
        if (rc) return PHC_ERR_PROTO;
    }
    /* no handler: dismiss, so the page does not stay blocked */
    if (c->ev.on_dialog) c->ev.on_dialog(c->ev.user, &d);
    else phc_dialog_reply(c, d.id, 0, "");
    return PHC_OK;
}

static int on_download(phc *c, phc_r *r)
{
    uint32_t n, i;
    phc_download d;
    memset(&d, 0, sizeof d);
    if (phc_r_map(r, &n)) return PHC_ERR_PROTO;
    for (i = 0; i < n; i++) {
        const char *k;
        size_t kl;
        int rc;
        if (phc_r_text(r, &k, &kl)) return PHC_ERR_PROTO;
        if (phc_text_eq(k, kl, "url")) rc = copy_text(r, d.url, sizeof d.url);
        else if (phc_text_eq(k, kl, "name")) rc = copy_text(r, d.name, sizeof d.name);
        else if (phc_text_eq(k, kl, "refused")) rc = phc_r_bool(r, &d.refused);
        else rc = phc_r_skip(r);
        if (rc) return PHC_ERR_PROTO;
    }
    if (c->ev.on_download) c->ev.on_download(c->ev.user, &d);
    return PHC_OK;
}

static int on_done(phc *c, phc_r *r)
{
    uint32_t n, i;
    phc_done d;
    memset(&d, 0, sizeof d);
    if (phc_r_map(r, &n)) return PHC_ERR_PROTO;
    for (i = 0; i < n; i++) {
        const char *k;
        size_t kl;
        int rc;
        if (phc_r_text(r, &k, &kl)) return PHC_ERR_PROTO;
        if (phc_text_eq(k, kl, "pid")) rc = get_int(r, &d.pid);
        else if (phc_text_eq(k, kl, "seq")) rc = get_int(r, &d.seq);
        else if (phc_text_eq(k, kl, "ms")) rc = get_int(r, &d.ms);
        else if (phc_text_eq(k, kl, "tiles")) rc = get_int(r, &d.tiles);
        else if (phc_text_eq(k, kl, "bytes")) rc = get_int(r, &d.bytes);
        else if (phc_text_eq(k, kl, "more")) rc = phc_r_bool(r, &d.more);
        else rc = phc_r_skip(r);
        if (rc) return PHC_ERR_PROTO;
    }
    c->done = d;
    return PHC_OK;
}

/* the "v" (view) of a frame: 1 when absent */
static int frame_view(const uint8_t *p, size_t n)
{
    phc_r r;
    uint32_t cnt, i;
    phc_r_init(&r, p, n);
    if (phc_r_map(&r, &cnt)) return 1;
    for (i = 0; i < cnt; i++) {
        const char *k;
        size_t kl;
        uint64_t v;
        if (phc_r_text(&r, &k, &kl)) return 1;
        if (phc_text_eq(k, kl, "v")) return phc_r_uint(&r, &v) ? 1 : (int)v;
        if (phc_r_skip(&r)) return 1;
    }
    return 1;
}

static int on_tabs(phc *c, phc_r *r)
{
    uint32_t n, i, cnt, j;
    c->ntabs = 0;
    if (phc_r_map(r, &n)) return PHC_ERR_PROTO;
    for (i = 0; i < n; i++) {
        const char *k;
        size_t kl;
        if (phc_r_text(r, &k, &kl)) return PHC_ERR_PROTO;
        if (phc_text_eq(k, kl, "active")) { if (get_int(r, &c->active_tab)) return PHC_ERR_PROTO; continue; }
        if (!phc_text_eq(k, kl, "list")) { if (phc_r_skip(r)) return PHC_ERR_PROTO; continue; }
        if (phc_r_array(r, &cnt)) return PHC_ERR_PROTO;
        for (j = 0; j < cnt; j++) {
            uint32_t m, q;
            phc_tabinfo t;
            memset(&t, 0, sizeof t);
            if (phc_r_map(r, &m)) return PHC_ERR_PROTO;
            for (q = 0; q < m; q++) {
                const char *kk;
                size_t kkl;
                int rc;
                if (phc_r_text(r, &kk, &kkl)) return PHC_ERR_PROTO;
                if (phc_text_eq(kk, kkl, "v")) rc = get_int(r, &t.v);
                else if (phc_text_eq(kk, kkl, "title")) rc = copy_text(r, t.title, sizeof t.title);
                else if (phc_text_eq(kk, kkl, "url")) rc = copy_text(r, t.url, sizeof t.url);
                else rc = phc_r_skip(r);
                if (rc) return PHC_ERR_PROTO;
            }
            if (c->ntabs < PHC_MAX_TABS) c->tabs[c->ntabs++] = t;
        }
    }
    return 2;
}

/* returns PHC_OK to keep reading, 1 after a `done`, 2 after a `tabs` frame, or an error */
static int dispatch(phc *c, size_t len)
{
    phc_r r;
    int rc, t = frame_type(c->rx, len);
    phc_r_init(&r, c->rx, len);
    switch (t) {   /* the page frames of a view that is not shown are ignored */
    case T_PAGE: case T_TILE: case T_LAYERS: case T_REGIONS: case T_FOCUS: case T_DONE:
        if (frame_view(c->rx, len) != c->view) return PHC_OK;
        break;
    default: break;
    }
    switch (t) {
    case T_ERROR: return on_error(c, &r);
    case T_PAGE: return on_page(c, &r);
    case T_TILE: return on_tile(c, &r);
    case T_LAYERS: return on_layers(c, &r);
    case T_REGIONS: return on_regions(c, &r);
    case T_FOCUS: return on_focus(c, &r);
    case T_DIALOG: return on_dialog(c, &r);
    case T_DOWNLOAD: return on_download(c, &r);
    case T_TABS: return on_tabs(c, &r);
    case T_DONE: rc = on_done(c, &r); return rc ? rc : 1;
    case T_PONG: return PHC_OK;
    default: return PHC_OK;   /* unknown frame types are ignored: the protocol may grow */
    }
}

static int pump_until(phc *c, int want)
{
    for (;;) {
        size_t len;
        int rc = read_frame(c, &len);
        if (rc) return rc;
        rc = dispatch(c, len);
        if (rc == want) return PHC_OK;
        if (rc == 1 || rc == 2) continue;   /* a done / tabs frame we did not wait for */
        if (rc) return rc;
    }
}

int phc_pump(phc *c) { return pump_until(c, 1); }
int phc_pump_tabs(phc *c) { return pump_until(c, 2); }

int phc_tab(phc *c, const char *action, int v)
{
    phc_w w;
    int saved = c->view;
    c->view = v;                 /* the tab frame's "v" is the target view */
    begin(c, &w, "tab", 1);
    c->view = saved;
    phc_w_kv_text(&w, "action", action);
    return send_frame(c, &w);
}

void phc_set_view(phc *c, int v) { c->view = v; }

int phc_zoom(phc *c, int pct)
{
    phc_w w;
    begin(c, &w, "zoom", 1);
    phc_w_kv_int(&w, "pct", pct);
    return send_frame(c, &w);
}

/* ---- authentication ---- */

int phc_auth(phc *c, phc_creds *cr, const char *code, const char *devname)
{
    size_t len;
    int rc, t;
    phc_r r;
    uint32_t n, i;
    phc_w w;
    static const char suffix[] = "phecloud-v1";

    rc = read_frame_wait(c, &len);
    if (rc) return rc;
    t = frame_type(c->rx, len);
    if (t != T_CHALLENGE) return PHC_ERR_PROTO;
    c->nonce_len = 0;
    phc_r_init(&r, c->rx, len);
    if (phc_r_map(&r, &n)) return PHC_ERR_PROTO;
    for (i = 0; i < n; i++) {
        const char *k;
        size_t kl;
        if (phc_r_text(&r, &k, &kl)) return PHC_ERR_PROTO;
        if (phc_text_eq(k, kl, "nonce")) {
            const uint8_t *b;
            size_t bl;
            if (phc_r_bytes(&r, &b, &bl) || bl > sizeof c->nonce) return PHC_ERR_PROTO;
            memcpy(c->nonce, b, bl);
            c->nonce_len = bl;
        } else if (phc_r_skip(&r)) return PHC_ERR_PROTO;
    }
    if (c->nonce_len == 0) return PHC_ERR_PROTO;

    if (code) {
        begin(c, &w, "pair", 2);
        phc_w_kv_text(&w, "code", code);
        phc_w_kv_text(&w, "name", devname ? devname : "phone");
    } else {
        uint8_t mac[32];
        if (!cr || !cr->have) return PHC_ERR_AUTH;
        phc_hmac_sha256(cr->key, sizeof cr->key, c->nonce, c->nonce_len, suffix, sizeof suffix - 1, mac);
        begin(c, &w, "hello", 2);
        phc_w_kv_text(&w, "dev", cr->dev);
        phc_w_kv_bytes(&w, "mac", mac, sizeof mac);
    }
    rc = send_frame(c, &w);
    if (rc) return rc;

    rc = read_frame_wait(c, &len);
    if (rc) return rc;
    t = frame_type(c->rx, len);
    phc_r_init(&r, c->rx, len);
    if (t == T_ERROR) { on_error(c, &r); return PHC_ERR_AUTH; }
    if (t != T_WELCOME) return PHC_ERR_PROTO;
    if (phc_r_map(&r, &n)) return PHC_ERR_PROTO;
    for (i = 0; i < n; i++) {
        const char *k;
        size_t kl;
        if (phc_r_text(&r, &k, &kl)) return PHC_ERR_PROTO;
        if (code && phc_text_eq(k, kl, "dev")) {
            if (copy_text(&r, cr->dev, sizeof cr->dev)) return PHC_ERR_PROTO;
        } else if (code && phc_text_eq(k, kl, "key")) {
            const uint8_t *b;
            size_t bl;
            if (phc_r_bytes(&r, &b, &bl) || bl != sizeof cr->key) return PHC_ERR_PROTO;
            memcpy(cr->key, b, bl);
            cr->have = 1;
        } else if (phc_text_eq(k, kl, "features")) {
            uint32_t fc, fi;
            if (phc_r_array(&r, &fc)) return PHC_ERR_PROTO;
            for (fi = 0; fi < fc; fi++) {
                const char *fs;
                size_t fl;
                if (phc_r_text(&r, &fs, &fl)) return PHC_ERR_PROTO;
                if (phc_text_eq(fs, fl, "zoom")) c->feat_zoom = 1;
                if (phc_text_eq(fs, fl, "tabs")) c->feat_tabs = 1;
            }
        } else if (phc_r_skip(&r)) return PHC_ERR_PROTO;
    }
    return PHC_OK;
}

/* ---- hit test ---- */

int phc_layer_screen_y(const phc_layer *l, int scroll_y, int view_h)
{
    int y = l->y;
    if (l->kind == PHC_LAYER_FIXED) return y;
    y -= scroll_y;
    if (l->kind == PHC_LAYER_SCROLL) return y;   /* a scroll box moves with the page */
    if (l->has_top) { if (y < l->top) y = l->top; }
    else if (l->has_bottom) { if (y + l->h > view_h - l->bottom) y = view_h - l->bottom - l->h; }
    return y;
}

int phc_hit_test(const phc *c, int scroll_y, int view_h, int sx, int sy, phc_hit *out)
{
    const phc_layer *top = NULL;
    const phc_region *best = NULL;
    int i, ly = 0, lx = 0, layer = 0;
    long best_area = 0;

    memset(out, 0, sizeof *out);
    for (i = 0; i < c->nlayers; i++) {
        const phc_layer *l = &c->layers[i];
        int y = phc_layer_screen_y(l, scroll_y, view_h);
        if (sx < l->x || sx >= l->x + l->w || sy < y || sy >= y + l->h) continue;
        if (!top || l->z > top->z || (l->z == top->z && l->id > top->id)) top = l;
    }
    if (top) {
        layer = top->id;
        lx = sx - top->x;
        ly = sy - phc_layer_screen_y(top, scroll_y, view_h);
        if (top->kind == PHC_LAYER_SCROLL) { lx += top->scroll_x; ly += top->scroll_y; }   /* content coordinates */
    } else {
        lx = sx;
        ly = sy + scroll_y;
    }
    for (i = 0; i < c->nregions; i++) {
        const phc_region *g = &c->regions[i];
        long area;
        if (g->layer != layer) continue;
        if (lx < g->x || lx >= g->x + g->w || ly < g->y || ly >= g->y + g->h) continue;
        area = (long)g->w * g->h;
        if (!best || area < best_area) { best = g; best_area = area; }
    }
    out->layer = layer;
    out->region = best;
    if (top && top->kind != PHC_LAYER_STICKY) {   /* fixed and scroll layers: the server maps layer-relative (content) coordinates */
        out->tap_layer = layer; out->tap_x = lx; out->tap_y = ly;
    } else {                                     /* page or sticky: document coordinates of the screen point */
        out->tap_layer = 0; out->tap_x = sx; out->tap_y = sy + scroll_y;
    }
    return best != NULL;
}

int phc_tap_screen(phc *c, int scroll_y, int view_h, int sx, int sy, phc_hit *out)
{
    phc_hit h;
    phc_hit_test(c, scroll_y, view_h, sx, sy, &h);
    if (out) *out = h;
    return send_tap(c, h.tap_layer, h.tap_x, h.tap_y, scroll_y);
}
