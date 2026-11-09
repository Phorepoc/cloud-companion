// SPDX-License-Identifier: Apache-2.0
// What a person does on the Browser page, against real Firefox, with the web app's real CSP.
import { chromium } from 'playwright'
import assert from 'node:assert/strict'
const { BASE, CODE, CODE2, OUT } = process.env as Record<string, string>
const SITE = 'http://site/'
const b = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const ctx = await b.newContext({ viewport: { width: 1280, height: 860 } })
const p = await ctx.newPage()
const errs: string[] = []
p.on('pageerror', (e) => errs.push(e.message))
p.on('console', (m) => { if (m.type() === 'error') errs.push(m.text()) })
const step = (s: string) => console.log('ok -', s)
const addr = '.x-bf__row input'
const until = async (f: () => Promise<boolean>, what: string, ms = 15000) => { const t = Date.now(); for (;;) { if (await f()) return; assert(Date.now() - t < ms, 'timed out: ' + what); await p.waitForTimeout(50) } }
const live = () => until(() => p.evaluate(() => { const v = document.querySelector('[data-test=screen] video') as HTMLVideoElement | null; return !!v && v.videoWidth > 0 && v.currentTime > 0 }), 'live video', 30000)
const value = () => p.inputValue(addr)
const title = () => p.locator('[data-test=title]').textContent()
const tabCount = () => p.evaluate(() => (window as any).__pheBrowser.tabs.length as number)
const more = async (t: string) => { await p.click(".x-bf__row [aria-label^='More']"); await p.click(`[data-test=${t}]`) }
// the video element fills the stage, the picture is centred in it
const at = async (x: number, y: number) => { const v = await p.locator('[data-test=screen] video').boundingBox(); assert(v); const s = await p.evaluate(() => { const e = document.querySelector('[data-test=screen] video') as HTMLVideoElement; return [e.videoWidth, e.videoHeight] }); const k = Math.min(v.width / s[0], v.height / s[1]); return [v.x + (v.width - s[0] * k) / 2 + x * k, v.y + (v.height - s[1] * k) / 2 + y * k] as const }
const go = async (u: string) => { await p.fill(addr, u); await p.press(addr, 'Enter') }
try {
  await p.goto(BASE + '/#/pair'); await p.fill('[data-test=code]', CODE); await p.click('[data-test=pair]')
  await p.waitForSelector('[data-test=browser-frame]', { timeout: 30000 })
  const trace = () => p.evaluate(() => { const t0 = Date.now(); let last = ''; setInterval(() => { const v = document.querySelector('[data-test=screen] video') as HTMLVideoElement | null; const s = document.querySelector('[data-test=browser-connecting]') ? 'overlay' : 'clear'; const n = s + ' ' + (v ? v.videoWidth + 'x' + v.videoHeight + ' ' + (v.currentTime > 0) + ' ' + v.readyState : 'novideo'); if (n !== last) { console.log('STATE', Date.now() - t0, n); last = n } }, 100) })
  p.on('console', (m) => { if (m.text().startsWith('STATE')) console.log(m.text()) })
  await p.goto(BASE + '/#/browse'); await trace(); await live(); step('the picture is live in the real page')
  await go(SITE + 'index.html')
  await until(async () => (await title()) === 'probe one', 'title'); step('address bar loads a page, title follows')
  await p.waitForTimeout(800)
  await p.keyboard.type('typed through our page', { delay: 30 }); await p.waitForTimeout(800)
  await p.screenshot({ path: OUT + '/browser-typed.png' }); step('keys go to the page straight after the address bar (see the screenshot)')
  await p.mouse.move(...await at(60, 130)); await p.waitForTimeout(300); await p.mouse.down(); await p.mouse.up()
  await until(async () => (await value()).includes('two.html'), 'link updates address'); step('a link in the stream updates the address bar')
  await p.click(".x-bf__row [aria-label='Back']"); await until(async () => (await title()) === 'probe one', 'back'); step('Back')
  await p.click(".x-bf__row [aria-label='Forward']"); await until(async () => (await title()) === 'probe two', 'forward'); step('Forward')
  await p.reload(); await trace(); await live(); await until(async () => (await title()) === 'probe two', 'tab back after reload'); step('reloading our page reconnects and restores the tab')
  assert.deepEqual(errs.filter((e) => /Content Security Policy/.test(e)), [], 'CSP'); step('no CSP errors')
  const n = await tabCount()
  for (let i = 0; i < 5; i++) { await more('tab-new'); await p.waitForTimeout(150) }
  await until(async () => (await tabCount()) === n + 5, 'five new tabs, more than the old limit'); step('five quick new tabs, past the old limit of three')
  await more('other-devices'); await p.waitForSelector('[data-test=others-dialog]')
  for (let i = 0; i < 5; i++) { await p.locator('[data-test=others-dialog] [aria-label^=Close]').last().click(); await p.waitForTimeout(150) }
  await until(async () => (await tabCount()) === n, 'closing them'); await p.keyboard.press('Escape'); step('closing them from Tabs on your devices')
  await p.waitForTimeout(500); await p.click(".x-bf__row [aria-label^='More']"); await p.click('[data-test=zoom-in]')
  await until(async () => (await p.textContent('[data-test=zoom-pct]')) === '125%', 'zoom'); await p.click('[data-test=zoom-out]'); await p.keyboard.press('Escape'); step('Zoom')
  // Desktop site off: the phone version of sites, in the request and to the page's scripts; on again: the desktop one
  await go(SITE + 'ua'); await until(async () => (await title()) === 'header-desktop script-desktop', 'desktop ua')
  await more('desktop-site'); await until(async () => (await title()) === 'header-mobile script-mobile', 'mobile ua')
  await more('desktop-site'); await until(async () => (await title()) === 'header-desktop script-desktop', 'desktop ua again'); step('Desktop site switches the tab between the phone and desktop versions')
  // Send to device: a second device (its own browser context) is asked, and opens the page in the Browser
  const ctx2 = await b.newContext({ viewport: { width: 400, height: 800 } }); const q = await ctx2.newPage()
  await q.goto(BASE + '/#/pair'); await q.fill('[data-test=code]', CODE2); await q.click('[data-test=pair]'); await q.waitForURL(/#\/(?!pair)/, { timeout: 30000 })
  await go(SITE + 'two.html'); await until(async () => (await title()) === 'probe two', 'two')
  await more('send-device'); await p.locator('[data-test=send-dialog] [data-test^=send-btn-]').first().click({ timeout: 10000 })
  await q.waitForSelector('[data-test=sent-dialog]', { timeout: 10000 }); assert.match(await q.textContent('[data-test=sent-dialog]') || '', /two\.html|probe two/)
  const before = await tabCount()
  await q.click('[data-test=sent-open]'); await q.waitForURL(/#\/browse/)
  await until(async () => (await tabCount()) === before + 1, 'the sent page opens in a new tab'); step('Send to device: the other device asks, and opens it in the Browser')
  await ctx2.close()
  // Settings, Browser: the number of tabs kept loaded follows the memory; set to 2, the others unload and come back when picked
  const loaded = () => p.evaluate(() => (window as any).__pheBrowser.tabs.filter((t: any) => !t.asleep).length as number)
  await p.goto(BASE + '/#/settings/webbrowser'); await p.waitForSelector('[data-test=bset-memory]')
  await until(async () => /GB|MB/.test(await p.textContent('[data-test=bset-memory]') || ''), 'memory shown')
  assert.match(await p.textContent('[data-test=bset-auto]') || '', /\d+ tabs loaded at most/); step('Settings shows the memory and the automatic number')
  await p.click('[data-test=bset-auto-switch]'); await p.fill('[data-test=bset-number] input', '2'); await p.click('[data-test=bset-save]')
  await until(async () => /loaded/.test(await p.textContent('[data-test=bset-open]') || ''), 'saved')
  await p.goto(BASE + '/#/browse'); await live()
  for (const u of ['index.html', 'two.html', 'ua']) { await more('tab-new'); await p.waitForTimeout(400); await go(SITE + u); await p.waitForTimeout(1200) }
  await until(async () => (await loaded()) <= 2, 'at most two loaded'); assert((await tabCount()) >= 4, 'no tab was closed'); step('above the number, background tabs unload and stay open')
  const sleeper = await p.evaluate(() => (window as any).__pheBrowser.tabs.find((t: any) => t.asleep)?.id as number)
  await p.locator(`.x-bf [data-tab-id="${sleeper}"], .x-bf [data-id="${sleeper}"]`).first().click({ timeout: 3000 }).catch(async () => { await more('other-devices'); await p.locator(`[data-test=fx-tab-${sleeper}] [aria-label='Open here']`).click() })
  await until(() => p.evaluate((id) => !(window as any).__pheBrowser.tabs.find((t: any) => t.id === id)?.asleep, sleeper), 'picked tab loads again'); step('an unloaded tab loads again when picked')
  await p.goto(BASE + '/#/settings/webbrowser'); await p.waitForSelector('[data-test=bset-auto-switch]'); await p.click('[data-test=bset-auto-switch]')
  await until(async () => /from the memory/.test(await p.textContent('[data-test=bset-auto]') || ''), 'auto again'); step('back to automatic')
  await p.goto(BASE + '/#/browse'); await live()
  await p.screenshot({ path: OUT + '/browser-end.png' })
  console.log('BROWSER PASS')
} catch (e) { await p.screenshot({ path: OUT + '/browser-fail.png' }).catch(() => {}); console.log(errs.slice(-8).join('\n').slice(0, 900)); throw e } finally { await b.close() }
