// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only
//
// Connections → Add an account → Telegram through the real UI, without reaching Telegram (a real sign-in needs your phone and is
// checked by hand, docs/messaging.md): the sign-in button waits for the API ID and hash; Test and Add refuse an account that is not
// signed in (the real server); then the sign-in's states as the server reports them (the browser answers /phe/telegram/login*):
// a QR code that changes, the two-step password asked and sent, signed in, and Test then sends the signed-in user's ID.
//   BASE=http://localhost:8080 CODE=<admin code> OUT=/out node --experimental-strip-types telegram.ts
import { chromium } from 'playwright'
import assert from 'node:assert/strict'
import { deflateSync } from 'node:zlib'

const BASE = process.env.BASE || 'http://localhost:8080'
const OUT = process.env.OUT || '.'
const { CODE } = process.env
assert(CODE, 'CODE is required')

const browser = await chromium.launch()
const page = await (await browser.newContext({ viewport: { width: 900, height: 1400 }, serviceWorkers: 'block' })).newPage() // the app's CSP on: data: images
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
const t0 = Date.now()
const step = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)
const shot = async (name: string) => {
  for (const scheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: scheme })
    await page.waitForTimeout(250)
    await page.screenshot({ path: `${OUT}/telegram-${scheme}-${name}.png` })
  }
  await page.emulateMedia({ colorScheme: 'light' })
}

// a small real PNG (a 21×21 checkerboard, as a QR code's size), so the dialog shows an image that decodes
function png(seed: number): string {
  const w = 21, crc = (b: Buffer) => { let c = ~0; for (const x of b) { c ^= x; for (let k = 0; k < 8; k++) c = (c >>> 1) ^ (0xedb88320 & -(c & 1)) } return ~c >>> 0 }
  const chunk = (t: string, d: Buffer) => { const l = Buffer.alloc(4); l.writeUInt32BE(d.length); const td = Buffer.concat([Buffer.from(t), d]); const c = Buffer.alloc(4); c.writeUInt32BE(crc(td)); return Buffer.concat([l, td, c]) }
  const hdr = Buffer.alloc(13); hdr.writeUInt32BE(w, 0); hdr.writeUInt32BE(w, 4); hdr[8] = 8; hdr[9] = 0
  const rows = Buffer.concat(Array.from({ length: w }, (_, y) => Buffer.concat([Buffer.from([0]), Buffer.from(Array.from({ length: w }, (_, x) => ((x + y + seed) % 2 ? 0 : 255)))])))
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', hdr), chunk('IDAT', deflateSync(rows)), chunk('IEND', Buffer.alloc(0))]).toString('base64')
}

await page.goto(BASE)
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
for (let i = 0; !(await page.evaluate(() => window.__phe?.state.status === 'online')); i++) { assert(i < 80, 'not online'); await page.waitForTimeout(250) } // the app's CSP on: no waitForFunction
await page.goto(BASE + '/#/connections')
await page.waitForSelector('[data-test=no-accounts]')

await page.click('[data-test=add-account]')
await page.click('[data-test=f-kind]')
await page.getByRole('option', { name: 'Telegram', exact: true }).click()
await page.fill('[data-test=f-name]', 'My Telegram')
assert(await page.locator('[data-test=f-tg-signin]').isDisabled(), 'sign-in possible without an API ID and hash')
await page.fill('[data-test=f-tg-api-id]', '12345')
await page.fill('[data-test=f-secret]', '0123456789abcdef0123456789abcdef')
assert(!(await page.locator('[data-test=f-tg-signin]').isDisabled()), 'sign-in still off with an API ID and hash')
await page.click('[data-test=f-test]')
await page.locator('[data-test=test-result]', { hasText: /not signed in yet/ }).waitFor()
await page.click('[data-test=f-add]')
await page.locator('[data-test=test-result]', { hasText: /the test failed: .*not signed in yet/ }).waitFor()
step('the sign-in waits for the API ID and hash; Test and Add refuse an account that is not signed in')

// the sign-in, as the server reports it
let polls = 0, password = '', started: Record<string, unknown> | null = null
await page.route((u) => u.pathname === '/phe/telegram/login', async (route) => {
  if (route.request().method() === 'POST') { started = route.request().postDataJSON(); return route.fulfill({ json: { id: 'abc' } }) }
  polls++
  const state = password === 'secret-2fa' ? { state: 'done', user_id: 4242, name: 'Me Myself' }
    : polls < 3 ? { state: 'waiting', png: png(0) } : polls < 5 ? { state: 'waiting', png: png(1) } : { state: 'password', error: password ? 'wrong password, try again' : '' }
  return route.fulfill({ json: state })
})
await page.route('**/phe/telegram/login/password', async (route) => { password = route.request().postDataJSON().password; return route.fulfill({ json: { ok: true } }) })
await page.click('[data-test=f-tg-signin]')
const img = page.locator('[data-test=f-tg-qr] img')
await img.waitFor()
assert.equal(await page.locator('[data-test=test-result]').count(), 0, 'the earlier test result still shows')
assert.deepEqual(started, { api_id: 12345, api_hash: '0123456789abcdef0123456789abcdef' })
const first = await img.getAttribute('src')
for (let i = 0; (await img.getAttribute('src')) === first; i++) { assert(i < 40, 'the QR code did not change'); await page.waitForTimeout(250) }
assert.equal(await img.evaluate((i: HTMLImageElement) => i.naturalWidth), 21, 'the QR code decodes')
await shot('1-qr')
step('the QR code shows and changes as the server renews it')

await page.waitForSelector('[data-test=f-tg-password]')
await page.fill('[data-test=f-tg-password]', 'wrong')
await page.click('[data-test=f-tg-password-send]')
await page.locator('[data-test=f-tg-error]', { hasText: 'wrong password' }).waitFor()
await page.fill('[data-test=f-tg-password]', 'secret-2fa')
await page.keyboard.press('Enter')
await page.locator('[data-test=f-tg-done]', { hasText: 'Signed in as Me Myself' }).waitFor()
await shot('2-done')
step('the two-step password: a wrong one is said so, the right one signs in')

let tested: Record<string, unknown> | null = null
await page.route('**/phe/connections/test', async (route) => { tested = route.request().postDataJSON(); return route.fulfill({ json: { ok: true, detail: 'signed in as Me Myself' } }) })
await page.click('[data-test=f-test]')
await page.locator('[data-test=test-result]', { hasText: 'signed in as Me Myself' }).waitFor()
assert.deepEqual(JSON.parse(String(tested!.config)), { api_id: 12345, user_id: 4242 })
assert.equal(tested!.secret, '0123456789abcdef0123456789abcdef')
step('Test sends the API ID, the signed-in user and the hash')

const before = polls
await page.keyboard.press('Escape')
await page.waitForTimeout(3500)
assert.equal(polls, before, 'the closed dialog still asks about its sign-in')
step('closing the dialog stops asking')

assert.deepEqual(errors, [], 'no page errors')
await browser.close()
console.log('telegram: all passed')
