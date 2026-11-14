// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only
//
// Connections → Add an account → SIP service against a SIP service (the fake Asterisk, internal/fakes/sip): a wrong password fails the test, the right one passes,
// the account registers (Connected) and shows up as a connection the Messaging rules can send to; removed, it is gone from there.
//   BASE=http://localhost:8080 CODE=<admin code> SECRET=<asterisk secret> OUT=/out node --experimental-strip-types sipservice.ts
import { chromium } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://localhost:8080'
const OUT = process.env.OUT || '.'
const { CODE, SECRET } = process.env
assert(CODE && SECRET, 'CODE and SECRET are required')

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
    await page.screenshot({ path: `${OUT}/sipservice-${scheme}-${name}.png` })
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
await page.getByRole('option', { name: /SIP service/ }).click()
await page.fill('[data-test=f-name]', 'Home PBX')
await page.fill('[data-test=f-sip-server]', 'asterisk')
await page.fill('[data-test=f-user]', 'cloud-companion')
await page.fill('[data-test=f-secret]', 'definitely-wrong')
await page.click('[data-test=f-test]')
await page.waitForSelector('[data-test=test-result]')
assert.match((await page.textContent('[data-test=test-result]')) ?? '', /login refused/i)
step('a wrong password fails the test')
await page.fill('[data-test=f-secret]', SECRET)
await page.click('[data-test=f-test]')
await page.waitForFunction(() => /login works as cloud-companion@asterisk/.test(document.querySelector('[data-test=test-result]')?.textContent || ''))
await shot('1-dialog')
await page.click('[data-test=f-add]')
await page.waitForSelector('[data-test=account-row]')
step('the right password passes; account added')

// the pool picks it up within 10 s, registers and offers it to the rules
for (let i = 0; ; i++) {
  await page.reload()
  await page.waitForSelector('[data-test=deliver-row]')
  if (/Home PBX \(SIP\)/.test((await page.textContent('[data-test=routing]')) ?? '')) break
  assert(i < 20, 'no connection for the account')
  await page.waitForTimeout(1500)
}
await page.waitForTimeout(1500) // its first registration
await page.reload()
await page.waitForSelector('[data-test=account-row]')
assert.match((await page.textContent('[data-test=account-status]')) ?? '', /Connected/, 'registered')
assert.match((await page.textContent('[data-test=account-row]')) ?? '', /SIP service/)
step('registered with the SIP service: Connected, and a connection for the rules')

await page.click('[data-test=route-add]')
await page.fill('[data-test=route-row] >> nth=0 >> input[data-test=route-value]', 'pbx.')
await page.locator('[data-test=route-row]').nth(0).locator('[data-test=route-conn]').click()
await page.getByRole('option', { name: 'Home PBX (SIP)' }).click()
await page.click('[data-test=routing-save]')
await page.waitForSelector('[data-test=routing-saved]')
await page.locator('[data-test=routing]').scrollIntoViewIfNeeded()
await shot('2-rule')
step('a rule sends pbx. addresses to it')

await page.click('[data-test=account-remove]')
await page.click('[data-test=remove-confirm]')
await page.waitForSelector('[data-test=no-accounts]')
for (let i = 0; ; i++) {
  await page.reload()
  await page.waitForSelector('[data-test=deliver-row]')
  if (!/Home PBX \(SIP\)/.test((await page.textContent('[data-test=routing]')) ?? '')) break
  assert(i < 20, 'the connection stayed after the account was removed')
  await page.waitForTimeout(1500)
}
step('removed: no longer a connection')

assert.deepEqual(errors, [], 'no page errors')
await browser.close()
console.log('sipservice: all passed')
