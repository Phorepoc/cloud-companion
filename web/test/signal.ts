// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Connections → Add an account → Signal against a real signal-cli-rest-api (compose profile "signal") with nothing linked, which is
// all a test can do without a phone: "Link this device" shows the QR code from the API, Test says the number is not linked yet, and Add
// refuses it. Linking itself is checked by hand (docs/messaging.md).
//   BASE=http://localhost:8080 CODE=<admin code> SERVER=<signal-cli-rest-api URL> OUT=/out node --experimental-strip-types signal.ts
import { chromium } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://localhost:8080'
const OUT = process.env.OUT || '.'
const { CODE, SERVER } = process.env
assert(CODE && SERVER, 'CODE and SERVER are required')

const browser = await chromium.launch()
// the app's own CSP stays on: the QR code is a data: image, which it must allow
const page = await (await browser.newContext({ bypassCSP: false, viewport: { width: 900, height: 1400 } })).newPage()
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
page.on('console', (m) => { if (m.type() === 'error') errors.push(m.text()) })
const t0 = Date.now()
const step = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)
const shot = async (name: string) => {
  for (const scheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: scheme })
    await page.waitForTimeout(250)
    await page.screenshot({ path: `${OUT}/signal-${scheme}-${name}.png` })
  }
  await page.emulateMedia({ colorScheme: 'light' })
}

await page.goto(BASE)
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online')
await page.goto(BASE + '/#/connections')
await page.waitForSelector('[data-test=no-accounts]')

await page.click('[data-test=add-account]')
await page.click('[data-test=f-kind]')
await page.getByRole('option', { name: 'Signal', exact: true }).click()
await page.fill('[data-test=f-name]', 'My Signal')
await page.fill('[data-test=f-url]', SERVER)
await page.click('[data-test=f-signal-link]')
const img = page.locator('[data-test=f-signal-qr] img')
await img.waitFor()
await page.waitForFunction(() => { const i = document.querySelector('[data-test=f-signal-qr] img') as HTMLImageElement | null; return !!i && i.complete && i.naturalWidth > 50 })
await shot('1-qr')
step('Link this device shows the QR code from signal-cli-rest-api')

await page.fill('[data-test=f-user]', '+306900000001')
await page.click('[data-test=f-test]')
await page.waitForSelector('[data-test=test-result]')
assert.match((await page.textContent('[data-test=test-result]')) ?? '', /not linked yet/)
step('Test: the number is not linked yet')

await page.click('[data-test=f-add]')
await page.locator('[data-test=test-result]', { hasText: /the test failed: .*not linked yet/ }).waitFor()
await shot('2-refused')
await page.keyboard.press('Escape')
await page.waitForTimeout(300)
assert.equal(await page.locator('[data-test=account-row]').count(), 0, 'an unlinked account was added')
step('Add refuses an account that is not linked yet')

assert.deepEqual(errors.filter((e) => !/Failed to load resource/.test(e)), [], 'no page errors')
await browser.close()
console.log('signal: all passed')
