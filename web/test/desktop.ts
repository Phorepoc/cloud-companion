// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Playwright test (Firefox) of the desktop view, run against a live server:
//   BASE=http://phecloud:8080 CODE=ABCD-EFGH OUT=/out node --experimental-strip-types desktop.ts
//
// The desktop is the shared Etesian one (@etesian-design/shell). Pairs at 1440 x 900, checks the colour modes, enters the desktop from
// the top bar, opens sections from the dock, snaps by dragging, drags a floating window over the dock, uses the full-screen Overview
// (Events | the open windows themselves | the sections), Events, checks that no Super or Ctrl+Alt shortcut is offered, minimises and
// restores, closes, reloads, goes back to the classic view, and that a small screen always gets the classic layout.

import { firefox } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const CODE = process.env.CODE
const OUT = process.env.OUT || '.'
assert(CODE, 'CODE (a pairing code from `phecloud paircode --admin`) is required')

const browser = await firefox.launch()
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: 'light', serviceWorkers: 'block' })
const page = await ctx.newPage()
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
const t0 = Date.now()
const step = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)
async function box(sel: string) {
  const b = await page.locator(sel).first().boundingBox()
  assert(b, `${sel} has no box`)
  return b
}
const win = (id: string) => `.x-win[data-win=${id}]`
// a dock item is named after its section, and while it runs, its state too ("Mail, minimised")
const dockBtn = (label: string) => page.locator(`.x-shell-dock button[aria-label="${label}"], .x-shell-dock button[aria-label^="${label}, "]`).first()
const barApp = async () => ((await page.textContent('.x-mbar__app')) ?? '').trim()
/** what the pointer would hit at the centre of a box */
const hitAt = (x: number, y: number) => page.evaluate(([x, y]) => {
  const e = document.elementFromPoint(x, y)
  return e?.closest('.x-win') ? 'window' : e?.closest('.x-shell-dock') ? 'dock' : 'other'
}, [x, y])

step('pair')
await page.goto(BASE + '/#/pair')
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online', null, { timeout: 20000 })

step('enter the desktop from the top bar: the current section opens as a window and names the menu bar')
await page.goto(BASE + '/#/settings')
await page.click('[data-test=desktop-view]')
await page.waitForSelector('[data-test=desktop]')
await page.waitForSelector(win('settings'))
assert.equal(await barApp(), 'Settings')
await page.screenshot({ path: `${OUT}/desktop-1-enter.png` })

step('the dock: Overview first, the sections, the recycle bin last')
const dockLabels = await page.locator('.x-shell-dock button').evaluateAll((bs) => bs.map((b) => b.getAttribute('aria-label')))
assert.equal(dockLabels[0], 'Overview')
assert.match(String(dockLabels.at(-1)), /bin/i, `the recycle bin ends the dock: ${dockLabels.join(', ')}`)

step('open Mail from the dock: it comes to the front')
await dockBtn('Mail').click()
await page.waitForSelector(win('mail'))
assert.equal(await barApp(), 'Mail')
const z = async (id: string) => Number(await page.locator(win(id)).evaluate((el) => getComputedStyle(el).zIndex))
assert((await z('mail')) > (await z('settings')), 'the newest window is on top')

step('a floating window dragged over the dock covers it (regression 2026-11-06: windows always went under the dock)')
const dock = await box('.x-shell-dock')
const bar = await box(`${win('mail')} .x-win__drag`)
await page.mouse.move(bar.x + 250, bar.y + 10)
await page.mouse.down()
await page.mouse.move(bar.x + 100, bar.y + 120, { steps: 15 })
await page.mouse.up()
const wb = await box(win('mail'))
assert(wb.x < dock.x + dock.width, `the window moved over the dock (${wb.x} < ${dock.x + dock.width})`)
assert.equal(await hitAt(dock.x + dock.width / 2, wb.y + 200), 'window', 'the window is drawn over the dock')

step('maximised, a window stops at the dock edge')
await page.dblclick(`${win('mail')} .x-win__drag`)
const mb = await box(win('mail'))
assert(mb.x >= dock.x + dock.width, `a maximised window starts right of the dock (${mb.x} >= ${dock.x + dock.width})`)
await page.dblclick(`${win('mail')} .x-win__drag`)

step('drag Mail by its bar to the right edge: it snaps to the right half')
const b2 = await box(`${win('mail')} .x-win__drag`)
await page.mouse.move(b2.x + 60, b2.y + 10)
await page.mouse.down()
await page.mouse.move(1438, 450, { steps: 20 })
await page.mouse.up()
const rb = await box(win('mail'))
assert(Math.abs(rb.x + rb.width - 1440) < 20 && rb.width < 760, `snapped to the right half: ${JSON.stringify(rb)}`)
assert.equal(await page.locator('.x-shell-zone').count(), 0, 'the snap preview is gone after the drop')

step('the Overview (dock button): full screen, Events | the windows themselves | the sections; typing searches; Enter opens')
await dockBtn('Overview').click()
const ov = await box('.x-shell-ov')
assert(ov.width >= 1440 - 2 && ov.height >= 900 - 40, `the Overview covers the screen under the menu bar: ${JSON.stringify(ov)}`)
assert.equal(await page.locator('.x-shell-ov').evaluate((e) => getComputedStyle(e).backgroundColor.includes('0, 0, 0, 0') || getComputedStyle(e).opacity !== '1'), false, 'opaque')
assert(await page.locator(`${win('mail')}.is-card`).isVisible(), 'the Mail window itself is a card')
assert(await page.locator('.x-shell-ov__events [data-test=events]').isVisible(), 'Events on the left')
await page.waitForTimeout(700) // the windows settle into their cards
await page.screenshot({ path: `${OUT}/desktop-2-overview.png` })
await page.keyboard.type('cont')
await page.keyboard.press('Enter')
await page.waitForSelector(win('contacts'))
assert.equal(await page.locator('.x-shell-ov').count(), 0)

step('Events from the clock, full height; Escape closes')
await page.click('.x-shell__clock')
await page.waitForSelector('[data-test=events]')
await page.screenshot({ path: `${OUT}/desktop-3-events.png` })
await page.keyboard.press('Escape')
await page.waitForFunction(() => !document.querySelector('[data-test=events]'))

step('no Super or Ctrl+Alt shortcut is offered anywhere (they belong to the system in a browser)')
for (const m of ['View', 'Window']) {
  await page.getByRole('menuitem', { name: m, exact: true }).click()
  await page.screenshot({ path: `${OUT}/desktop-4-menu-${m}.png` })
  const t = (await page.getByRole('menu', { name: m }).textContent({ timeout: 5000 })) ?? ''
  assert(!/Super|Ctrl\+Alt/.test(t), `${m} menu: ${t}`)
  await page.keyboard.press('Escape')
}
assert(!/Super|Ctrl\+Alt/.test((await page.content())), 'no Super or Ctrl+Alt in the page')

step('minimise from the window bar, restore from the dock')
await page.click(`${win('contacts')} [aria-label=Minimise]`)
await page.waitForFunction(() => !document.querySelector('.x-win[data-win=contacts]'))
await dockBtn('Contacts').click()
await page.waitForSelector(win('contacts'))

step('close a window')
await page.click(`${win('contacts')} [aria-label=Close]`)
await page.waitForFunction(() => !document.querySelector('.x-win[data-win=contacts]'))

step('the arrangement survives a reload')
await page.reload()
await page.waitForSelector(win('mail'))
await page.waitForSelector(win('settings'))

step('back to the classic view from the menu bar')
await page.getByRole('menuitem', { name: 'Cloud Companion', exact: true }).click()
await page.getByRole('menu', { name: 'Cloud Companion' }).getByText('Classic view').click()
await page.waitForFunction(() => !document.querySelector('[data-test=desktop]'))

step('a small screen always gets the classic layout')
await page.setViewportSize({ width: 800, height: 900 })
await page.goto(BASE + '/#/desktop')
await page.waitForTimeout(800)
assert.equal(await page.locator('[data-test=desktop]').count(), 0)

assert.deepEqual(errors, [], 'no page errors')
step('ok')
await browser.close()
