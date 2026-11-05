// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Connections → Messaging through the real UI: make a SIP login (its password shown once), add rules, reorder and save them, choose
// who gets SMS, and find all of it again after a reload; a rule without a prefix is refused.
//   BASE=http://localhost:8080 CODE=<admin code> OUT=/out node --experimental-strip-types routing.ts
import { chromium } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://localhost:8080'
const OUT = process.env.OUT || '.'
const { CODE } = process.env
assert(CODE, 'CODE is required')

const browser = await chromium.launch()
const page = await (await browser.newContext({ bypassCSP: true, viewport: { width: 900, height: 1400 } })).newPage()
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
const t0 = Date.now()
const step = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)
const shot = async (name: string) => {
  for (const scheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: scheme })
    await page.waitForTimeout(250)
    await page.locator('[data-test=routing]').screenshot({ path: `${OUT}/routing-${scheme}-${name}.png` })
  }
  await page.emulateMedia({ colorScheme: 'light' })
}

await page.goto(BASE)
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online')
await page.goto(BASE + '/#/connections')
await page.waitForSelector('[data-test=routing] [data-test=no-sip-logins]')
await page.waitForSelector('[data-test=no-routes]')
await shot('1-empty')
step('Messaging section: no logins, no rules')

await page.fill('[data-test=sip-login-name]', 'N9')
await page.click('[data-test=sip-login-add]')
await page.waitForSelector('[data-test=sip-login-made]')
const pw = (await page.textContent('[data-test=sip-login-password]'))?.trim() ?? ''
assert.match(pw, /^[0-9a-f]{24}$/, 'a password is shown once')
assert.equal(await page.locator('[data-test=sip-login]').count(), 1)
await page.fill('[data-test=sip-login-name]', 'e7')
await page.click('[data-test=sip-login-add]')
await page.waitForFunction(() => document.querySelectorAll('[data-test=sip-login]').length === 2)
step('two logins made; the password was shown')

// a rule without a prefix is refused
await page.click('[data-test=route-add]')
await page.click('[data-test=routing-save]')
await page.waitForSelector('[data-test=routing-error]')
step('a prefix rule without its prefix is refused')

await page.fill('[data-test=route-row] >> nth=0 >> [data-test=route-value]', 'home.')
await page.locator('[data-test=route-row]').nth(0).locator('[data-test=route-conn]').click()
await page.getByRole('option', { name: 'Your SIP devices' }).click()
await page.click('[data-test=route-add]')
await page.locator('[data-test=route-row]').nth(1).locator('[data-test=route-kind]').click()
await page.getByRole('option', { name: 'Phone numbers' }).click()
await page.locator('[data-test=route-row]').nth(1).locator('[data-test=route-conn]').click()
await page.getByRole('option', { name: 'SMS through your phones' }).click()
await page.locator('[data-test=route-row]').nth(1).locator('[data-test=route-up]').click()   // numbers first
await page.click('[data-test=deliver-sms]')
await page.getByRole('option', { name: 'n9' }).click()
await page.keyboard.press('Escape')
await page.click('[data-test=routing-save]')
await page.waitForSelector('[data-test=routing-saved]')
await shot('2-saved')
step('rules added, reordered and saved; SMS go to n9 only')

await page.reload()
await page.waitForFunction(() => document.querySelectorAll('[data-test=route-row]').length === 2)
const rows = await page.locator('[data-test=route-row]').allTextContents()
assert.match(rows[0], /Phone numbers/, 'numbers first after the move')
assert.match(rows[1], /Your SIP devices/)
assert.equal(await page.locator('[data-test=route-row]').nth(1).locator('input[data-test=route-value]').inputValue(), 'home.')
assert.match((await page.locator('[data-test=deliver-row]').nth(0).textContent()) ?? '', /SMS through your phones.*n9/s, 'SMS still go to n9')
assert.equal(await page.locator('[data-test=sip-login-made]').count(), 0, 'the password is not shown again')
step('after a reload: the same rules in the same order, SMS to n9, no password')

await page.locator('[data-test=sip-login-remove]').nth(1).click()
await page.waitForFunction(() => document.querySelectorAll('[data-test=sip-login]').length === 1)
step('a login removed')

assert.deepEqual(errors, [], 'no page errors')
await browser.close()
console.log('routing: all passed')
