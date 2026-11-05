// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Connections → Add an account → XMPP against a throwaway Prosody (user cc@localhost, made by xmpp.sh): a wrong password fails the
// test, the right one passes, the account signs in (Connected) and shows up as a connection the Messaging rules can send to;
// removed, it is gone from there.
//   BASE=http://localhost:8080 CODE=<admin code> SERVER=<host of the XMPP server> OUT=/out node --experimental-strip-types xmpp.ts
import { chromium } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://localhost:8080'
const OUT = process.env.OUT || '.'
const { CODE, SERVER } = process.env
assert(CODE && SERVER, 'CODE and SERVER are required')

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
    await page.screenshot({ path: `${OUT}/xmpp-${scheme}-${name}.png` })
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
await page.getByRole('option', { name: 'XMPP (Jabber)' }).click()
await page.fill('[data-test=f-name]', 'My XMPP')
await page.fill('[data-test=f-user]', 'cc@localhost')
await page.fill('[data-test=f-xmpp-server]', SERVER)
await page.click('[data-test=f-insecure]')
await page.fill('[data-test=f-secret]', 'definitely-wrong')
await page.click('[data-test=f-test]')
await page.waitForSelector('[data-test=test-result]')
assert.match((await page.textContent('[data-test=test-result]')) ?? '', /login refused/i)
step('a wrong password fails the test')
await page.fill('[data-test=f-secret]', 'pw-cc')
await page.click('[data-test=f-test]')
await page.waitForFunction(() => /login works as cc@localhost/.test(document.querySelector('[data-test=test-result]')?.textContent || ''))
await shot('1-dialog')
await page.click('[data-test=f-add]')
await page.waitForSelector('[data-test=account-row]')
step('the right password passes; account added')

for (let i = 0; ; i++) {
  await page.reload()
  await page.waitForSelector('[data-test=deliver-row]')
  if (/My XMPP \(XMPP\)/.test((await page.textContent('[data-test=routing]')) ?? '')) break
  assert(i < 20, 'no connection for the account: ' + (await page.textContent('[data-test=account-row]')))
  await page.waitForTimeout(1500)
}
await page.waitForTimeout(1500)
await page.reload()
await page.waitForSelector('[data-test=account-row]')
assert.match((await page.textContent('[data-test=account-status]')) ?? '', /Connected/, 'signed in: ' + (await page.textContent('[data-test=account-row]')))
assert.match((await page.textContent('[data-test=account-row]')) ?? '', /XMPP/)
step('signed in: Connected, and a connection for the rules')

await page.click('[data-test=route-add]')
await page.fill('[data-test=route-row] >> nth=0 >> input[data-test=route-value]', 'xm.')
await page.locator('[data-test=route-row]').nth(0).locator('[data-test=route-conn]').click()
await page.getByRole('option', { name: 'My XMPP (XMPP)' }).click()
await page.click('[data-test=routing-save]')
await page.waitForSelector('[data-test=routing-saved]')
await page.locator('[data-test=routing]').scrollIntoViewIfNeeded()
await shot('2-rule')
step('a rule sends xm. addresses to it')

await page.click('[data-test=account-remove]')
await page.click('[data-test=remove-confirm]')
await page.waitForSelector('[data-test=no-accounts]')
for (let i = 0; ; i++) {
  await page.reload()
  await page.waitForSelector('[data-test=deliver-row]')
  if (!/My XMPP \(XMPP\)/.test((await page.textContent('[data-test=routing]')) ?? '')) break
  assert(i < 20, 'the connection stayed after the account was removed')
  await page.waitForTimeout(1500)
}
step('removed: no longer a connection')

assert.deepEqual(errors, [], 'no page errors')
await browser.close()
console.log('xmpp: all passed')
