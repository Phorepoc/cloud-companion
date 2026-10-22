/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */
#include "phc_mbedtls.h"
#include "phc_sha256.h"

#include <psa/crypto.h>
#include <string.h>

static int bio_send(void *ctx, const unsigned char *buf, size_t len)
{
    phc_tls *t = (phc_tls *)ctx;
    int r = t->raw.send(t->raw.ctx, buf, len);
    if (r > 0) return r;
    if (r == 0) return MBEDTLS_ERR_SSL_WANT_WRITE;
    return MBEDTLS_ERR_SSL_INTERNAL_ERROR;
}

static int bio_recv(void *ctx, unsigned char *buf, size_t len)
{
    phc_tls *t = (phc_tls *)ctx;
    int r = t->raw.recv(t->raw.ctx, buf, len);
    if (r > 0) return r;
    if (r == 0) return MBEDTLS_ERR_SSL_WANT_READ;
    return MBEDTLS_ERR_SSL_CONN_EOF;
}

static void wait_io(phc_tls *t, int want_write)
{
    if (t->raw.wait) t->raw.wait(t->raw.ctx, want_write);
}

int phc_tls_init(phc_tls *t, phc_raw_io raw, const uint8_t *pin)
{
    static const char pers[] = "phc";
    int rc;
    memset(t, 0, sizeof *t);
    t->raw = raw;
    if (pin) { memcpy(t->pin, pin, 32); t->have_pin = 1; }
    mbedtls_ssl_init(&t->ssl);
    mbedtls_ssl_config_init(&t->conf);
    mbedtls_entropy_init(&t->entropy);
    mbedtls_ctr_drbg_init(&t->drbg);
    if (psa_crypto_init() != PSA_SUCCESS) return -1;
    rc = mbedtls_ctr_drbg_seed(&t->drbg, mbedtls_entropy_func, &t->entropy, (const unsigned char *)pers, sizeof pers - 1);
    if (rc) return rc;
    rc = mbedtls_ssl_config_defaults(&t->conf, MBEDTLS_SSL_IS_CLIENT, MBEDTLS_SSL_TRANSPORT_STREAM, MBEDTLS_SSL_PRESET_DEFAULT);
    if (rc) return rc;
    mbedtls_ssl_conf_min_tls_version(&t->conf, MBEDTLS_SSL_VERSION_TLS1_3);
    mbedtls_ssl_conf_max_tls_version(&t->conf, MBEDTLS_SSL_VERSION_TLS1_3);
    mbedtls_ssl_conf_authmode(&t->conf, MBEDTLS_SSL_VERIFY_NONE);   /* the pin replaces chain validation */
    mbedtls_ssl_conf_rng(&t->conf, mbedtls_ctr_drbg_random, &t->drbg);
    rc = mbedtls_ssl_setup(&t->ssl, &t->conf);
    if (rc) return rc;
    mbedtls_ssl_set_bio(&t->ssl, t, bio_send, bio_recv, NULL);
    return 0;
}

int phc_tls_handshake(phc_tls *t)
{
    const mbedtls_x509_crt *crt;
    phc_sha256 h;
    for (;;) {
        int rc = mbedtls_ssl_handshake(&t->ssl);
        if (rc == 0) break;
        if (rc == MBEDTLS_ERR_SSL_WANT_READ) { wait_io(t, 0); continue; }
        if (rc == MBEDTLS_ERR_SSL_WANT_WRITE) { wait_io(t, 1); continue; }
        return PHC_ERR_IO;
    }
    crt = mbedtls_ssl_get_peer_cert(&t->ssl);
    if (!crt) return PHC_ERR_AUTH;
    phc_sha256_init(&h);
    phc_sha256_update(&h, crt->raw.p, crt->raw.len);
    phc_sha256_final(&h, t->seen);
    if (t->have_pin && memcmp(t->pin, t->seen, 32) != 0) return PHC_ERR_AUTH;
    return PHC_OK;
}

void phc_tls_peer_fingerprint(const phc_tls *t, uint8_t out[32]) { memcpy(out, t->seen, 32); }

static int tls_send(void *ctx, const void *p, size_t n)
{
    phc_tls *t = (phc_tls *)ctx;
    size_t off = 0;
    while (off < n) {
        int rc = mbedtls_ssl_write(&t->ssl, (const unsigned char *)p + off, n - off);
        if (rc > 0) { off += (size_t)rc; continue; }
        if (rc == MBEDTLS_ERR_SSL_WANT_WRITE) { wait_io(t, 1); continue; }
        if (rc == MBEDTLS_ERR_SSL_WANT_READ) { wait_io(t, 0); continue; }
        return -1;
    }
    return (int)n;
}

static int tls_recv(void *ctx, void *p, size_t n)
{
    phc_tls *t = (phc_tls *)ctx;
    for (;;) {
        int rc = mbedtls_ssl_read(&t->ssl, (unsigned char *)p, n);
        if (rc > 0) return rc;
        if (rc == 0 || rc == MBEDTLS_ERR_SSL_PEER_CLOSE_NOTIFY) return 0;
        if (rc == MBEDTLS_ERR_SSL_RECEIVED_NEW_SESSION_TICKET) continue;   /* TLS 1.3 post-handshake ticket: not data */
        if (rc == MBEDTLS_ERR_SSL_WANT_READ) {
            if (t->nonblocking) return PHC_WOULDBLOCK;
            wait_io(t, 0);
            continue;
        }
        if (rc == MBEDTLS_ERR_SSL_WANT_WRITE) { wait_io(t, 1); continue; }
        return -1;
    }
}

static void tls_wait(void *ctx) { wait_io((phc_tls *)ctx, 0); }

phc_transport phc_tls_transport(phc_tls *t, int nonblocking)
{
    phc_transport io;
    t->nonblocking = nonblocking;
    io.ctx = t;
    io.send = tls_send;
    io.recv = tls_recv;
    io.wait = tls_wait;
    return io;
}

void phc_tls_close(phc_tls *t)
{
    mbedtls_ssl_close_notify(&t->ssl);
    mbedtls_ssl_free(&t->ssl);
    mbedtls_ssl_config_free(&t->conf);
    mbedtls_ctr_drbg_free(&t->drbg);
    mbedtls_entropy_free(&t->entropy);
}
