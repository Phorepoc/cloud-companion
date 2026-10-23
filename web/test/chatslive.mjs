// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Looks at the Chats page of a server that a REAL (emulated) phone syncs with, and optionally sends a message through that phone.
//   BASE=http://localhost:8085 CODE=<admin code> OUT=/out SEND="reply text" node chatslive.mjs
import { chromium } from 'playwright'

const BASE = process.env.BASE
const OUT = process.env.OUT || '.'
const browser = await chromium.launch()
const ctx = await browser.newContext({ bypassCSP: true, viewport: { width: 1000, height: 900 } })
const page = await ctx.newPage()
await page.goto(BASE)
await page.fill('[data-test=code]', process.env.CODE)
await page.click('[data-test=pair]')
await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online')
const j = await page.evaluate(() => window.__phe.apiCall('GET', '/sms/conversations'))
console.log('CONVERSATIONS ' + JSON.stringify(j))
await page.goto(BASE + '/#/chats')
await page.waitForTimeout(1500)
if (j.conversations.length) {
  await page.locator('[data-test=conv]').first().click()
  await page.waitForSelector('[data-test=msg]')
  console.log('THREAD ' + JSON.stringify(await page.evaluate((k) => window.__phe.apiCall('GET', '/sms/thread?key=' + k), j.conversations[0].key)))
  if (process.env.SEND) {
    await page.fill('[data-test=sms-text]', process.env.SEND)
    await page.click('[data-test=sms-send]')
    await page.waitForTimeout(1500)
  }
}
await page.screenshot({ path: `${OUT}/chatslive.png` })
await browser.close()
