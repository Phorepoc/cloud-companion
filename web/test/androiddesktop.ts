// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only
// The two display shapes: a large (desktop) viewer gets a landscape display, a phone-sized window a portrait one, and back. The home app is the image's own
// (Etesian's launcher) in both: the bridge never swaps it and never installs anything. Runs against the bridge's standalone page with the hooks of android.sh.
import { chromium } from 'playwright'
import assert from 'node:assert/strict'
import fs from 'fs'

const BASE = process.env.BASE, TICKET = process.env.TICKET, OUT = process.env.OUT || '/out', QUERY = process.env.QUERY || ''
assert(BASE && TICKET, 'BASE and TICKET (a bridge ticket) are required')
async function hook(name: string) {
  for (const x of ['.done', '.out']) try { fs.unlinkSync(`${OUT}/hook-${name}${x}`) } catch (e) {}
  fs.writeFileSync(`${OUT}/hook-${name}.req`, '')
  for (let i = 0; i < 400; i++) { if (fs.existsSync(`${OUT}/hook-${name}.done`)) return fs.readFileSync(`${OUT}/hook-${name}.out`, 'utf8').trim(); await new Promise((r) => setTimeout(r, 250)) }
  throw new Error('hook ' + name + ' timed out')
}
const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
const dims = () => page.evaluate(() => { const c = (document.getElementById('screen') as HTMLCanvasElement); return { w: c.width, h: c.height } })
const waitFocus = async (re: RegExp, label: string, ms = 150000) => { // the focused window, asked of Android itself
  const t0 = Date.now()
  for (;;) { const f = await hook('gohome'); if (re.test(f)) return f; if (Date.now() - t0 > ms) throw new Error(label + ': last focus was ' + f); await page.waitForTimeout(1500) }
}
const waitShape = async (wide: boolean, label: string) => {
  const t0 = Date.now()
  for (;;) { const d = await dims(); if ((d.w > d.h) === wide) return d; if (Date.now() - t0 > 60000) throw new Error(label); await page.waitForTimeout(500) }
}
const nonblank = () => page.evaluate(() => { const c = (document.getElementById('screen') as HTMLCanvasElement), d = c.getContext('2d')!.getImageData(0, 0, c.width, c.height).data; let mn = 255, mx = 0; for (let i = 0; i < d.length; i += 388) { const v = (d[i] + d[i + 1] + d[i + 2]) / 3; if (v < mn) mn = v; if (v > mx) mx = v } return mx - mn > 20 })

await page.goto(BASE + '/?t=' + TICKET + QUERY)
await page.waitForFunction(() => (document.getElementById('info')?.textContent ?? '').includes('fps'), null, { timeout: 240000 })
await waitShape(true, 'desktop stream')
const HOME = new RegExp(process.env.HOME_RE || 'com\\.bompotis\\.etesian\\.launcher')
const t0 = Date.now()
let f = await waitFocus(HOME, 'desktop viewer: the image\'s own launcher should stay home')
console.log('desktop viewer: own launcher is home (', f.replace(/\s+/g, ' ').slice(0, 110), ') after', ((Date.now() - t0) / 1000).toFixed(0), 's')
assert.equal(await hook('foreignpkgs'), '0', 'the bridge installed an app of its own (Taskbar or a launcher)')
await page.waitForTimeout(1500)
await page.screenshot({ path: OUT + '/androiddesktop-1-home.png' })
assert(await nonblank(), 'blank screen')

await page.setViewportSize({ width: 390, height: 844 })
await waitShape(false, 'phone stream')
f = await waitFocus(HOME, 'phone viewer: the image\'s own launcher should stay home', 60000)
console.log('phone viewer: own launcher is home (', f.replace(/\s+/g, ' ').slice(0, 100), ')')
await page.waitForTimeout(1500)
await page.screenshot({ path: OUT + '/androiddesktop-2-phone.png' })
assert(await nonblank(), 'blank phone screen')

await page.setViewportSize({ width: 1280, height: 800 })
await waitShape(true, 'desktop stream again')
await waitFocus(HOME, 'back on desktop: the own launcher should still be home', 60000)
assert.equal(await hook('foreignpkgs'), '0', 'the bridge installed an app of its own')
console.log('desktop viewer again: own launcher is home')
await browser.close()
console.log('OK')
