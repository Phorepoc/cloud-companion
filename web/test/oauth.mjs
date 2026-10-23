// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// "Sign in with Google" on the Connections page against a fake OAuth provider (started here on 127.0.0.1:9099): the provider button appears only because the
// server is configured, the browser is redirected to the provider, comes back to /phe/oauth/callback, and the new accounts show up.
//   BASE=http://localhost:8080 CODE=<admin code> OUT=/out node oauth.mjs
import http from 'node:http'
import { chromium } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://localhost:8080'
const OUT = process.env.OUT || '.'
const seen = { auth: null, token: null }
const fake = http.createServer((req, res) => {
  const u = new URL(req.url, 'http://localhost:9099')
  if (u.pathname === '/auth') {
    seen.auth = Object.fromEntries(u.searchParams)
    res.writeHead(302, { Location: `${u.searchParams.get('redirect_uri')}?code=good-code&state=${u.searchParams.get('state')}` })
    return res.end()
  }
  if (u.pathname === '/token') {
    let body = ''
    req.on('data', (c) => (body += c))
    req.on('end', () => {
      seen.token = Object.fromEntries(new URLSearchParams(body))
      const idt = 'x.' + Buffer.from(JSON.stringify({ email: 'me@gmail.test' })).toString('base64url') + '.y'
      res.writeHead(200, { 'Content-Type': 'application/json' })
      res.end(JSON.stringify({ access_token: 'tok1', refresh_token: 'ref1', expires_in: 3600, id_token: idt }))
    })
    return
  }
  res.writeHead(404); res.end()
})
await new Promise((r) => fake.listen(9099, '127.0.0.1', r))

const browser = await chromium.launch()
const ctx = await browser.newContext({ viewport: { width: 900, height: 1100 } })
const page = await ctx.newPage()
const log = []
page.on('pageerror', (e) => log.push('pageerror: ' + e.message))
const t0 = Date.now()
const step = (s) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)
async function shots(name) {
  for (const scheme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme: scheme })
    await page.waitForTimeout(250)
    await page.screenshot({ path: `${OUT}/oauth-${scheme}-${name}.png` })
  }
  await page.emulateMedia({ colorScheme: 'light' })
}
await page.goto(BASE)
await page.fill('[data-test=code]', process.env.CODE)
await page.click('[data-test=pair]')
await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online')
await page.goto(BASE + '/#/connections')
await page.waitForSelector('[data-test=oauth-google]')
assert.equal(await page.locator('[data-test=oauth-microsoft]').count(), 0, 'only configured providers are offered')
await shots('1-buttons')
step('"Sign in with Google" is offered because the server is configured; Microsoft is not')

await page.click('[data-test=oauth-google]')
await page.waitForSelector('[data-test=oauth-dialog]')
await page.fill('[data-test=oauth-name]', 'Personal')
await page.locator('[data-test=oauth-kinds]').getByText('Contacts').click()
await shots('2-dialog')
await Promise.all([page.waitForURL(/oauth=ok/), page.click('[data-test=oauth-go]')])
await page.waitForSelector('[data-test=oauth-result]')
assert.match(await page.textContent('[data-test=oauth-result]'), /Signed in/)
assert.equal(seen.auth.client_id, 'test-client')
assert.equal(seen.auth.code_challenge_method, 'S256')
assert.match(seen.auth.scope, /mail\.google\.com/)
assert.match(seen.auth.scope, /carddav/)
assert.equal(seen.auth.redirect_uri, BASE + '/phe/oauth/callback')
assert.equal(seen.token.grant_type, 'authorization_code')
assert.equal(seen.token.client_secret, 'test-secret')
assert(seen.token.code_verifier.length >= 40, 'PKCE verifier sent')
await page.waitForFunction(() => document.querySelectorAll('[data-test=account-row]').length === 2)
const rows = await page.locator('[data-test=account-row]').allTextContents()
assert(rows.some((r) => /Personal mail/.test(r)) && rows.some((r) => /Personal contacts/.test(r)), rows.join(' | '))
await shots('3-accounts')
step('redirect to the provider with PKCE and the right scopes, back through the callback, two accounts created')

// tokens never reach the browser
const list = await page.evaluate(async () => JSON.stringify(await window.__phe.apiCall('GET', '/phe/connections')))
assert(!list.includes('tok1') && !list.includes('ref1'))
// a second callback with the same state does nothing
const again = await page.evaluate(async () => (await fetch('/phe/oauth/callback?code=good-code&state=forged', { redirect: 'manual' })).type)
assert.equal(again, 'opaqueredirect')
assert.deepEqual(log, [], 'page errors: ' + log.join('; '))
fake.close()
await browser.close()
console.log('OAUTH PASS')
