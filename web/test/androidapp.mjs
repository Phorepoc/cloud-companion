// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
// The web app's own Android page (#/android): a native component, not an iframe. Pairs as admin, streams (VP8 in this headless Chromium), checks layout at phone and
// desktop widths in light and dark, input through the page, rotation (the frame follows), the app's own controls, reconnect. Screenshots: OUT/androidapp-*.png
import { chromium } from 'playwright'
import assert from 'node:assert/strict'
import fs from 'fs'

const BASE = process.env.BASE, CODE = process.env.CODE, OUT = process.env.OUT || '/out'
async function hook(name) {
  for (const x of ['.done', '.out']) try { fs.unlinkSync(`${OUT}/hook-${name}${x}`) } catch (e) {}
  fs.writeFileSync(`${OUT}/hook-${name}.req`, '')
  for (let i = 0; i < 200; i++) { if (fs.existsSync(`${OUT}/hook-${name}.done`)) return fs.readFileSync(`${OUT}/hook-${name}.out`, 'utf8'); await new Promise((r) => setTimeout(r, 250)) }
  throw new Error('hook ' + name + ' timed out')
}
const browser = await chromium.launch()
const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } })
const page = await ctx.newPage()
const problems = []
page.on('pageerror', (e) => problems.push('pageerror: ' + e.message))
page.on('console', (m) => { if (m.type() === 'error') problems.push('console: ' + m.text()) })

await page.goto(BASE)
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online')
await page.goto(BASE + '/#/android?codec=vp8')
await page.waitForFunction(() => document.querySelector('[data-test=android-state]')?.textContent.includes('Connected'), null, { timeout: 90000 })
console.log('the app page is live:', (await page.textContent('[data-test=android-state]')).trim())

const canvas = () => page.evaluate(() => {
  const c = document.querySelector('[data-test=android-screen]'), x = c.getContext('2d'), d = x.getImageData(0, 0, c.width, c.height).data
  let min = 255, max = 0
  for (let i = 0; i < d.length; i += 4 * 97) { const v = (d[i] + d[i + 1] + d[i + 2]) / 3; if (v < min) min = v; if (v > max) max = v }
  return { w: c.width, h: c.height, range: max - min, hash: Array.from(d.filter((_, i) => i % 4001 === 0)).join(',') }
})
const geometry = () => page.evaluate(() => {
  const f = document.querySelector('.ph-frame').getBoundingClientRect(), c = document.querySelector('[data-test=android-screen]')
  return { fw: f.width, fh: f.height, ft: f.top, fb: f.bottom, vw: innerWidth, vh: innerHeight, scrollW: document.documentElement.scrollWidth, ar: c.width / c.height }
})
async function shots(name) {
  for (const scheme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme: scheme }); await page.waitForTimeout(400)
    await page.screenshot({ path: `${OUT}/androidapp-${name}-${scheme}.png` })
  }
  await page.emulateMedia({ colorScheme: 'light' })
}
const sizes = {}
async function layout(label) {
  await page.waitForTimeout(600)
  const g = await geometry(), c = await canvas()
  assert(c.range > 30, label + ': the canvas looks blank')
  assert(g.scrollW <= g.vw + 1, label + ': the page scrolls sideways (' + g.scrollW + ' > ' + g.vw + ')')
  assert(g.fw <= g.vw + 1, label + ': the frame is wider than the screen')
  assert(Math.abs(g.fw / g.fh - g.ar) < 0.03, label + ': the frame is not the stream shape (' + (g.fw / g.fh).toFixed(3) + ' vs ' + g.ar.toFixed(3) + ')')
  console.log(`${label}: frame ${g.fw.toFixed(0)}x${g.fh.toFixed(0)} in ${g.vw}x${g.vh}, stream ${c.w}x${c.h}`)
  sizes[label] = g
  return g
}

// desktop, portrait stream
let g = await layout('desktop portrait')
assert(g.fb <= g.vh + 40, 'the whole screen should be (nearly) visible without scrolling on desktop')
await shots('1-desktop-portrait')

// the app's own controls work: Home, then a swipe on the canvas
await page.click('[data-test=android-home]'); await page.waitForTimeout(1200)
const h0 = (await canvas()).hash
const box = await page.locator('[data-test=android-screen]').boundingBox()
await page.mouse.move(box.x + box.width / 2, box.y + box.height * 0.9); await page.mouse.down()
for (let i = 1; i <= 8; i++) { await page.mouse.move(box.x + box.width / 2, box.y + box.height * (0.9 - 0.65 * i / 8)); await page.waitForTimeout(20) }
await page.mouse.up(); await page.waitForTimeout(1500)
assert.notEqual((await canvas()).hash, h0, 'a swipe on the page canvas did not change the screen')
console.log('input through the page: ok')
await page.click('[data-test=android-home]'); await page.waitForTimeout(1000)

// phone width
await page.setViewportSize({ width: 390, height: 844 }); await page.waitForTimeout(800)
await layout('phone portrait')
await shots('2-phone-portrait')

// rotation follows the device
await hook('settings'); await page.waitForTimeout(2000) // the home screen itself does not rotate; an app does
await hook('rot1'); await page.waitForTimeout(3500)
await layout('phone landscape'); await shots('3-phone-landscape')
await page.setViewportSize({ width: 1280, height: 800 }); await page.waitForTimeout(800)
g = await layout('desktop landscape'); assert(g.fw > g.fh, 'landscape frame should be wider than tall')
await shots('4-desktop-landscape')
await hook('rot0'); await page.waitForTimeout(3500)
await hook('home'); await page.waitForTimeout(1000)
g = await layout('desktop portrait again'); assert(g.fh > g.fw)

// the controls: sound toggles, install dialog, APK upload through the app, reconnect
await page.click('[data-test=android-sound]')
assert((await page.textContent('[data-test=android-sound]')).includes('Sound on'))
await page.click('[data-test=android-sound]')
await page.click('[data-test=android-install]'); await page.waitForSelector('[data-test=android-install-dialog]')
await page.setInputFiles('[data-test=android-apk]', '/t/markor.apk')
await page.waitForFunction(() => /installed|failed/.test(document.querySelector('[data-test=android-install-note]').textContent), null, { timeout: 120000 })
const inst = await page.textContent('[data-test=android-install-note]')
console.log('install through the app:', inst.trim())
assert(inst.includes('installed. Success'))
await page.screenshot({ path: `${OUT}/androidapp-5-install-dialog.png` })
await page.keyboard.press('Escape'); await page.waitForTimeout(400)
await page.click('[data-test=android-reconnect]')
await page.waitForFunction(() => document.querySelector('[data-test=android-state]')?.textContent.includes('Connected'), null, { timeout: 60000 })
assert((await canvas()).range > 30)
console.log('reconnect: live again')
// a viewer replaced by another one shows the closed state with the reconnect button (open a second page)
const p2 = await ctx.newPage()
await p2.goto(BASE + '/#/android?codec=vp8')
await p2.waitForFunction(() => document.querySelector('[data-test=android-state]')?.textContent.includes('Connected'), null, { timeout: 60000 })
await page.waitForFunction(() => !document.querySelector('[data-test=android-state]').textContent.includes('Connected'), null, { timeout: 20000 })
const st = (await page.textContent('[data-test=android-state]')).trim(); console.log('first viewer after the second took over:', st); assert(st.includes('took over'))
await page.screenshot({ path: `${OUT}/androidapp-6-replaced.png` })
await p2.close()
assert.deepEqual(problems.filter((p) => !/favicon|WebSocket connection/.test(p)), [], 'console problems')
await browser.close()
console.log('OK')
