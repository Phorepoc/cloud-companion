/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <unistd.h>
#include <fcntl.h>
#include <errno.h>
#include <sys/types.h>
#include <sys/time.h>
#include <sys/select.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <netdb.h>

#include "../phc.h"
#include "../phc_mbedtls.h"
#include "ss_net.h"

#define INI_PATH "C:\data\phecloud.ini"
#define RXCAP (16 * 1024)   /* SMS frames are small: no tiles here */

static phc *pc;
static uint8_t *rxbuf;
static phc_tls tls;
static int sock = -1;
static int fd_for_io;

typedef struct { char host[128], port[8], pin[80], dev[48], key[80], name[48]; } cfg_t;

static int hexval(int c) { return c >= '0' && c <= '9' ? c - '0' : c >= 'a' && c <= 'f' ? c - 'a' + 10 : c >= 'A' && c <= 'F' ? c - 'A' + 10 : -1; }
static int unhex(const char *s, uint8_t *out, size_t n)
{
    size_t i;
    if (strlen(s) != n * 2) return -1;
    for (i = 0; i < n; i++) { int h = hexval(s[2 * i]), l = hexval(s[2 * i + 1]); if (h < 0 || l < 0) return -1; out[i] = (uint8_t)(h * 16 + l); }
    return 0;
}
static void cp(char *d, size_t cap, const char *s) { strncpy(d, s, cap - 1); d[cap - 1] = 0; }

static int cfg_read(cfg_t *c)
{
    FILE *f = fopen(INI_PATH, "r");
    char line[256];
    memset(c, 0, sizeof *c);
    cp(c->port, sizeof c->port, "7400");
    cp(c->name, sizeof c->name, "Phorepoc");
    if (!f) return -1;
    while (fgets(line, sizeof line, f)) {
        char *eq = strchr(line, '='), *e;
        if (!eq) continue;
        *eq++ = 0;
        for (e = eq + strlen(eq); e > eq && (e[-1] == '\n' || e[-1] == '\r' || e[-1] == ' '); ) *--e = 0;
        if (!strcmp(line, "host")) cp(c->host, sizeof c->host, eq);
        else if (!strcmp(line, "port")) cp(c->port, sizeof c->port, eq);
        else if (!strcmp(line, "pin")) cp(c->pin, sizeof c->pin, eq);
        else if (!strcmp(line, "dev")) cp(c->dev, sizeof c->dev, eq);
        else if (!strcmp(line, "key")) cp(c->key, sizeof c->key, eq);
        else if (!strcmp(line, "name")) cp(c->name, sizeof c->name, eq);
    }
    fclose(f);
    return c->host[0] && c->dev[0] && c->key[0] && c->pin[0] ? 0 : -1;   /* paired and pinned only: this service has no pairing screen */
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
static int raw_send(void *ctx, const void *p, size_t n)
{
    int r = (int)send(*(int *)ctx, p, n, 0);
    if (r >= 0) return r;
    return (errno == EAGAIN || errno == EWOULDBLOCK || errno == EINTR) ? 0 : -1;
}
static int raw_recv(void *ctx, void *p, size_t n)
{
    int r = (int)recv(*(int *)ctx, p, n, 0);
    if (r > 0) return r;
    if (r == 0) return -1;
    return (errno == EAGAIN || errno == EWOULDBLOCK || errno == EINTR) ? 0 : -1;
}
static void raw_wait(void *ctx, int want_write) { wait_fd(*(int *)ctx, want_write, 30000); }

void ss_close(void)
{
    if (pc) { phc_tls_close(&tls); free(pc); pc = NULL; }
    free(rxbuf); rxbuf = NULL;
    if (sock >= 0) { close(sock); sock = -1; }
}

int ss_open(void)
{
    cfg_t cfg;
    uint8_t pin[32];
    phc_creds cr;
    struct addrinfo hints, *res, *a;
    phc_raw_io raw;
    if (pc) return 0;
    if (cfg_read(&cfg) != 0 || unhex(cfg.pin, pin, 32) != 0 || unhex(cfg.key, cr.key, 32) != 0) return -1;
    memset(&hints, 0, sizeof hints);
    hints.ai_family = AF_UNSPEC; hints.ai_socktype = SOCK_STREAM;
    if (getaddrinfo(cfg.host, cfg.port, &hints, &res) != 0) return -2;
    for (a = res; a; a = a->ai_next) {
        sock = socket(a->ai_family, a->ai_socktype, a->ai_protocol);
        if (sock < 0) continue;
        if (connect(sock, a->ai_addr, a->ai_addrlen) == 0) break;
        close(sock); sock = -1;
    }
    freeaddrinfo(res);
    if (sock < 0) return -2;
    fcntl(sock, F_SETFL, fcntl(sock, F_GETFL, 0) | O_NONBLOCK);
    fd_for_io = sock;
    pc = (phc *)calloc(1, sizeof(phc));
    rxbuf = (uint8_t *)malloc(RXCAP);
    if (!pc || !rxbuf) { ss_close(); return -3; }
    raw.ctx = &fd_for_io; raw.send = raw_send; raw.recv = raw_recv; raw.wait = raw_wait;
    if (phc_tls_init(&tls, raw, pin) != 0) { free(pc); pc = NULL; ss_close(); return -4; }
    if (phc_tls_handshake(&tls) != PHC_OK) { ss_close(); return -5; }
    phc_init(pc, phc_tls_transport(&tls, 0), rxbuf, RXCAP);
    cp(cr.dev, sizeof cr.dev, cfg.dev);
    cr.have = 1;
    if (phc_auth(pc, &cr, NULL, cfg.name) != PHC_OK) { ss_close(); return -6; }
    return 0;
}

int ss_sync(const phc_sms_msg *m, int nm, const phc_sms_ack *a, int na, phc_sms_pending *out, int maxp, int *npend, int *accepted)
{
    int rc = ss_open();
    if (rc) return rc;
    rc = phc_sms_sync(pc, m, nm, a, na, out, maxp, npend, accepted);
    if (rc) ss_close();
    return rc;
}
