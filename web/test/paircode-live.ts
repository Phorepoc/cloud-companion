// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// A look at a RUNNING server's Settings, "Pair a new device": pairs a throwaway admin browser with an admin code, makes a code in the page, checks it against the server's own
// fingerprint, pairs a second throwaway device with it, and saves screenshots. The devices it makes are named "paircode-live-*": revoke them afterwards (`phecloud devices`, `phecloud revoke <id>`).
//   BASE=https://host:9443 CODE=<admin code> FP=<server fingerprint> OUT=/out node --experimental-strip-types paircode-live.ts
import { chromium } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE!, CODE = process.env.CODE!, FP = process.env.FP || '', OUT = process.env.OUT || '.'
const browser = await chromium.launch()
const ctx = await browser.newContext({ bypassCSP: true, ignoreHTTPSErrors: true, viewport: { width: 1100, height: 900 } })
await ctx.grantPermissions(['clipboard-read', 'clipboard-write'])
const page = await ctx.newPage()
await page.goto(BASE)
await page.fill('[data-test=code]', CODE)
await page.fill('[data-test=name]', 'paircode-live-admin')
await page.click('[data-test=pair]')
await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online', null, { timeout: 30000 })
await page.goto(BASE + '/#/settings')
await page.waitForSelector('[data-test=pair-generate]')
await page.click('[data-test=pair-generate]')
await page.waitForSelector('[data-test=pair-code]')
const code = ((await page.textContent('[data-test=pair-code]')) ?? '').trim()
const fp = ((await page.textContent('[data-test=pair-fingerprint]')) ?? '').replace(/\s+/g, '')
const timer = ((await page.textContent('[data-test=pair-countdown]')) ?? '').trim()
console.log('code', code, '| countdown:', timer, '| fingerprint', fp)
assert.match(code, /^[A-Z0-9]{4}-[A-Z0-9]{4}$/)
if (FP) assert.equal(fp, FP, 'the fingerprint shown is the server certificate fingerprint')
await page.locator('[data-test=pair-device]').scrollIntoViewIfNeeded()
await page.screenshot({ path: OUT + '/pair-live-light.png' })
await page.emulateMedia({ colorScheme: 'dark' })
await page.waitForTimeout(300)
await page.screenshot({ path: OUT + '/pair-live-dark.png' })
await page.setViewportSize({ width: 390, height: 844 })
await page.waitForTimeout(500)
await page.locator('[data-test=pair-device]').scrollIntoViewIfNeeded()
await page.screenshot({ path: OUT + '/pair-live-phone.png' })

const ctx2 = await browser.newContext({ bypassCSP: true, ignoreHTTPSErrors: true })
const p2 = await ctx2.newPage()
await p2.goto(BASE)
await p2.fill('[data-test=code]', code)
await p2.fill('[data-test=name]', 'paircode-live-normal')
await p2.click('[data-test=pair]')
await p2.waitForFunction(() => window.__phe && window.__phe.state.status === 'online', null, { timeout: 30000 })
assert.equal(await p2.evaluate(() => window.__phe.state.admin), false)
await p2.goto(BASE + '/#/settings')
await p2.waitForSelector('[data-test=pair-denied]')
await p2.screenshot({ path: OUT + '/pair-live-denied.png' })
console.log('the code paired a normal device; its Settings shows the reason instead of the action')
await browser.close()
console.log('OK')
