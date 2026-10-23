// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Chats page with a FAKE PHONE: a second paired browser context plays the phone's SMS service (it signs /sms/sync calls with its device
// key). The owner's browser lists the conversation, sends a message through that phone, and watches the status follow the phone's acks.
//   BASE=http://localhost:8080 CODES=<admin>,<plain> OUT=/out node chats.mjs
import { chromium } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://localhost:8080'
const OUT = process.env.OUT || '.'
const [ADMIN, PLAIN] = (process.env.CODES || '').split(',')
assert(ADMIN && PLAIN, 'CODES=<admin>,<plain> is required')
const browser = await chromium.launch()
const log = []
const t0 = Date.now()
const step = (s) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)

async function pairIn(code, name, viewport) {
  const ctx = await browser.newContext({ bypassCSP: true, viewport })
  const page = await ctx.newPage()
  page.on('pageerror', (e) => log.push('pageerror: ' + e.message))
  await page.goto(BASE)
  await page.fill('[data-test=code]', code)
  await page.fill('[data-test=name]', name)
  await page.click('[data-test=pair]')
  await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online')
  return page
}
const owner = await pairIn(ADMIN, 'Owner browser', { width: 1000, height: 900 })
const phone = await pairIn(PLAIN, 'Test phone', { width: 400, height: 700 })
const sync = (body) => phone.evaluate((b) => window.__phe.apiCall('POST', '/sms/sync', b), body)
async function shots(name) {
  for (const scheme of ['light', 'dark']) {
    await owner.emulateMedia({ colorScheme: scheme })
    await owner.waitForTimeout(250)
    await owner.screenshot({ path: `${OUT}/chats-${scheme}-${name}.png` })
  }
  await owner.emulateMedia({ colorScheme: 'light' })
}

await owner.goto(BASE + '/#/chats')
await owner.waitForSelector('[data-test=no-sms-devices]')
step('no phone syncs yet: the page says so')

const now = Math.floor(Date.now() / 1000)
const up = await sync({ messages: [
  { id: 'p-1', number: '+49 171 5550100', dir: 'in', body: 'Are you coming tonight?', ts: now - 300 },
  { id: 'p-2', number: '0171 5550100', dir: 'out', body: 'Yes, around eight', ts: now - 240, status: 'sent' },
  { id: 'p-3', number: '+49 171 5550100', dir: 'in', body: 'Great, see you', ts: now - 200 },
  { id: 'p-4', number: 'BANK', dir: 'in', body: 'Your code is 123456', ts: now - 100 },
] })
assert.equal(up.accepted, 4)
assert.equal((await sync({ messages: [{ id: 'p-1', number: '0171 5550100', dir: 'in', body: 'Are you coming tonight?', ts: now - 300 }] })).accepted, 0, 'a retry stores nothing')
step('the phone uploaded 4 messages; a retry added none')

await owner.waitForSelector('[data-test=conv]', { timeout: 15000 })
assert.equal(await owner.locator('[data-test=conv]').count(), 2, 'two conversations: the person (both spellings of the number) and the sender id')
assert.equal(await owner.locator('[data-test=no-sms-devices]').count(), 0)
await owner.locator('[data-test=conv]', { hasText: 'see you' }).click()
await owner.waitForFunction(() => document.querySelectorAll('[data-test=msg]').length === 3)
assert(await owner.locator('[data-test=send-via]').count() >= 1)
await shots('1-thread')
step('conversation by number: 3 messages in one thread (+49 and 0171 spellings merged)')

await owner.fill('[data-test=sms-text]', 'Running late, 10 minutes')
await owner.click('[data-test=sms-send]')
await owner.waitForFunction(() => document.querySelectorAll('[data-test=msg]').length === 4)
assert.match(await owner.locator('[data-test=msg]').last().textContent(), /waiting for the phone/)
step('sent from the web: queued for the phone')

// the phone's next sync picks it up, sends, reports
const r = await sync({})
assert.equal(r.pending.length, 1)
assert.equal(r.pending[0].body, 'Running late, 10 minutes')
assert.match(r.pending[0].number, /5550100/)
await sync({ acks: [{ out_id: r.pending[0].out_id, id: 'p-5', status: 'sent' }] })
await owner.waitForFunction(() => /^sent$/.test(document.querySelectorAll('[data-test=msg-status]')[document.querySelectorAll('[data-test=msg-status]').length - 1]?.textContent.trim()), null, { timeout: 15000 })
await sync({ acks: [{ out_id: r.pending[0].out_id, id: 'p-5', status: 'delivered' }] })
await owner.waitForFunction(() => /^delivered$/.test(document.querySelectorAll('[data-test=msg-status]')[document.querySelectorAll('[data-test=msg-status]').length - 1]?.textContent.trim()), null, { timeout: 15000 })
await shots('2-delivered')
step('the status follows the phone: waiting, sent, delivered')

// a failure is shown plainly
await owner.fill('[data-test=sms-text]', 'This one fails')
await owner.click('[data-test=sms-send]')
const r2 = await (async () => { for (let i = 0; i < 20; i++) { const x = await sync({}); if (x.pending.length) return x; await owner.waitForTimeout(300) } })()
await sync({ acks: [{ out_id: r2.pending[0].out_id, id: 'p-6', status: 'failed', error: 'no signal' }] })
await owner.waitForFunction(() => /not sent \(no signal\)/.test(document.body.textContent), null, { timeout: 15000 })
await shots('3-failed')
step('a failed send is shown with the reason')

// a phone cannot read the owner's conversations, and a new message to a new number works
assert.rejects(phone.evaluate(() => window.__phe.apiCall('GET', '/sms/conversations')), /sign in as the owner/)
await owner.click('[data-test=new-chat]')
await owner.fill('[data-test=new-number]', '0176 5550199')
await owner.fill('[data-test=sms-text]', 'Hello, new number')
await owner.click('[data-test=sms-send]')
await owner.waitForFunction(() => document.querySelectorAll('[data-test=conv]').length === 3)
step('a new conversation can be started from the web')
assert.deepEqual(log, [], 'page errors: ' + log.join('; '))
await browser.close()
console.log('CHATS PASS')
