/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */

/*
 * phc_mbedtls -- a TLS 1.3 transport for the phc core on top of mbedTLS 3.6 (Apache-2.0; an external dependency, not vendored).
 *
 * The host supplies a raw non-blocking byte pipe (a socket); this adapter runs the handshake, pins the server certificate by
 * the SHA-256 of its DER encoding (no chain validation: the server is self-signed and the pin is the trust anchor), and exposes
 * a phc_transport. TLS 1.3 only. No malloc of its own (mbedTLS allocates internally; on the phone point mbedtls at a static
 * pool with MBEDTLS_MEMORY_BUFFER_ALLOC_C).
 *
 * Random numbers: mbedTLS 3.6 needs an entropy source. The host build uses the default platform entropy; a Symbian build must
 * provide mbedtls_hardware_poll() (for example from the kernel's secure random).
 */
#ifndef PHC_MBEDTLS_H
#define PHC_MBEDTLS_H

#include "phc.h"

#include <mbedtls/ctr_drbg.h>
#include <mbedtls/entropy.h>
#include <mbedtls/ssl.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct {
    void *ctx;
    int (*send)(void *ctx, const void *p, size_t n);   /* > 0 bytes sent, 0 = would block, < 0 = error */
    int (*recv)(void *ctx, void *p, size_t n);         /* > 0 bytes read, 0 = would block, < 0 = error or end of stream */
    void (*wait)(void *ctx, int want_write);           /* block until readable (or writable); NULL = spin */
} phc_raw_io;

typedef struct {
    mbedtls_ssl_context ssl;
    mbedtls_ssl_config conf;
    mbedtls_entropy_context entropy;
    mbedtls_ctr_drbg_context drbg;
    phc_raw_io raw;
    uint8_t pin[32];
    int have_pin;
    uint8_t seen[32];       /* SHA-256 of the server certificate, after a handshake */
    int nonblocking;
} phc_tls;

/* pin = the expected SHA-256 of the server certificate (32 bytes) or NULL = trust on first use (read it with
 * phc_tls_peer_fingerprint after the handshake and store it). Returns 0 or an mbedTLS error. */
int phc_tls_init(phc_tls *t, phc_raw_io raw, const uint8_t *pin);
/* runs the handshake; returns PHC_OK, PHC_ERR_AUTH on a pin mismatch (phc_tls_peer_fingerprint still works), PHC_ERR_IO */
int phc_tls_handshake(phc_tls *t);
void phc_tls_peer_fingerprint(const phc_tls *t, uint8_t out[32]);
/* the phc transport; nonblocking = 1: recv returns PHC_WOULDBLOCK between frames instead of waiting */
phc_transport phc_tls_transport(phc_tls *t, int nonblocking);
void phc_tls_close(phc_tls *t);

#ifdef __cplusplus
}
#endif
#endif
