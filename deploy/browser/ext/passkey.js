// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only
// Passkeys: Firefox here has no authenticator of its own (a security key would have to be plugged into the server). A page's
// navigator.credentials.create/get for a passkey goes to the device you are using, through Cloud Companion, whose passkey
// provider (the OS, a hardware key or a password manager there) answers. Cloud Companion keeps no keys.
// (in a block: the content scripts share one scope)
{
const b64 = (buf) => btoa(String.fromCharCode(...new Uint8Array(buf instanceof ArrayBuffer ? buf : buf.buffer.slice(buf.byteOffset, buf.byteOffset + buf.byteLength)))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
const unb64 = (s) => Uint8Array.from(atob(s.replace(/-/g, '+').replace(/_/g, '/')), (c) => c.charCodeAt(0)).buffer
// the options as JSON (the same shape as PublicKeyCredential.parseCreationOptionsFromJSON takes)
function plain(v) {
  if (v instanceof ArrayBuffer || ArrayBuffer.isView(v)) return b64(v)
  if (Array.isArray(v)) return v.map(plain)
  if (v && typeof v === 'object') { const o = {}; for (const k of Object.keys(v)) if (k !== 'signal') o[k] = plain(v[k]); return o }
  return v
}
const page = window.wrappedJSObject
const waiting = new Map()
let n = 0
function ask(kind, opts) {
  let options
  try { options = plain(opts.publicKey) } catch (e) { return page.Promise.reject(new page.DOMException('passkey options: ' + e, 'NotSupportedError')) }
  return new page.Promise(exportFunction((ok, fail) => {
    const id = Date.now() + '-' + ++n
    waiting.set(id, { ok, fail, kind })
    browser.runtime.sendMessage({ passkey: { id, kind, origin: location.origin, options } })
  }, page))
}
// what the page gets back: a PublicKeyCredential-shaped object in the page's world
function credential(r, kind) {
  const resp = { clientDataJSON: unb64(r.response.clientDataJSON) }
  if (kind === 'create') {
    resp.attestationObject = unb64(r.response.attestationObject)
    resp.getTransports = () => r.response.transports || ['internal', 'hybrid']
    resp.getAuthenticatorData = () => unb64(r.response.authenticatorData || '')
    resp.getPublicKey = () => (r.response.publicKey ? unb64(r.response.publicKey) : null)
    resp.getPublicKeyAlgorithm = () => r.response.publicKeyAlgorithm ?? -7
  } else {
    resp.authenticatorData = unb64(r.response.authenticatorData)
    resp.signature = unb64(r.response.signature)
    resp.userHandle = r.response.userHandle ? unb64(r.response.userHandle) : null
  }
  const c = { id: r.id, rawId: unb64(r.rawId || r.id), type: 'public-key', authenticatorAttachment: r.authenticatorAttachment || 'cross-platform', response: resp,
    getClientExtensionResults: () => r.clientExtensionResults || {}, toJSON: () => r }
  return cloneInto(c, page, { cloneFunctions: true })
}
// on the interface's prototype, as the page sees it (the same way pick.js replaces input.click)
const C = page.CredentialsContainer.prototype
const orig = { create: C.create, get: C.get }
// the page's own objects, not the content script's view of them (an Xray), so its options read as the page wrote them
const own = (o) => (o && o.wrappedJSObject) || o
C.create = exportFunction(function (opts) { const o = own(opts); return o && o.publicKey ? ask('create', o) : orig.create.call(own(this), o) }, page)
C.get = exportFunction(function (opts) { const o = own(opts); return o && o.publicKey ? ask('get', o) : orig.get.call(own(this), o) }, page)
// a platform authenticator is there (the device's), so sites offer passkeys
if (page.PublicKeyCredential) page.PublicKeyCredential.isUserVerifyingPlatformAuthenticatorAvailable = exportFunction(() => page.Promise.resolve(true), page)
browser.runtime.onMessage.addListener((m) => {
  if (!m.passkeyDone) return
  const w = waiting.get(m.passkeyDone.id)
  if (!w) return
  waiting.delete(m.passkeyDone.id)
  const d = m.passkeyDone
  if (d.result) w.ok(credential(d.result, w.kind))
  else w.fail(new page.DOMException(d.message || 'The request was not allowed.', d.error || 'NotAllowedError'))
})
}
