// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The phecloud protocol over a WebSocket: every message is one binary CBOR frame (no length prefix). Same pairing and
// HMAC authentication as the phone clients; the device key lives in IndexedDB (see idb.ts).
import { encode, decode } from 'cbor-x'
import { hmac } from '@noble/hashes/hmac'
import { sha256 } from '@noble/hashes/sha256'
import type { ServerFrame, WelcomeFrame } from './proto'

/** The device's credentials: its id and HMAC key. */
export interface Creds { dev: string; key: Uint8Array }
/** A frame to the server: its type and fields (internal/proto/proto.go). */
export interface ClientFrame { t: string; v?: number; [field: string]: unknown }

const enc = new TextEncoder()

export class PheClient {
  ws: WebSocket | null
  onframe: (f: ServerFrame) => void
  onclose: () => void
  view: number

  constructor() {
    this.ws = null
    this.onframe = () => {}
    this.onclose = () => {}
    this.view = 1 // the view (tab) later requests are about
  }

  // Resolves with the welcome frame ({dev, key (pairing only), admin}); rejects with the server's error text.
  // creds = {dev, key} to authenticate, or pass code (+ name) to pair.
  connect(url: string, { creds, code, name }: { creds?: Creds; code?: string; name?: string } = {}) {
    return new Promise<WelcomeFrame>((resolve, reject) => {
      const ws = new WebSocket(url)
      ws.binaryType = 'arraybuffer'
      this.ws = ws
      let stage = 'challenge'
      let settled = false
      const fail = (e: unknown) => { if (!settled) { settled = true; reject(e instanceof Error ? e : new Error(String(e))) } }
      ws.onerror = () => fail('connection failed')
      ws.onclose = () => { fail('connection closed'); this.onclose() }
      ws.onmessage = (ev) => {
        const f = decode(new Uint8Array(ev.data)) as ServerFrame
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

  send(frame: ClientFrame) {
    if (frame.v === undefined && frame.t !== 'pair' && frame.t !== 'hello') frame.v = this.view
    if (this.ws && this.ws.readyState === 1) this.ws.send(encode(frame))
  }

  open(url: string, width = 360, height = 640) { this.send({ t: 'open', url, width, height }) }
  /** The window the page is shown in has this size (css px): the cloud lays the page out at it. */
  viewport(width: number, height: number) { this.send({ t: 'viewport', width, height }) }
  navigate(url: string) { this.send({ t: 'navigate', url }) }
  action(a: string) { this.send({ t: 'navigate', action: a }) }
  tap(layer: number, x: number, y: number, sy: number) { this.send({ t: 'input', kind: 'tap', layer, x, y, sy }) }
  scroll(y: number) { this.send({ t: 'input', kind: 'scroll', y }) }
  text(id: number, text: string, replace?: boolean) { this.send({ t: 'input', kind: 'text', id, text, replace: !!replace }) }
  key(name: string) { this.send({ t: 'input', kind: 'key', text: name }) }
  select(id: number, index: number) { this.send({ t: 'input', kind: 'select', id, index }) }
  dialogReply(id: number, ok: boolean, text?: string, pass?: string) { this.send({ t: 'dialogreply', id, ok, text: text || '', pass: pass || '' }) }
  settings(q: number, gray: boolean) { this.send({ t: 'settings', q, gray }) }
  devices() { this.send({ t: 'devices' }) }
  revoke(id: string) { this.send({ t: 'revoke', id }) }
  tab(action: string, v?: number) { this.send({ t: 'tab', action, v: v || 0 }) }
  zoom(pct: number) { this.send({ t: 'zoom', pct }) }

  close() { if (this.ws) { this.ws.onclose = null; this.ws.close() } this.ws = null }
}
