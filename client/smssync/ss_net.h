/* Copyright The Phorepoc Authors
 * SPDX-License-Identifier: Apache-2.0 */
/* The network half of phesmssync: one TLS 1.3 connection to the phecloud server (pinned certificate, device key from C:\data\phecloud.ini,
 * written by the Phecloud engine's pairing screen), and the SMS sync request over it. Plain C (Open C sockets), blocking with timeouts. */
#ifndef SS_NET_H
#define SS_NET_H
#include "../phc.h"
#ifdef __cplusplus
extern "C" {
#endif
/* connect + authenticate; 0 on success. Reconnects are the caller's business (call ss_close, then ss_open again). */
int ss_open(void);
void ss_close(void);
/* one sync round trip (see phc_sms_sync); the connection is opened when needed. Returns 0, or a negative phc error (the link is closed). */
int ss_sync(const phc_sms_msg *m, int nm, const phc_sms_ack *a, int na, phc_sms_pending *out, int maxp, int *npend, int *accepted);
#ifdef __cplusplus
}
#endif
#endif
