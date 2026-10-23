// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// phecloud behind Traefik: passkeys must get the PUBLIC host as relying party id and origin (https://phecloud.test), and a client that
// reaches phecloud directly must not be able to choose them with forwarded headers.
//   VIA=https://phecloud.test DIRECT=http://phe-cloud-px:8080 TRAEFIK=phe-traefik-px CODE=<admin code> OUT=/out node proxy.mjs
import { chromium } from 'playwright'
import assert from 'node:assert/strict'
import http from 'node:http'

const { VIA, DIRECT, TRAEFIK, CODE } = process.env
const OUT = process.env.OUT || '.'
assert(VIA && DIRECT && TRAEFIK && CODE, 'VIA, DIRECT, TRAEFIK and CODE are required')
const t0 = Date.now()
const step = (s) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)

// 1. direct access from an untrusted peer: forwarded headers change nothing
const probe = (hdr) => new Promise((resolve, reject) => {
  const u = new URL(DIRECT)
  const body = JSON.stringify({ code: CODE })
  const rq = http.request({ host: u.hostname, port: u.port, path: '/passkey/register/begin', method: 'POST', headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(body), Host: 'direct.example:8080', ...hdr } }, (res) => {
    let d = ''
    res.on('data', (c) => (d += c))
    res.on('end', () => { const j = JSON.parse(d); j.options ? resolve(j.options.publicKey.rp.id) : reject(new Error('begin failed: ' + res.statusCode + ' ' + d)) })
  })
  rq.on('error', reject)
  rq.end(body)
})
const direct = 'direct.example' // the Host header sent (a dotted name: a relying party id must be a domain)
assert.equal(await probe({}), direct)
assert.equal(await probe({ 'X-Forwarded-Host': 'evil.example', 'X-Forwarded-Proto': 'https', 'X-Forwarded-For': '1.2.3.4', Forwarded: 'host=evil.example;proto=https', 'CF-Connecting-IP': '5.6.7.8' }), direct)
step('an untrusted peer cannot choose the relying party id with X-Forwarded-Host / Forwarded')

// 2. through Traefik: the browser uses https://phecloud.test (name resolved to the Traefik container)
const browser = await chromium.launch({ args: [`--host-resolver-rules=MAP phecloud.test ${TRAEFIK}`, '--ignore-certificate-errors'] })
const ctx = await browser.newContext({ bypassCSP: true, viewport: { width: 900, height: 1000 }, ignoreHTTPSErrors: true })
const page = await ctx.newPage()
const log = []
page.on('pageerror', (e) => log.push('pageerror: ' + e.message))
const cdp = await ctx.newCDPSession(page)
await cdp.send('WebAuthn.enable')
await cdp.send('WebAuthn.addVirtualAuthenticator', { options: { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true } })

await page.goto(VIA)
await page.waitForSelector('[data-test=passkey-register]')
assert.equal(await page.evaluate(() => window.isSecureContext), true)
assert.equal(await page.locator('[data-test=passkey-blocker]').count(), 0, 'a domain name over https: no passkey blocker')
const rpid = await page.evaluate(async (code) => (await (await fetch('/passkey/register/begin', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ code }) })).json()).options.publicKey.rp.id, CODE)
assert.equal(rpid, 'phecloud.test')
step('through Traefik the relying party id is the public name: ' + rpid)

await page.fill('[data-test=code]', CODE)
await page.click('[data-test=passkey-register]')
await page.waitForSelector('[data-test=recovery-dialog]')
await page.click('[data-test=recovery-saved]')
await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online')
step('passkey registered through the proxy (wss over Traefik works)')
await page.evaluate(() => window.__phe.forget())
await page.goto(VIA)
await page.click('[data-test=passkey-login]')
await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online')
step('passkey sign-in through the proxy')
await page.screenshot({ path: `${OUT}/proxy-traefik.png` })
assert.deepEqual(log, [], 'page errors: ' + log.join('; '))
await browser.close()
console.log('PROXY PASS')
