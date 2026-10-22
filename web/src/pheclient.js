// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The phecloud protocol over a WebSocket: every message is one binary CBOR frame (no length prefix). Same pairing and
// HMAC authentication as the phone clients; the device key lives in IndexedDB (see idb.js).
import { encode, decode } from 'cbor-x'
import { hmac } from '@noble/hashes/hmac'
import { sha256 } from '@noble/hashes/sha256'

const enc = new TextEncoder()

export class PheClient {
  constructor() {
    this.ws = null
    this.onframe = () => {}
    this.onclose = () => {}
    this.view = 1 // the view (tab) later requests are about
  }

  // Resolves with the welcome frame ({dev, key (pairing only), admin}); rejects with the server's error text.
  // creds = {dev, key} to authenticate, or pass code (+ name) to pair.
  connect(url, { creds, code, name } = {}) {
    return new Promise((resolve, reject) => {
      const ws = new WebSocket(url)
      ws.binaryType = 'arraybuffer'
      this.ws = ws
      let stage = 'challenge'
      let settled = false
      const fail = (e) => { if (!settled) { settled = true; reject(e instanceof Error ? e : new Error(String(e))) } }
      ws.onerror = () => fail('connection failed')
      ws.onclose = () => { fail('connection closed'); this.onclose() }
      ws.onmessage = (ev) => {
        const f = decode(new Uint8Array(ev.data))
        if (stage === 'challenge') {
          if (f.t !== 'challenge') return fail('unexpected ' + f.t)
          stage = 'welcome'
          if (code) {
            this.send({ t: 'pair', code, name: name || 'web' })
          } else if (creds) {
            const msg = new Uint8Array([...f.nonce, ...enc.encode('phecloud-v1')])
            this.send({ t: 'hello', dev: creds.dev, mac: hmac(sha256, creds.key, msg) })
          } else fail('no credentials')
        } else if (stage === 'welcome') {
          if (f.t === 'error') return fail(f.msg)
          if (f.t !== 'welcome') return fail('unexpected ' + f.t)
          stage = 'open'
          settled = true
          resolve(f)
        } else {
          this.onframe(f)
        }
      }
    })
  }

  send(frame) {
    if (frame.v === undefined && frame.t !== 'pair' && frame.t !== 'hello') frame.v = this.view
    if (this.ws && this.ws.readyState === 1) this.ws.send(encode(frame))
  }

  open(url) { this.send({ t: 'open', url, width: 360 }) }
  navigate(url) { this.send({ t: 'navigate', url }) }
  action(a) { this.send({ t: 'navigate', action: a }) }
  tap(layer, x, y, sy) { this.send({ t: 'input', kind: 'tap', layer, x, y, sy }) }
  scroll(y) { this.send({ t: 'input', kind: 'scroll', y }) }
  text(id, text, replace) { this.send({ t: 'input', kind: 'text', id, text, replace: !!replace }) }
  key(name) { this.send({ t: 'input', kind: 'key', text: name }) }
  select(id, index) { this.send({ t: 'input', kind: 'select', id, index }) }
  dialogReply(id, ok, text, pass) { this.send({ t: 'dialogreply', id, ok, text: text || '', pass: pass || '' }) }
  settings(q, gray) { this.send({ t: 'settings', q, gray }) }
  devices() { this.send({ t: 'devices' }) }
  revoke(id) { this.send({ t: 'revoke', id }) }
  tab(action, v) { this.send({ t: 'tab', action, v: v || 0 }) }
  zoom(pct) { this.send({ t: 'zoom', pct }) }

  close() { if (this.ws) { this.ws.onclose = null; this.ws.close() } this.ws = null }
}
