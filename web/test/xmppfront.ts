// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// The XMPP front, first half: two phone logins made through the real Messaging section, which shows each one's XMPP address.
// Their passwords go to OUT/xmppfront-logins.json for the Jabber clients of xmppfront.py.
//   BASE=http://localhost:8080 CODE=<admin code> OUT=/out node --experimental-strip-types xmppfront.ts
import { chromium } from 'playwright'
import assert from 'node:assert/strict'
import { writeFileSync } from 'node:fs'

const BASE = process.env.BASE || 'http://localhost:8080'
const OUT = process.env.OUT || '.'
const { CODE } = process.env
assert(CODE, 'CODE is required')

const browser = await chromium.launch()
const page = await (await browser.newContext({ bypassCSP: true, viewport: { width: 900, height: 1400 } })).newPage()
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))

await page.goto(BASE)
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online')
await page.goto(BASE + '/#/connections')
await page.waitForSelector('[data-test=routing] [data-test=no-sip-logins]')

const logins: Record<string, string> = {}
for (const name of ['n9', 'e7']) {
  await page.fill('[data-test=sip-login-name]', name)
  await page.click('[data-test=sip-login-add]')
  await page.waitForFunction((n) => document.querySelector('[data-test=sip-login-jid]')?.textContent === n + '@cloud-companion', name)
  logins[name] = (await page.textContent('[data-test=sip-login-password]'))?.trim() ?? ''
}
await page.locator('[data-test=routing]').screenshot({ path: `${OUT}/xmppfront-logins.png` })
writeFileSync(`${OUT}/xmppfront-logins.json`, JSON.stringify(logins))
assert.deepEqual(errors, [], 'no page errors')
await browser.close()
console.log('two logins made; the XMPP address is shown')
