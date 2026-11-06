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
await page.waitForSelector('[data-test=classic] .x-cl__apps', { timeout: 30000 })
for (const sec of ['browse', 'chats', 'mail', 'calendar', 'contacts', 'files', 'music', 'android', 'connections', 'catalogue', 'devices', 'settings']) {
  await page.goto(BASE + '/#/' + sec)
  await page.waitForTimeout(700)
  console.log('section', sec, 'ok')
}
// the desktop view: windows, dock tiles (inline custom properties), the launcher and the self-hosted fonts, under the policy
await page.goto(BASE + '/#/desktop?open=settings')
await page.waitForSelector('.x-win[data-win=settings]')
await page.click('.x-shell-dock button[aria-label=Mail]')
await page.click('.x-shell-dock button[aria-label=Overview]')
await page.keyboard.type('cal')
await page.keyboard.press('Enter')
await page.waitForSelector('.x-win[data-win=calendar]')
assert(await page.evaluate(() => document.fonts.check('300 16px "HD Fira Sans"')), 'the bundled font loads under the policy')
await page.click('.x-shell__clock')
await page.waitForSelector('[data-test=events]')
await page.waitForTimeout(700) // its calendar, mail and chats requests
console.log('desktop ok')

// the service worker (compiled from src/sw.ts) runs under the policy and keeps the app shell; offline, a page it kept still opens
// and one it never saw is a plain network error, not a broken response
const sw = await page.evaluate(async () => { const r = await navigator.serviceWorker.ready; return r.active?.scriptURL || '' })
assert.match(sw, /\/sw\.js$/, 'the service worker is active')
await page.reload() // now it is in charge of the page's requests
await page.waitForSelector('.x-shell-dock button[aria-label=Overview]')
await ctx.setOffline(true)
await ctx.route('**/*', (r) => r.abort('internetdisconnected')) // the service worker's own requests too (PW_EXPERIMENTAL_SERVICE_WORKER_NETWORK_EVENTS, csp.sh)
const offline = await page.evaluate(async () => {
  const kept = await fetch('./').then((r) => r.status, () => 'network error')
  const never = await fetch('./never-seen-' + Date.now()).then((r) => r.status, () => 'network error')
  return { kept, never }
})
await ctx.unroute('**/*')
await ctx.setOffline(false)
assert.deepEqual(offline, { kept: 200, never: 'network error' }, 'offline answers from the service worker')
console.log('service worker ok')
assert.deepEqual(bad, [], 'CSP violations or page errors')
await browser.close()
console.log('CSP: PASS (security headers present, every section, the desktop and the service worker work under the policy)')
