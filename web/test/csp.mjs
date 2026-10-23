// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
// The app must work under its own Content-Security-Policy: pair as admin, open every section, and fail on any CSP violation or page error. (The other browser tests run with the policy
// bypassed because Playwright evaluates code inside the page; this one runs with it enforced.) Also checks the security headers.
import { chromium } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE, CODE = process.env.CODE
const browser = await chromium.launch()
const ctx = await browser.newContext({ viewport: { width: 1100, height: 800 } })
const page = await ctx.newPage()
const bad = []
page.on('pageerror', (e) => bad.push('pageerror: ' + e.message))
page.on('console', (m) => { if (/content security policy|refused to/i.test(m.text())) bad.push('csp: ' + m.text()) })

const r = await page.goto(BASE)
const h = r.headers()
assert(/frame-ancestors 'none'/.test(h['content-security-policy'] || ''), 'CSP header missing')
assert.equal(h['x-content-type-options'], 'nosniff')
assert.equal(h['x-frame-options'], 'DENY')
assert.equal(h['referrer-policy'], 'no-referrer')
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
await page.waitForSelector('[data-test=nav-settings]', { timeout: 30000 })
for (const sec of ['browse', 'chats', 'mail', 'calendar', 'contacts', 'files', 'android', 'connections', 'devices', 'settings']) {
  await page.goto(BASE + '/#/' + sec)
  await page.waitForTimeout(700)
  console.log('section', sec, 'ok')
}
assert.deepEqual(bad, [], 'CSP violations or page errors')
await browser.close()
console.log('CSP: PASS (security headers present, every section loads under the policy)')
