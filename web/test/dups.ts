// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Duplicate review in the browser against a REAL hub: two pairs of duplicate contacts are made, the review proposes them, "Keep" merges
// one pair (the other contact disappears from the hub, the kept one has both phone numbers), "Not the same" hides the other pair.
//   BASE=http://localhost:8080 CODE=<admin code> OUT=/out node --experimental-strip-types dups.ts
import { chromium } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://localhost:8080'
const OUT = process.env.OUT || '.'
const CODE = process.env.CODE
assert(CODE, 'CODE (an admin pairing code) is required')
const browser = await chromium.launch()
const ctx = await browser.newContext({ bypassCSP: true, viewport: { width: 900, height: 1000 } })
const page = await ctx.newPage()
const log: string[] = []
page.on('pageerror', (e) => log.push('pageerror: ' + e.message))
const t0 = Date.now()
const step = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)
async function shots(name: string) {
  for (const scheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: scheme })
    await page.waitForTimeout(250)
    await page.screenshot({ path: `${OUT}/dups-${scheme}-${name}.png` })
  }
  await page.emulateMedia({ colorScheme: 'light' })
}

await page.goto(BASE)
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online')
const save = (c: Omit<Contact, 'href' | 'etag'>) => page.evaluate((c) => window.__phe.hubCall({ op: 'contacts.save', contact: c }), c)
// a contact as the hub's contacts.list returns it (the fields this test reads)
interface Contact { href: string; etag: string; name: string; phones?: { type: string; value: string }[]; emails?: { type: string; value: string }[] }
const list = () => page.evaluate(async (): Promise<Contact[]> => (await window.__phe.hubCall({ op: 'contacts.list' })).contacts)
for (const c of await list()) if (/Fischer|Erik/.test(c.name)) await page.evaluate((h) => window.__phe.hubCall({ op: 'contacts.delete', href: h.href, etag: h.etag }), c)
await save({ name: 'Dora Fischer', phones: [{ type: 'cell', value: '+49 170 1112223' }] })
await save({ name: 'D. Fischer', phones: [{ type: 'home', value: '0170 1112223' }] })
await save({ name: 'Erik Berg', emails: [{ type: 'home', value: 'erik@example.org' }] })
await save({ name: 'Erik B.', emails: [{ type: 'work', value: 'ERIK@example.org' }] })

await page.goto(BASE + '/#/connections')
await page.waitForSelector('[data-test=find-dups]')
await page.click('[data-test=find-dups]')
await page.waitForFunction(() => document.querySelectorAll('[data-test=dup-row]').length >= 2)
assert.equal(await page.locator('[data-test=no-hub]').count(), 0, 'the hub is configured in this stack')
const rows = await page.locator('[data-test=dup-row]').allTextContents()
assert(rows.some((r) => /Dora Fischer/.test(r) && /same phone number/.test(r)), rows.join(' | '))
assert(rows.some((r) => /Erik/.test(r) && /same email address/.test(r)), rows.join(' | '))
await shots('1-suggestions')
step('the review proposes both pairs with their reasons')

// keep Dora: the other contact is merged into her
const dora = page.locator('[data-test=dup-row]', { hasText: 'Dora Fischer' })
await dora.getByRole('button', { name: /Keep Dora/ }).click()
await page.waitForFunction(() => ![...document.querySelectorAll('[data-test=dup-row]')].some((r) => /Dora/.test(r.textContent ?? '')))
let cs = await list()
const names = cs.map((c) => c.name)
assert(!names.includes('D. Fischer') && names.includes('Dora Fischer'), names.join())
const d = cs.find((c) => c.name === 'Dora Fischer')
assert(d, 'Dora Fischer is kept')
assert.equal(d.phones?.length, 1, 'the same number is not repeated')
step('"Keep" merged the pair on the hub (one contact left, number kept once)')

// "Not the same" hides the other pair and it stays hidden
await page.locator('[data-test=dup-row]', { hasText: 'Erik' }).locator('[data-test=dup-dismiss]').click()
await page.waitForFunction(() => document.querySelectorAll('[data-test=dup-row]').length === 0)
cs = await list()
assert(cs.map((c) => c.name).includes('Erik Berg') && cs.map((c) => c.name).includes('Erik B.'), 'dismissing deletes nothing')
await page.click('[data-test=find-dups]')
await page.waitForSelector('[data-test=no-dups]')
await shots('2-after')
step('"Not the same" keeps both contacts and the pair stays hidden')

for (const c of await list()) if (/Fischer|Erik/.test(c.name)) await page.evaluate((h) => window.__phe.hubCall({ op: 'contacts.delete', href: h.href, etag: h.etag }), c)
assert.deepEqual(log, [], 'page errors: ' + log.join('; '))
await browser.close()
console.log('DUPS PASS')
