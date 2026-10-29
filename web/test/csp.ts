// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
// The app must work under its own Content-Security-Policy: pair as admin, open every section, and fail on any CSP violation or page error. (The other browser tests run with the policy
// bypassed because Playwright evaluates code inside the page; this one runs with it enforced.) Also checks the security headers.
import { chromium } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE, CODE = process.env.CODE
assert(BASE && CODE, 'BASE and CODE (an admin pairing code) are required')
const browser = await chromium.launch()
const ctx = await browser.newContext({ viewport: { width: 1100, height: 800 } })
const page = await ctx.newPage()
const bad: string[] = []
page.on('pageerror', (e) => bad.push('pageerror: ' + e.message))
page.on('console', (m) => { if (/content security policy|refused to/i.test(m.text())) bad.push('csp: ' + m.text()) })

const r = await page.goto(BASE)
assert(r, 'the page loads')
const h = r.headers()
assert(/frame-ancestors 'none'/.test(h['content-security-policy'] || ''), 'CSP header missing')
assert.equal(h['x-content-type-options'], 'nosniff')
assert.equal(h['x-frame-options'], 'DENY')
assert.equal(h['referrer-policy'], 'no-referrer')
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
await page.waitForSelector('[data-test=nav-settings]', { timeout: 30000 })
for (const sec of ['browse', 'chats', 'mail', 'calendar', 'contacts', 'files', 'android', 'connections', 'catalogue', 'devices', 'settings']) {
  await page.goto(BASE + '/#/' + sec)
  await page.waitForTimeout(700)
  console.log('section', sec, 'ok')
}
// the desktop view: windows, dock tiles (inline custom properties), the launcher and the self-hosted fonts, under the policy
await page.goto(BASE + '/#/desktop?open=settings')
await page.waitForSelector('[data-test=win-settings]')
await page.click('[data-test=dock-mail]')
await page.click('[data-test=dock-launcher]')
await page.keyboard.type('cal')
await page.keyboard.press('Enter')
await page.waitForSelector('[data-test=win-calendar]')
assert(await page.evaluate(() => document.fonts.check('300 16px "HD Fira Sans"')), 'the bundled font loads under the policy')
await page.click('[data-test=clock]')
await page.waitForSelector('[data-test=events]')
await page.waitForTimeout(700) // its calendar, mail and chats requests
console.log('desktop ok')
assert.deepEqual(bad, [], 'CSP violations or page errors')
await browser.close()
console.log('CSP: PASS (security headers present, every section and the desktop load under the policy)')
