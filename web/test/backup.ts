// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Playwright test (Firefox) of backups and a restore on a fresh board, run by backup.sh in two phases with the same browser
// (its storage is kept in OUT between them):
//   PHASE=backup: set up board A on its page, become the owner, turn on backups to a restic REST server in Settings, Backups,
//     and back up now.
//   PHASE=restore: on board B (fresh) restore from that server on the setup page, open Cloud Companion and be its owner already,
//     with no pairing: the devices came back with the data.
//   BASE=http://127.0.0.1 OUT=/out REST=http://<ip>:8000/cloud PHASE=backup node --experimental-strip-types backup.ts

import { firefox } from 'playwright'
import assert from 'node:assert/strict'
import { existsSync, readFileSync, writeFileSync } from 'node:fs'

const BASE = process.env.BASE || 'http://127.0.0.1'
const OUT = process.env.OUT || '.'
const REST = process.env.REST || ''
const PHASE = process.env.PHASE || 'backup'
const PASS = 'correct horse battery staple'
const BKPASS = 'a long backup password'
const STORE = `${OUT}/backup-browser.json`
const CREDS = `${OUT}/backup-creds.json`
// the device key is in IndexedDB, which Playwright 1.49 does not keep in a storage state: carried here as the browser keeps it
const idb = (op: 'get' | 'put', v?: unknown) => page.evaluate(([op, v]) => new Promise((resolve, reject) => {
  const r = indexedDB.open('phecloud', 1)
  r.onupgradeneeded = () => r.result.createObjectStore('kv')
  r.onerror = () => reject(r.error)
  r.onsuccess = () => {
    const tx = r.result.transaction('kv', op === 'get' ? 'readonly' : 'readwrite')
    const st = tx.objectStore('kv')
    if (op === 'get') { const q = st.get('creds'); q.onsuccess = () => resolve(q.result ? { dev: q.result.dev, key: Array.from(q.result.key) } : null) }
    else { const c = v as { dev: string; key: number[] }; st.put({ dev: c.dev, key: Uint8Array.from(c.key) }, 'creds'); tx.oncomplete = () => resolve(null) }
  }
}), [op, v] as const)
const browser = await firefox.launch()
const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, colorScheme: 'light', serviceWorkers: 'block', timezoneId: 'Europe/Athens',
  storageState: PHASE === 'restore' && existsSync(STORE) ? STORE : undefined })
const page = await ctx.newPage()
process.on('uncaughtException', async (e) => {
  await page.screenshot({ path: `${OUT}/backup-failed.png`, fullPage: true }).catch(() => {})
  console.error('app state:', await page.evaluate(() => JSON.stringify({ status: window.__phe?.state.status, admin: window.__phe?.state.admin, error: window.__phe?.state.error, url: location.href })).catch((x) => String(x)))
  console.error('console:\n' + consoleLog.slice(-15).join('\n'))
  console.error('page errors:', errors.slice(-5))
  console.error(e); process.exit(1)
})
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
const consoleLog: string[] = []
page.on('console', (m) => { if (m.type() === 'error' || m.type() === 'warning') consoleLog.push(`${new Date().toISOString().slice(11, 19)} ${m.type()}: ${m.text().slice(0, 300)}`) })
const visible = (sel: string, ms = 30000) => page.waitForSelector(sel, { state: 'visible', timeout: ms })
const pick = async (sel: string, text: string) => { await page.click(sel); await page.click(`.q-menu .q-item:has-text("${text}")`) }
// a q-input's data-test lands on its <input> or on its wrapper, depending on the version: fill whichever it is
const field = (t: string) => page.locator(`input[data-test=${t}], [data-test=${t}] input`).first()
const isOwner = () => page.evaluate(() => window.__phe?.state.status === 'online' && window.__phe.state.admin)
async function waitText(sel: string, re: RegExp, ms: number) {
  const text = () => page.locator(sel).first().textContent({ timeout: 1000 }).catch(() => '')
  for (const end = Date.now() + ms; Date.now() < end; await page.waitForTimeout(500)) if (re.test((await text()) || '')) return
  assert.match((await text()) || '', re, sel)
}

await page.goto(BASE + '/')
await visible('#locked')
await page.fill('#secret', 'PINE-APPLE-42')
await page.click('#unlockBtn')
await visible('#choose')

if (PHASE === 'backup') {
  await page.click('#pickNew')
  await visible('#setup')
  await page.fill('#pass1', PASS)
  await page.fill('#pass2', PASS)
  await page.click('#start')
  await visible('#done', 600000)
  await page.click('#open')
  await page.waitForSelector('[data-test=code]')
  await page.click('[data-test=pair]')
  for (let i = 0; i < 60 && !(await isOwner()); i++) await page.waitForTimeout(500)
  assert(await isOwner(), 'this browser is the owner of board A')

  await page.goto('http://127.0.0.1:8080/#/settings/backups')
  await visible('[data-test=backups-form]')
  assert.match(await page.textContent('[data-test=backups-status]') || '', /Backups are off/)
  await page.click('[data-test=backups-more]')
  await page.click('[data-test=backups-kind-rest]')
  await field('backups-url').fill(REST)
  await field('backups-password').fill(BKPASS)
  await field('backups-password2').fill(BKPASS)
  await page.click('[data-test=backups-save]')
  await waitText('[data-test=backups-status]', /Backups are on|Your data is backed up/, 300000)
  await page.click('[data-test=backups-now]')
  await waitText('[data-test=backups-last]', /Last backup/, 600000)
  await page.screenshot({ path: `${OUT}/backup-1-settings.png`, fullPage: true })
  for (let i = 0; i < 60 && !(await isOwner()); i++) await page.waitForTimeout(1000) // Cloud Companion is back after the backup
  assert(await isOwner(), 'Cloud Companion runs again after the backup')
  await ctx.storageState({ path: STORE })
  writeFileSync(CREDS, JSON.stringify(await idb('get')))
  console.log('backup ok')
} else {
  await page.click('#pickRestore')
  await visible('#restore')
  await page.click('#rMore')
  await page.check('#rKinds input[value=rest]')
  await page.fill('#rUrl', REST)
  await page.fill('#rPass', 'wrong password!!')
  await page.click('#rFind')
  await waitText('#restoreErr', /Wrong backup password/, 300000)
  await page.fill('#rPass', BKPASS)
  await page.click('#rFind')
  await visible('#rSnapBox', 120000)
  await page.screenshot({ path: `${OUT}/backup-2-restore.png`, fullPage: true })
  await page.click('#rStart')
  await visible('#done', 900000)
  const link = await page.getAttribute('#open', 'href')
  assert.equal(link, 'http://127.0.0.1:8080/', 'a restored board opens Cloud Companion, no owner code')
  await page.goto(link!) // the pairing page: this browser has no key for 127.0.0.1:8080 in the test's fresh profile
  await idb('put', JSON.parse(readFileSync(CREDS, 'utf8')))
  await page.goto(link!)
  for (let i = 0; i < 90 && !(await isOwner()); i++) await page.waitForTimeout(1000)
  assert(await isOwner(), 'the browser is the owner again, without pairing')
  await page.goto('http://127.0.0.1:8080/#/settings/backups')
  await waitText('[data-test=backups-status]', /Backups are on|Your data is backed up/, 30000)
  await page.screenshot({ path: `${OUT}/backup-3-restored.png`, fullPage: true })
  console.log('restore ok')
}
assert.deepEqual(errors, [], 'no page errors')
await browser.close()
