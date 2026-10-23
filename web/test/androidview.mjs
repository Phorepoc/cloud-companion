// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
// The display follows the viewing device (desktop window -> large landscape display, phone window -> phone display, and back when the window changes), and the page's own
// context menu: what it offers follows Android's state (text field focused, selection). Runs against the bridge's standalone page with the hooks of android.sh.
import { chromium } from 'playwright'
import assert from 'node:assert/strict'
import fs from 'fs'

const BASE = process.env.BASE, TICKET = process.env.TICKET, OUT = process.env.OUT || '/out', QUERY = process.env.QUERY || ''
async function hook(name) {
  for (const x of ['.done', '.out']) try { fs.unlinkSync(`${OUT}/hook-${name}${x}`) } catch (e) {}
  fs.writeFileSync(`${OUT}/hook-${name}.req`, '')
  for (let i = 0; i < 400; i++) { if (fs.existsSync(`${OUT}/hook-${name}.done`)) return fs.readFileSync(`${OUT}/hook-${name}.out`, 'utf8').trim(); await new Promise((r) => setTimeout(r, 250)) }
  throw new Error('hook ' + name + ' timed out')
}
const browser = await chromium.launch()
const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } })
await ctx.grantPermissions(['clipboard-read', 'clipboard-write'])
const page = await ctx.newPage()
const dims = () => page.evaluate(() => { const c = document.getElementById('screen'); return { w: c.width, h: c.height } })
const waitDims = async (pred, label, ms = 30000) => {
  const t0 = Date.now()
  for (;;) { const d = await dims(); if (pred(d)) return d; if (Date.now() - t0 > ms) throw new Error(label + ': display never became the expected shape, last ' + JSON.stringify(d)); await page.waitForTimeout(400) }
}
await page.goto(BASE + '/?t=' + TICKET + QUERY)
await page.waitForFunction(() => document.getElementById('info').textContent.includes('fps'), null, { timeout: 120000 })

// 1. desktop window: a large landscape display
let d = await waitDims((x) => x.w > x.h && x.w >= 1000, 'desktop')
console.log('desktop window 1280x800: stream', d.w + 'x' + d.h, '| device display:', (await hook('wmsize')).replace(/\n/g, ' '))
await page.waitForTimeout(1500)
await page.screenshot({ path: OUT + '/androidview-1-desktop.png' })

// 2. the window becomes phone-sized: the display follows (debounced)
await page.setViewportSize({ width: 390, height: 844 })
d = await waitDims((x) => x.h > x.w && x.w < 700, 'phone')
console.log('window resized to 390x844: stream', d.w + 'x' + d.h, '| device display:', (await hook('wmsize')).replace(/\n/g, ' '))
await page.waitForTimeout(1500)
await page.screenshot({ path: OUT + '/androidview-2-phone.png' })

// 3. and back to desktop
await page.setViewportSize({ width: 1400, height: 900 })
d = await waitDims((x) => x.w > x.h && x.w >= 1000, 'desktop again')
console.log('window resized to 1400x900: stream', d.w + 'x' + d.h, '| device display:', (await hook('wmsize')).replace(/\n/g, ' '))
await page.waitForTimeout(1500)

// 4. the context menu follows Android's state
await hook('home'); await page.waitForTimeout(1000)
const center = async () => { const b = await page.locator('#screen').boundingBox(); return [b.x + b.width / 2, b.y + b.height / 2] }
const menu = async () => { // open in the middle of the screen, wait for the state reply, report which items are enabled
  const [x, y] = await center()
  await page.mouse.click(x, y, { button: 'right' })
  await page.waitForSelector('[role=menu]')
  await page.waitForTimeout(1500)
  return page.evaluate(() => Object.fromEntries([...document.querySelectorAll('[role=menu] button')].map((b) => [b.dataset.item, b.getAttribute('aria-disabled') !== 'true'])))
}
let m = await menu()
console.log('menu, nothing focused:', JSON.stringify(m))
assert.deepEqual([m.copy, m.cut, m.paste, m.selectall, m.back, m.home], [false, false, false, false, true, true])
await page.screenshot({ path: OUT + '/androidview-3-menu-idle.png' })
await page.keyboard.press('Escape')
await page.waitForFunction(() => !document.querySelector('[role=menu]'))

await hook('settingssearch'); await page.waitForTimeout(2500) // Settings, its search field tapped (focused)
await page.evaluate(() => navigator.clipboard.writeText('menu paste test'))
m = await menu()
console.log('menu, a text field focused:', JSON.stringify(m))
assert.deepEqual([m.copy, m.cut, m.paste, m.selectall], [false, false, true, true])
await page.screenshot({ path: OUT + '/androidview-4-menu-field.png' })
await page.click('[role=menu] [data-item=paste]') // Paste from the menu
await page.waitForTimeout(1500)
const ui1 = await hook('dump')
assert(ui1.includes('menu paste test'), 'Paste from the menu did not reach the text field')
console.log('Paste from the menu: the text is in the field')
{
  const [x, y] = await center()
  await page.mouse.click(x, y, { button: 'right' })
}
await page.waitForSelector('[role=menu]'); await page.waitForTimeout(1500)
await page.click('[role=menu] [data-item=selectall]')
await page.waitForTimeout(800)
await page.evaluate(() => { window.__clip = '' })
m = await menu()
console.log('menu, after Select all:', JSON.stringify(m))
assert(m.copy && m.cut, 'Copy / Cut should be offered when text is selected')
await page.screenshot({ path: OUT + '/androidview-5-menu-selection.png' })
await page.click('[role=menu] [data-item=copy]')
await page.waitForFunction(() => (window.__clip || '').includes('menu paste test'), null, { timeout: 8000 })
console.log('Copy from the menu: the phone clipboard text reached the page:', await page.evaluate(() => window.__clip))
await browser.close()
console.log('OK')
