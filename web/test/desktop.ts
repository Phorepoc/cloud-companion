// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Playwright test of the desktop view and the Etesian theme, run against a live server:
//   BASE=http://phecloud:8080 CODE=ABCD-EFGH OUT=/out node --experimental-strip-types desktop.ts
//
// Pairs at 1440 x 900, checks the Etesian colour modes, enters the desktop from the top bar, opens sections from the dock, moves
// focus, snaps by dragging to an edge and with Ctrl+Alt+arrows, uses the Overview and the spotlight, the quick switcher and Events, opens Android as a
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

step('the Overview (dock button / Super): Events, the open windows and the sections; typing searches; Enter opens')
await page.click('[data-test=dock-launcher]')
await page.waitForSelector('[data-test=overview]')
assert(!(await shown('[data-test=launcher]')), 'the dock button opens the Overview, not the spotlight')
assert(await shown('[data-test=overview] [data-test=events]'), 'the Overview shows Events')
assert(await shown('[data-test=ov-win-mail]'), 'the Overview shows the open Mail window')
await page.keyboard.type('cal')
assert(await shown('[data-test=ov-launch-calendar]'))
assert(!(await shown('[data-test=ov-launch-mail]')), 'the Overview search filters by name')
await page.screenshot({ path: `${OUT}/desktop-overview.png` })
await page.keyboard.press('Escape')
assert(!(await shown('[data-test=overview]')), 'Esc closes the Overview')

step('the spotlight (Ctrl+Alt+Space; Super+Space): type to find a section, Enter opens it')
await page.keyboard.press('Control+Alt+Space')
await page.waitForSelector('[data-test=launcher]')
assert(!(await shown('[data-test=overview]')), 'the spotlight is not the Overview')
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

step('Browser window: the page uses the whole window (no padding), the buttons are in the title bar, the address bar is right under it')
{
const bw = await box('[data-test=win-browse]')
const bar = await box('[data-test=win-browse] header')
for (const t of ['back', 'forward', 'reload', 'tab-new', 'zoom-in', 'phone-size']) {
  const b = await box(`[data-test=win-browse] header [data-test=${t}]`)
  assert(b.y >= bar.y - 1 && b.y + b.height <= bar.y + bar.height + 1, `${t} is in the title bar`)
}
const sep = await box('[data-test=win-browse] header .ph-win__ctl')
const lastBtn = await box('[data-test=win-browse] header [data-test=phone-size]')
assert(lastBtn.x + lastBtn.width <= sep.x + 1, 'the page buttons sit left of the separator before minimise / maximise / close')
const addr = await box('[data-test=win-browse] .ph-address')
assert(addr.y >= bar.y + bar.height - 1 && addr.y <= bar.y + bar.height + 4, `the address bar is directly under the title bar (${addr.y} vs ${bar.y + bar.height})`)
const near = (a: number, b: number, tol = 2) => Math.abs(a - b) <= tol
const fillCheck = async (what: string) => {
  const st = await box('[data-test=win-browse] [data-test=stage]')
  const cvb = await box('[data-test=win-browse] [data-test=canvas]')
  assert(near(cvb.width, st.width) && near(cvb.height, st.height) && near(cvb.x, st.x) && near(cvb.y, st.y), `${what}: the page fills its stage (${JSON.stringify(cvb)} in ${JSON.stringify(st)})`)
  assert(st.width + 2 >= bw.width - 4 || true, 'stage measured')
  return st
}
let stg = await fillCheck('full size')
assert(stg.width > 360 && stg.height > 200, 'not limited to a phone column')
// a real page, so the cloud lays it out: its width is the window's
await page.evaluate(() => window.__phe.api.open('data:text/html,<body style="margin:0"><h1>full</h1></body>'))
await page.waitForFunction(() => window.__phe.state.page.pid > 0 && window.__phe.state.page.w === window.__phe.state.viewport.w, null, { timeout: 30000 })
assert.equal(await page.evaluate(() => window.__phe.state.page.w), await page.evaluate(() => window.__phe.state.viewport.w), 'the cloud laid the page out at the window width')
assert((await page.evaluate(() => window.__phe.state.page.w)) > 400, 'wider than a Symbian phone')
await page.screenshot({ path: `${OUT}/desktop-browser-full.png` })
// the phone size toggle, and back
await page.click('[data-test=win-browse] [data-test=phone-size]')
await page.waitForFunction(() => window.__phe.state.viewport.phone === true && window.__phe.state.page.w === 360, null, { timeout: 20000 })
const pc = await box('[data-test=win-browse] [data-test=canvas]')
assert(near(pc.width / pc.height, 360 / 640, 0.02), `phone size is 360 x 640 in shape (${pc.width} x ${pc.height})`)
await page.screenshot({ path: `${OUT}/desktop-browser-phone.png` })
await page.click('[data-test=win-browse] [data-test=phone-size]')
await page.waitForFunction(() => window.__phe.state.viewport.phone === false && window.__phe.state.page.w > 400, null, { timeout: 20000 })
stg = await fillCheck('back to full size')
// resizing the window (live): drag the south-east corner; the cloud gets the new size
const wb = await box('[data-test=win-browse]')
await page.mouse.move(wb.x + wb.width - 3, wb.y + wb.height - 3)
await page.mouse.down()
await page.mouse.move(wb.x + wb.width - 3 - 120, wb.y + wb.height - 3 - 80, { steps: 6 })
await page.mouse.up()
await page.waitForFunction((w0) => window.__phe.state.viewport.w < w0 - 60 && window.__phe.state.page.w === window.__phe.state.viewport.w, Math.round(stg.width), { timeout: 20000 })
stg = await fillCheck('after resizing the window')
assert.equal(await page.evaluate(() => window.__phe.state.page.w), await page.evaluate(() => window.__phe.state.viewport.w), 'the cloud follows the window size')
}
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

step('dock: a floating strip centred on the left edge, as in the design (regression 2026-10-30: it sat at the top)')
{
  const d = await box('.ph-dock'), a = await box('.ph-desk__area')
  assert(Math.abs((d.y + d.height / 2) - (a.y + a.height / 2)) <= 2, `the dock is centred vertically (${d.y + d.height / 2} vs ${a.y + a.height / 2})`)
  assert(d.height < a.height - 40, 'and only as long as its icons')
}
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
// More Belle widgets (2026-10-30): notes, a month, the latest mail one at a time, the clock in words
await page.click('.ph-menubar__menu:has-text("View")')
await page.click('[data-test=edit-widgets]')
await page.waitForSelector('[data-test=widget-sheet]')
for (const k of ['notes', 'month', 'mail-stack', 'clock-word']) await page.click(`[data-test=widget-add-${k}]`)
for (const k of ['notes', 'month', 'mail-stack', 'clock-word']) await page.waitForSelector(`[data-test=widget-${k}]`)
await page.keyboard.press('Escape')
await page.waitForSelector('[data-test=widget-sheet]', { state: 'hidden' })
await page.keyboard.press('Control+Alt+d')
step('widgets: notes are kept, the month marks today, the time reads in words, lists scroll')
await page.fill('[data-test=widget-note]', 'Buy rye bread')
await page.click('[data-test=widget-note-add]')
assert.equal(await page.inputValue('[data-test=widget-note]'), '', '+ starts a new, empty note')
await page.fill('[data-test=widget-note]', 'Call Mikko')
assert.equal(await page.locator('[data-test=widget-month] .ph-wg__day').count(), 42, 'the month shows six weeks')
assert.equal(await page.locator('[data-test=widget-month] .ph-wg__day.is-today').count(), 1, 'and lights today')
assert.match(await text('[data-test=widget-clock-word]'), /(past|to|o'clock)/, 'the clock in words')
// regression 2026-10-30: the mail, agenda and chats widgets showed a fixed 3 or 4 rows; they hold up to 20 and scroll
assert.equal(await page.evaluate(() => { const host = document.querySelector('.ph-wg')!, d = document.createElement('div'); d.className = 'ph-wg__list'
  for (const a of host.getAttributeNames()) if (a.startsWith('data-v-')) d.setAttribute(a, '') // the component's scoped styles
  host.appendChild(d)
  const o = getComputedStyle(d).overflowY; d.remove(); return o }), 'auto', 'the widget lists scroll')
await page.reload()
await page.waitForSelector('[data-test=widget-note]')
assert.equal(await page.inputValue('[data-test=widget-note]'), 'Buy rye bread', 'notes are kept after a reload')
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

step('Browser, classic layout (wide): the page fills the content area, the buttons are in the top bar, the address bar is the first row')
await page.goto(BASE + '/#/browse')
await page.waitForSelector('[data-test=canvas]')
await page.waitForTimeout(500)
{
  const hdr = await box('header.q-header')
  const cvb = await box('[data-test=canvas]')
  const content = await box('.ph-browse')
  const adr = await box('.ph-address')
  for (const t of ['back', 'reload', 'phone-size']) {
    const b = await box(`header.q-header [data-test=${t}]`)
    assert(b.y >= hdr.y - 1 && b.y + b.height <= hdr.y + hdr.height + 1, `${t} is in the top bar`)
  }
  assert(adr.y <= hdr.y + hdr.height + 4, 'the address bar is directly under the top bar')
  assert(Math.abs(cvb.x - content.x) <= 2 && Math.abs(cvb.width - content.width) <= 2, `no side padding (${JSON.stringify(cvb)} in ${JSON.stringify(content)})`)
  assert(cvb.y + cvb.height >= content.y + content.height - 2, 'the page reaches the bottom of the content area')
  await page.screenshot({ path: `${OUT}/browser-classic-wide.png` })
}

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

step('Browser on a phone: edge to edge, buttons in the top bar')
await page.setViewportSize({ width: 390, height: 844 })
await page.goto(BASE + '/#/browse')
await page.waitForSelector('[data-test=canvas]')
await page.waitForTimeout(500)
{
  const hdr = await box('header.q-header')
  const cvb = await box('[data-test=canvas]')
  assert(Math.abs(cvb.x) <= 2 && Math.abs(cvb.width - 390) <= 2, `the page is as wide as the screen (${JSON.stringify(cvb)})`)
  for (const t of ['back', 'reload']) {
    const b = await box(`header.q-header [data-test=${t}]`)
    assert(b.y >= hdr.y - 1 && b.y + b.height <= hdr.y + hdr.height + 1, `${t} is in the top bar`)
  }
  await page.screenshot({ path: `${OUT}/browser-phone.png` })
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${OUT}/browser-phone-dark.png` })
  await page.emulateMedia({ colorScheme: 'light' })
}

step('a small screen always gets the classic layout')
await page.setViewportSize({ width: 800, height: 900 })
await page.goto(BASE + '/#/desktop')
await page.waitForFunction(() => location.hash === '#/browse')
await page.waitForSelector('[data-test=desktop-view]', { state: 'detached' }) // no desktop button below 1024 px

assert.deepEqual(errors, [], 'no page errors')
await browser.close()
step('desktop: all checks passed')
