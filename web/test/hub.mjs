// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Playwright test of the Contacts and Calendar pages against a live hub (Stalwart through the server):
//   BASE=http://phecloud:8080 CODE=ABCD-EFGH OUT=/out node hub.mjs
// Creates, finds, edits and deletes a contact and an event (with Undo and with the toast running out), and leaves the hub as it found
// it. Screenshots (light and dark, phone and desktop) go to OUT.
import { chromium } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const CODE = process.env.CODE
const OUT = process.env.OUT || '.'
assert(CODE, 'CODE is required')
const stamp = Date.now().toString(36)
const NAME = `Pw ${stamp}`
const browser = await chromium.launch()
const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 }, colorScheme: 'light' })
const page = await ctx.newPage()
const log = []
page.on('console', (m) => log.push(m.type() + ': ' + m.text()))
page.on('pageerror', (e) => log.push('pageerror: ' + e.message))
const t0 = Date.now()
const step = (s) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)
const go = async (route) => { await page.evaluate((h) => { location.hash = '#/' + h }, route); await page.waitForTimeout(300) }
const toastText = () => page.evaluate(() => document.querySelector('[data-test=toasts]')?.textContent || '')

async function shots(prefix) {
  for (const [vp, size] of [['phone', { width: 390, height: 844 }], ['desktop', { width: 1280, height: 800 }]]) {
    await page.setViewportSize(size)
    for (const scheme of ['light', 'dark']) {
      await page.emulateMedia({ colorScheme: scheme })
      await page.waitForTimeout(250)
      await page.screenshot({ path: `${OUT}/${vp}-${scheme}-${prefix}.png` })
    }
  }
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.emulateMedia({ colorScheme: 'light' })
}

try {
  await page.goto(BASE + '/#/pair')
  await page.fill('[data-test=code]', CODE)
  await page.click('[data-test=pair]')
  await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online')
  step('paired')

  // ---- contacts ----
  await go('contacts')
  await page.waitForSelector('[data-test=contact-new]')
  await page.waitForTimeout(1200)
  const before = await page.locator('[data-test^=contact-row-]').count()
  step('contacts page loaded, ' + before + ' existing contact(s)')
  await shots('contacts')

  await page.click('[data-test=contact-new]')
  await page.fill('[data-test=contact-given]', NAME)
  await page.fill('[data-test=contact-family]', 'Tester')
  await page.fill('[data-test=contact-org]', 'Playwright Ltd')
  await page.click('[data-test=contact-add-phone]')
  await page.fill('[data-test=contact-phone-0]', '+44 20 7946 0000')
  await page.click('[data-test=contact-add-email]')
  await page.fill('[data-test=contact-email-0]', `pw-${stamp}@example.org`)
  await page.fill('[data-test=contact-note]', 'Made by the test.')
  await page.screenshot({ path: OUT + '/desktop-light-contact-form.png' })
  await page.click('[data-test=contact-save]')
  await page.waitForSelector('[data-test=toast]')
  assert.match(await toastText(), /Contact added/)
  await page.waitForSelector(`text=${NAME} Tester`)
  step('contact created; the toast said so')

  await page.fill('[data-test=contacts-search]', 'playwright ltd')
  assert.equal(await page.locator('[data-test^=contact-row-]').count(), 1, 'search finds exactly the new contact')
  await page.fill('[data-test=contacts-search]', 'zzzz-nothing')
  await page.waitForSelector('[data-test=contacts-none]')
  await page.fill('[data-test=contacts-search]', '')
  step('search works')

  await page.locator('[data-test^=contact-row-]', { hasText: NAME }).click()
  assert.match(await page.textContent('[data-test=contact-view]'), /\+44 20 7946 0000/)
  await page.screenshot({ path: OUT + '/desktop-light-contact-view.png' })
  await page.click('[data-test=contact-edit]')
  await page.fill('[data-test=contact-org]', 'Playwright Group')
  await page.click('[data-test=contact-save]')
  await page.waitForFunction(() => /Contact saved/.test(document.querySelector('[data-test=toasts]')?.textContent || ''))
  await page.waitForSelector('text=Playwright Group')
  await page.locator('[data-test^=contact-row-]', { hasText: NAME }).click()
  assert.match(await page.textContent('[data-test=contact-view]'), /\+44 20 7946 0000/)
  assert.match(await page.textContent('[data-test=contact-view]'), /Made by the test/)
  step('contact edited; the phone number and the note survived the round trip')
  await page.keyboard.press('Escape')

  // delete with Undo: the contact comes back; delete again and let the toast run out: it is gone on the server
  await page.locator('[data-test^=contact-row-]', { hasText: NAME }).click()
  await page.click('[data-test=contact-delete]')
  await page.waitForSelector('[data-test=toast-undo]')
  await page.click('[data-test=toast-undo]')
  await page.waitForSelector(`text=${NAME} Tester`)
  step('delete then Undo: the contact is still there')
  await page.locator('[data-test^=contact-row-]', { hasText: NAME }).click()
  await page.click('[data-test=contact-delete]')
  await page.waitForTimeout(5500)
  await go('devices')
  await go('contacts')
  await page.waitForTimeout(1500)
  assert.equal(await page.locator('[data-test^=contact-row-]', { hasText: NAME }).count(), 0, 'deleted on the server')
  assert.equal(await page.locator('[data-test^=contact-row-]').count(), before)
  step('delete and let the toast run out: gone on the server')

  // ---- calendar ----
  await go('calendar')
  await page.waitForSelector('[data-test=event-new]')
  await page.waitForTimeout(1200)
  const today = new Date()
  const iso = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`
  await shots('calendar')
  await page.click('[data-test=event-new]')
  await page.fill('[data-test=event-title]', `Event ${stamp}`)
  await page.fill('[data-test=event-start-date]', iso)
  await page.fill('[data-test=event-start-time]', '14:00')
  await page.fill('[data-test=event-end-date]', iso)
  await page.fill('[data-test=event-end-time]', '15:30')
  await page.fill('[data-test=event-location]', 'Room 4')
  await page.screenshot({ path: OUT + '/desktop-light-event-form.png' })
  await page.click('[data-test=event-save]')
  await page.waitForFunction(() => /Event added/.test(document.querySelector('[data-test=toasts]')?.textContent || ''))
  await page.waitForSelector(`text=Event ${stamp}`)
  step('event created and listed under today')
  await shots('calendar-events')

  await page.locator('[data-test=event-row]', { hasText: `Event ${stamp}` }).click()
  assert.match(await page.textContent('[data-test=event-view]'), /Room 4/)
  await page.click('[data-test=event-edit]')
  await page.fill('[data-test=event-title]', `Event ${stamp} (moved)`)
  await page.fill('[data-test=event-start-time]', '16:00')
  await page.fill('[data-test=event-end-time]', '17:00')
  await page.click('[data-test=event-save]')
  await page.waitForSelector(`text=Event ${stamp} (moved)`)
  const row = page.locator('[data-test=event-row]', { hasText: `Event ${stamp} (moved)` })
  assert.match(await row.textContent(), /4:00|16:00/)
  step('event edited: new title and time')

  await page.click('[data-test=cal-next]')
  await page.waitForTimeout(1000)
  assert.equal(await page.locator('[data-test=event-row]', { hasText: stamp }).count(), 0, 'not in the next month')
  await page.click('[data-test=cal-today]')
  await page.waitForSelector(`text=Event ${stamp} (moved)`)
  step('month navigation')

  await row.click()
  await page.click('[data-test=event-delete]')
  await page.waitForSelector('[data-test=toast-undo]')
  await page.waitForTimeout(5500)
  await go('devices')
  await go('calendar')
  await page.waitForTimeout(1500)
  assert.equal(await page.locator('[data-test=event-row]', { hasText: stamp }).count(), 0, 'event deleted on the server')
  step('event deleted')

  // ---- month grid (the default view) and the agenda toggle; times are written with the browser's zone ----
  await go('calendar')
  await page.waitForSelector('[data-test=cal-grid]')
  await page.click('[data-test=event-new]')
  await page.fill('[data-test=event-title]', `Zoned ${stamp}`)
  await page.fill('[data-test=event-start-date]', iso)
  await page.fill('[data-test=event-start-time]', '10:15')
  await page.fill('[data-test=event-end-date]', iso)
  await page.fill('[data-test=event-end-time]', '11:15')
  assert.match(await page.textContent('[data-test=event-zone]'), /time zone \(.+\)/)
  await page.click('[data-test=event-save]')
  await page.waitForSelector('[data-test=cal-selected] >> text=Zoned ' + stamp)
  await page.waitForSelector(`[data-test=cal-day-${iso}] .ph-chip`)
  assert.match(await page.locator('[data-test=cal-selected]').textContent(), /10:15|10:15 AM/)
  step('month grid: the event shows on its day (chip) and in the selected-day list at the time that was entered')
  await shots('calendar-month')
  await page.click('[data-test=cal-view] >> text=Agenda')
  await page.waitForSelector(`text=Zoned ${stamp}`)
  await page.click('[data-test=cal-view] >> text=Month')
  await page.waitForSelector('[data-test=cal-grid]')
  step('agenda toggle')
  await page.locator('[data-test=event-row]', { hasText: `Zoned ${stamp}` }).click()
  await page.click('[data-test=event-delete]')
  await page.waitForTimeout(5500)

  // ---- app passwords as rows in Devices ----
  await go('devices')
  await page.waitForSelector('[data-test=apppw-new]')
  await page.click('[data-test=apppw-new]')
  await page.fill('[data-test=apppw-name]', `pw-test ${stamp}`)
  await page.click('[data-test=apppw-create]')
  await page.waitForSelector('[data-test=apppw-secret-dialog]')
  const secret = (await page.textContent('[data-test=apppw-secret]')).trim()
  assert.match(secret, /^app_/)
  await page.screenshot({ path: OUT + '/desktop-light-apppw-secret.png' })
  await page.click('[data-test=apppw-done]')
  await page.waitForSelector(`text=pw-test ${stamp}`)
  step('app password created (shown once), listed in Devices')
  await shots('devices-apppw')
  page.once('dialog', (d) => d.accept())
  await page.locator('[data-test^=apppw-row-]', { hasText: `pw-test ${stamp}` }).locator('[data-test^=apppw-revoke-]').click()
  await page.waitForFunction((n) => !document.body.textContent.includes(n), `pw-test ${stamp}`, { timeout: 10000 })
  step('app password switched off: the row is gone')

  // ---- mail, read-only ----
  await go('mail')
  await page.waitForSelector('[data-test=mail-folder]')
  await page.waitForTimeout(1500)
  await shots('mail')
  const mailCount = await page.locator('[data-test^=mail-row-]').count()
  if (mailCount > 0) {
    await page.click('[data-test=mail-row-0]')
    await page.waitForSelector('[data-test=mail-view-body]')
    assert((await page.textContent('[data-test=mail-view-subject]')).length > 0)
    assert((await page.textContent('[data-test=mail-view-body]')).length > 0)
    await page.screenshot({ path: OUT + '/desktop-light-mail-view.png' })
    await page.keyboard.press('Escape')
    step('mail: folder list, ' + mailCount + ' message(s) in the inbox, one opened and read')
  } else {
    step('mail: folders load; the inbox is empty (nothing to read)')
  }
  console.log('HUB PASS')
} catch (e) {
  console.error('HUB FAIL:', e.message)
  console.error(log.slice(-12).join('\n'))
  await page.screenshot({ path: OUT + '/hub-fail.png' }).catch(() => {})
  process.exitCode = 1
} finally {
  await browser.close()
}
