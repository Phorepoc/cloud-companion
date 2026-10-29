// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Playwright test of the desktop view and the Etesian theme, run against a live server:
//   BASE=http://phecloud:8080 CODE=ABCD-EFGH OUT=/out node --experimental-strip-types desktop.ts
//
// Pairs at 1440 x 900, checks the Etesian colour modes, enters the desktop from the top bar, opens sections from the dock, moves
// focus, snaps by dragging to an edge and with Ctrl+Alt+arrows, uses the launcher, the quick switcher and Events, opens Android as a
// phone-shaped window (and checks its phone layout), minimises and restores,
// follows a link between sections into a window, shows the desktop and edits its widgets (add, move, remove, kept on reload), goes back to the classic view (menu and Settings > Layout), checks that the choice is remembered and that
// a small screen always gets the classic layout.
import { chromium } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const CODE = process.env.CODE
const OUT = process.env.OUT || '.'
assert(CODE, 'CODE (a pairing code from `phecloud paircode --admin`) is required')

const browser = await chromium.launch()
const ctx = await browser.newContext({ bypassCSP: true, viewport: { width: 1440, height: 900 }, colorScheme: 'light',
  serviceWorkers: 'block' }) // so page.route sees every request (the Android checks fake the ticket)
const page = await ctx.newPage()
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
const t0 = Date.now()
const step = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)
/** The element's box; fails the test, naming the selector, when it is not laid out. */
async function box(sel: string) {
  const b = await page.locator(sel).first().boundingBox()
  assert(b, `${sel} has no box`)
  return b
}
const shown = (sel: string) => page.locator(sel).isVisible()
const text = async (sel: string) => (await page.textContent(sel)) ?? ''
// the page's h1 inside a window: kept for assistive technology, but only a 1 px clipped box on screen
const titleHidden = async (id: string) => ((await page.locator(`[data-test=win-${id}] h1`).boundingBox())?.width ?? 0) <= 1

step('pair')
await page.goto(BASE + '/#/pair')
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online', null, { timeout: 20000 })

step('Etesian theme: hybrid when light, dark when dark, Fira Sans or Nokia Pure')
assert.equal(await page.evaluate(() => document.documentElement.dataset.hdMode), 'hybrid')
assert(await page.evaluate(() => document.body.classList.contains('hd-quasar')), 'the Etesian Quasar theme is on')
assert.match(await page.evaluate(() => getComputedStyle(document.body).fontFamily), /Nokia Pure Text|HD Fira Sans/)
await page.emulateMedia({ colorScheme: 'dark' })
await page.waitForFunction(() => document.documentElement.dataset.hdMode === 'dark')
await page.emulateMedia({ colorScheme: 'light' })
await page.waitForFunction(() => document.documentElement.dataset.hdMode === 'hybrid')

step('enter the desktop from the top bar: the current section opens as a window')
await page.goto(BASE + '/#/settings')
await page.click('[data-test=desktop-view]')
await page.waitForSelector('[data-test=desktop]')
await page.waitForSelector('[data-test=win-settings]')
assert.equal((await text('[data-test=menubar-app]')).trim(), 'Settings')

step('open Mail from the dock: it comes to the front and names the menu bar')
await page.click('[data-test=dock-mail]')
await page.waitForSelector('[data-test=win-mail]')
assert.equal((await text('[data-test=menubar-app]')).trim(), 'Mail')
assert(await titleHidden('mail'), 'the window bar names the page: its h1 is not shown twice')
const z = async (id: string) => Number(await page.locator(`[data-test=win-${id}]`).evaluate((el) => getComputedStyle(el).zIndex))
assert((await z('mail')) > (await z('settings')), 'the newest window is on top')

step('drag Mail by its bar to the right edge: it snaps to the right half')
const bar = await box('[data-test=win-mail] header')
await page.mouse.move(bar.x + bar.width / 2, bar.y + 20)
await page.mouse.down()
await page.mouse.move(1439, 300, { steps: 15 })
assert(await shown('.ph-desk__zone'), 'the snap zone shows while dragging')
await page.mouse.up()
let r = await box('[data-test=win-mail]')
assert(r.x > 700 && r.x + r.width > 1400 && r.height > 800, `Mail fills the right half (${JSON.stringify(r)})`)

step('Ctrl+Alt+Left snaps the focused window to the left half, Ctrl+Alt+Up maximises it')
await page.keyboard.press('Control+Alt+ArrowLeft')
r = await box('[data-test=win-mail]')
assert(r.x < 100 && r.width < 720 && r.width > 600, `Mail fills the left half (${JSON.stringify(r)})`)
await page.keyboard.press('Control+Alt+ArrowUp')
r = await box('[data-test=win-mail]')
assert(r.width > 1300, 'Mail is maximised')

step('the launcher: type to find a section, Enter opens it')
await page.click('[data-test=dock-launcher]')
await page.waitForSelector('[data-test=launcher]')
await page.keyboard.type('cal')
assert(await shown('[data-test=launch-calendar]'))
assert(!(await shown('[data-test=launch-mail]')), 'the launcher filters by name')
await page.keyboard.press('Enter')
await page.waitForSelector('[data-test=win-calendar]')
assert(!(await shown('[data-test=launcher]')), 'the launcher closes after opening')
await page.screenshot({ path: `${OUT}/desktop-light.png` })

step('quick switcher: Ctrl+Alt+Tab steps through the windows, most recent first; letting go switches')
await page.keyboard.down('Control'); await page.keyboard.down('Alt')
await page.keyboard.press('Tab')
await page.waitForSelector('[data-test=switcher]')
assert.equal(await page.getAttribute('[data-test=switch-mail]', 'aria-selected'), 'true', 'the previous window is selected first')
await page.keyboard.press('Tab')
assert.equal(await page.getAttribute('[data-test=switch-settings]', 'aria-selected'), 'true', 'Tab again goes one further back')
await page.keyboard.up('Alt'); await page.keyboard.up('Control')
await page.waitForSelector('[data-test=switcher]', { state: 'detached' })
assert.equal((await text('[data-test=menubar-app]')).trim(), 'Settings')
await page.keyboard.down('Control'); await page.keyboard.down('Alt')
await page.keyboard.press('Tab')
await page.keyboard.press('Escape')
await page.keyboard.up('Alt'); await page.keyboard.up('Control')
assert.equal((await text('[data-test=menubar-app]')).trim(), 'Settings', 'Escape cancels the switch')

step('Events: the clock opens the panel (calendar, mail, chats), Escape closes it, Ctrl+Alt+N toggles it, a window click dismisses it')
await page.click('[data-test=clock]')
await page.waitForSelector('[data-test=events]')
for (const h of ['Calendar', 'Mail', 'Chats']) assert(await shown(`[data-test=events] h2:has-text("${h}")`), h + ' in Events')
// today and tomorrow, or one line saying the calendar service is not set up (as on the test server)
assert.match(await text('[data-test=events]'), /Today[\s\S]*Tomorrow|calendar is not set up/)
await page.screenshot({ path: `${OUT}/desktop-events.png` })
await page.keyboard.press('Escape')
await page.waitForSelector('[data-test=events]', { state: 'detached' })
await page.keyboard.press('Control+Alt+n')
await page.waitForSelector('[data-test=events]')
await page.keyboard.press('Control+Alt+n')
await page.waitForSelector('[data-test=events]', { state: 'detached' })
await page.click('[data-test=clock]')
await page.waitForSelector('[data-test=events]')
await page.click('[data-test=win-settings] header', { position: { x: 200, y: 20 } }) // the window in front
await page.waitForSelector('[data-test=events]', { state: 'detached' })

step('minimise from the window bar, restore from the dock')
await page.click('[data-test=dock-calendar]') // back in front (the switcher left Settings there)
assert(await titleHidden('calendar'), 'no second title, even when the h1 shares a row')
await page.click('[data-test=win-calendar] button[aria-label=Minimise]')
assert(!(await shown('[data-test=win-calendar]')), 'minimised windows are hidden')
await page.click('[data-test=dock-calendar]')
await page.waitForSelector('[data-test=win-calendar]')

step('close a window')
await page.click('[data-test=win-calendar] [data-test=win-close]')
assert(!(await page.locator('[data-test=win-calendar]').count()), 'closed')

step('a link to another section opens a window instead of leaving the desktop')
await page.click('[data-test=dock-files]')
await page.waitForSelector('[data-test=win-files]')
await page.click('[data-test=win-files] a[href="#/browse"], [data-test=win-files] .q-btn:has-text("Back to the browser")')
await page.waitForSelector('[data-test=win-browse]')
assert(await shown('[data-test=desktop]'), 'still on the desktop')

step('Android opens as a phone: a tall narrow window, the screen filling its height')
// no Android on the test server: answer the ticket request as if it were set up, so the (black) phone frame lays out
await page.route('**/phe/android/ticket', (r) => r.fulfill({ contentType: 'application/json', body: JSON.stringify({ configured: true, ticket: 'test' }) }))
await page.click('[data-test=dock-android]')
await page.waitForSelector('[data-test=win-android] [data-test=android-screen]')
await page.waitForTimeout(300)
const aw = await box('[data-test=win-android]')
const frame = await box('[data-test=win-android] .ph-frame')
assert(aw.height > aw.width * 1.5, `phone-shaped window (${aw.width} x ${aw.height})`)
assert(frame.height > aw.height * 0.7 && frame.y + frame.height <= aw.y + aw.height + 1, `the screen fills the window (${JSON.stringify(frame)} in ${JSON.stringify(aw)})`)
await page.screenshot({ path: `${OUT}/desktop-android.png` })
await page.click('[data-test=win-android] [data-test=win-close]')
await page.unroute('**/phe/android/ticket')

step('the arrangement survives a reload, and "/" goes back to the desktop')
await page.goto(BASE + '/#/')
await page.waitForSelector('[data-test=desktop]')
await page.waitForSelector('[data-test=win-mail]')

step('widgets: the default ones sit under the windows; Ctrl+Alt+D shows the desktop')
for (const k of ['clock-digital', 'mail', 'agenda']) await page.waitForSelector(`[data-test=widget-${k}]`)
// the same time as the menu bar, in the same (locale) format
const hm = (t: string) => t.match(/\d{1,2}:\d\d/)?.[0]
assert.equal(hm(await text('[data-test=widget-clock-digital]')), hm(await text('[data-test=clock]')), 'the big clock and the menu bar agree')
await page.keyboard.press('Control+Alt+d')
await page.waitForSelector('[data-test=win-mail]', { state: 'hidden' })
assert(await shown('[data-test=widget-mail]'), 'with the windows minimised the widgets show')

step('View > Edit widgets: the windows step aside, add, move with the keys, remove, Done; the arrangement is kept')
await page.click('[data-test=dock-mail]') // a window open while editing
await page.waitForSelector('[data-test=win-mail]')
await page.click('.ph-menubar__menu:has-text("View")')
await page.click('[data-test=edit-widgets]')
await page.waitForSelector('[data-test=widget-sheet]')
assert(!(await page.locator('[data-test=win-mail]').count()), 'windows step aside while editing')
await page.click('[data-test=widget-add-clock-analog]')
await page.click('[data-test=widget-add-chats]')
await page.waitForSelector('[data-test=widget-clock-analog]')
await page.waitForSelector('[data-test=widget-chats]')
const before = await box('[data-test=widget-clock-analog]')
await page.focus('[data-test=widget-clock-analog]')
await page.keyboard.press('ArrowRight')
const after = await box('[data-test=widget-clock-analog]')
assert.equal(after.x - before.x, 90, 'ArrowRight moves the widget one 90 px cell')
await page.keyboard.press('ArrowLeft')
await page.keyboard.press('ArrowLeft')
assert.equal((await box('[data-test=widget-clock-analog]')).x, before.x, 'it stops at the edge of the grid')
await page.keyboard.press('ArrowRight')
await page.click('[data-test=widget-remove-agenda]')
assert(!(await page.locator('[data-test=widget-agenda]').count()), 'the ✕ removes the widget')
await page.screenshot({ path: `${OUT}/desktop-widgets-edit.png` })
await page.click('[data-test=widgets-done]')
await page.waitForSelector('[data-test=widget-sheet]', { state: 'hidden' })
await page.waitForSelector('[data-test=win-mail]')
await page.reload()
await page.waitForSelector('[data-test=widget-chats]')
assert(await shown('[data-test=widget-clock-analog]'), 'an added widget survives a reload')
assert(!(await page.locator('[data-test=widget-agenda]').count()), 'a removed widget stays removed')
assert.equal((await box('[data-test=widget-clock-analog]')).x, after.x, 'and so does its position')
// after a reload the widgets connect and load by themselves; requests made while the connection is still opening wait for it (this server
// has no hub, so they say so: not "Could not load: unknown or revoked device" from a request sent before the connection was signed in)
await page.waitForFunction(() => /not set up/.test(document.querySelector('[data-test=widget-mail]')?.textContent || ''), null, { timeout: 10000 })
assert.doesNotMatch(await text('[data-test=widget-mail]'), /Could not load/)
await page.click('.ph-menubar__menu:has-text("View")')
await page.click('[data-test=edit-widgets]')
await page.waitForSelector('[data-test=widget-sheet]')
await page.keyboard.press('Escape')
await page.waitForSelector('[data-test=widget-sheet]', { state: 'hidden' })
await page.keyboard.press('Control+Alt+d')
await page.screenshot({ path: `${OUT}/desktop-widgets.png` })

step('dark: the desktop follows the colour mode')
await page.emulateMedia({ colorScheme: 'dark' })
await page.waitForFunction(() => document.documentElement.dataset.hdMode === 'dark')
await page.screenshot({ path: `${OUT}/desktop-dark.png` })
await page.emulateMedia({ colorScheme: 'light' })

step('View > Classic view goes back to the pages, and "/" then stays classic')
await page.click('.ph-menubar__menu:has-text("View")')
await page.click('[data-test=classic-view]')
await page.waitForSelector('[data-test=desktop-view]')
await page.goto(BASE + '/#/')
await page.waitForSelector('[data-test=desktop-view]')
assert(!(await page.locator('[data-test=desktop]').count()), 'classic layout')

step('Settings > Layout switches to the desktop and back at once')
await page.goto(BASE + '/#/settings')
await page.click('[data-test=layout] button:has-text("Desktop")')
await page.waitForSelector('[data-test=win-settings]')
await page.click('[data-test=win-settings] [data-test=layout] button:has-text("Classic")')
await page.waitForSelector('[data-test=desktop-view]')
assert(!(await page.locator('[data-test=desktop]').count()), 'back to the classic layout')

step('Android on a phone: one row of named icons, and the screen keeps most of the height')
await page.setViewportSize({ width: 390, height: 844 })
await page.route('**/phe/android/ticket', (r) => r.fulfill({ contentType: 'application/json', body: JSON.stringify({ configured: true, ticket: 'test' }) }))
await page.goto(BASE + '/#/android')
await page.waitForSelector('[data-test=android-screen]')
await page.waitForTimeout(300)
const tops = new Set()
for (const b of await page.locator('[role=toolbar] .q-btn').all()) tops.add(Math.round((await b.boundingBox())?.y ?? -1))
assert.equal(tops.size, 1, 'the controls fit one row')
assert.equal(await page.getAttribute('[data-test=android-home]', 'aria-label'), 'Home')
const pf = await box('.ph-frame')
assert(pf.height > 844 * 0.6, `the screen keeps most of the height (${pf.height})`)
await page.screenshot({ path: `${OUT}/android-phone.png` })
await page.unroute('**/phe/android/ticket')

step('a small screen always gets the classic layout')
await page.setViewportSize({ width: 800, height: 900 })
await page.goto(BASE + '/#/desktop')
await page.waitForFunction(() => location.hash === '#/browse')
await page.waitForSelector('[data-test=desktop-view]', { state: 'detached' }) // no desktop button below 1024 px

assert.deepEqual(errors, [], 'no page errors')
await browser.close()
step('desktop: all checks passed')
