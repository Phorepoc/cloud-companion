// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Playwright test (Firefox) of Feeds, against a live server that can reach this container's port 8099 (feeds.sh):
//   BASE=http://phecloud:8080 CODE=ABCD-EFGH OUT=/out node --experimental-strip-types feeds.ts
//
// Through the screen: add two feeds with +, browse one feed and all posts, read a post in the reader (it does not open its
// page), follow a link from the post, keep read marks over a reload, then a desktop widget for one feed opens a post in Feeds.

import { firefox, type Page } from 'playwright'
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { openApp } from './chrome.ts'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const CODE = process.env.CODE
const OUT = process.env.OUT || '.'
assert(CODE, 'CODE (a pairing code from `phecloud paircode --admin`) is required')

const rss = (title: string, items: string) => `<?xml version="1.0"?><rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel><title>${title}</title>${items}</channel></rss>`
const FEEDS: Record<string, string> = {
  '/one.xml': rss('Feed One', `<item><title>Tram line opens</title><link>http://localhost:8099/tram</link><pubDate>Fri, 06 Nov 2026 10:00:00 GMT</pubDate>
    <content:encoded><![CDATA[<p>The <b>new line</b> runs to the harbour. <a href="http://localhost:8099/map">See the map</a><script>window.pwned = 1</script></p>]]></content:encoded></item>
    <item><title>Library hours</title><link>http://localhost:8099/library</link><pubDate>Thu, 05 Nov 2026 10:00:00 GMT</pubDate><description>Longer weekends.</description></item>`),
  '/two.xml': rss('Feed Two', `<item><title>Northern lights tonight</title><link>http://localhost:8099/sky</link><pubDate>Fri, 06 Nov 2026 09:00:00 GMT</pubDate><description>Look north after ten.</description></item>`),
}
const srv = createServer((req, res) => {
  const body = FEEDS[req.url || '']
  res.writeHead(body ? 200 : 404, { 'content-type': body ? 'application/rss+xml' : 'text/plain' })
  res.end(body || 'no')
}).listen(8099)

const browser = await firefox.launch()
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, serviceWorkers: 'block' })
const page = await ctx.newPage()
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
const popups: string[] = []
ctx.on('page', (p) => popups.push(p.url() || 'about:blank'))
const t0 = Date.now()
const step = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)
const until = async (p: Page, f: () => boolean, what: string) => { for (let i = 0; !(await p.evaluate(f)); i++) { assert(i < 150, what); await p.waitForTimeout(200) } }
const tool = (label: string) => page.click(`button[aria-label="${label}"]:visible`)

step('pair and open Feeds')
await page.goto(BASE + '/#/pair')
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
await until(page, () => window.__phe?.state.status === 'online', 'online')
await page.goto(BASE + '/#/feeds')
await page.waitForSelector('[data-test=feeds]')

step('+ adds two feeds')
for (const f of ['one', 'two']) {
  await tool('Add feed')
  await page.fill('[data-test=feeds-add-url]', `http://localhost:8099/${f}.xml`)
  await page.click('[data-test=feeds-add-save]')
  await page.waitForSelector('[data-test=feeds-add]', { state: 'detached' })
}
await page.waitForSelector('.x-fl__row:has-text("Feed Two")')
assert.match(await page.textContent('.x-fl__row[data-feed=all]') ?? '', /3 unread posts/)
await page.screenshot({ path: `${OUT}/feeds-1-list.png` })

step('one feed shows only its posts')
await page.click('.x-fl__row:has-text("Feed Two")')
assert.deepEqual(await page.locator('.x-fp__row b').allTextContents(), ['Northern lights tonight'])
await tool('Back')

step('a post opens in the reader, not its page; its content is there, made safe')
await page.click('.x-fl__row[data-feed=all]')
await page.click('.x-fp__row:has-text("Tram line opens")')
await page.waitForSelector('.x-fr__title')
assert.equal(await page.textContent('.x-fr__title'), 'Tram line opens')
assert.equal(await page.textContent('.x-fr__body b'), 'new line')
assert.equal(await page.evaluate(() => (window as unknown as { pwned?: number }).pwned), undefined, 'no script from the feed ran')
assert.deepEqual(popups, [], 'opening a post opened no page')
assert(page.url().includes('#/feeds'), 'still in Feeds')
await page.screenshot({ path: `${OUT}/feeds-2-reader.png` })

step('a link in the post opens in a new tab')
const [tab] = await Promise.all([ctx.waitForEvent('page'), page.click('.x-fr__body a')])
await tab.waitForLoadState().catch(() => {})
assert.equal(tab.url(), 'http://localhost:8099/map')
await tab.close()

step('back in the list the post is read; that survives a reload')
await tool('Back')
assert(!(await page.locator('.x-fp__row:has-text("Tram line opens")').getAttribute('class'))?.includes('is-unread'), 'read after opening')
await page.waitForTimeout(1200) // the read mark is sent shortly after
await page.reload()
await page.waitForSelector('.x-fl__row[data-feed=all]')
await until(page, () => /2 unread posts/.test(document.querySelector('.x-fl__row[data-feed=all]')?.textContent || ''), 'the read mark is kept on the server')

step('desktop: a feeds widget for one feed; its post opens in the Feeds window')
await openApp(page, 'Desktop')
await page.waitForSelector('[data-test=desktop]')
await page.click('.x-mbar__menu:has-text("View")')
await page.click('text=Edit desktop')
await page.click('[data-test=widget-add-feeds]')
await page.click('[data-test=widget-setting-feed]:has-text("Feed Two")')
await page.click('[data-test=widgets-done]')
await page.click('.x-mbar__menu:has-text("View")')
await page.click('text=Show the desktop')
assert.deepEqual(await page.locator('[data-test=widget-feeds] .ph-wg__rsst').evaluateAll((e) => e.map((x) => x.firstChild?.textContent?.trim())), ['Northern lights tonight'])
await page.screenshot({ path: `${OUT}/feeds-3-widget.png` })
// a widget's list: no scrollbar, and the position indicator only while it scrolls
const list = page.locator('[data-test=widget-feeds] .ph-scrollthumb-list').first()
await list.waitFor()
assert.equal(await list.evaluate((e) => getComputedStyle(e).scrollbarWidth), 'none', 'a scrollbar on a widget list')
const thumb = list.locator('xpath=..').locator('.ph-scrollthumb')
assert.equal(await thumb.evaluate((e) => getComputedStyle(e).opacity), '0', 'the indicator shows at rest')
await list.evaluate((e) => { const t = document.createElement('div'); t.style.cssText = 'height:2000px;flex:none'; e.appendChild(t); e.scrollTop = 200 })
await page.waitForTimeout(400)
assert(Number(await thumb.evaluate((e) => getComputedStyle(e).opacity)) > 0.5, 'the indicator does not show while scrolling')
await page.waitForTimeout(1300)
assert.equal(await thumb.evaluate((e) => getComputedStyle(e).opacity), '0', 'the indicator stays after scrolling')
await page.click('[data-test=widget-feed]')
await page.waitForSelector('.x-win[data-win=feeds] .x-fr__title')
assert.equal(await page.textContent('.x-win[data-win=feeds] .x-fr__title'), 'Northern lights tonight')
assert.deepEqual((popups as string[]).filter((u) => !u.includes('/map')), [], 'the widget opened no page')
await page.screenshot({ path: `${OUT}/feeds-4-window.png` })
// the post is read now: its dot in the widget is gone, not an empty ring
const dot = page.locator('[data-test=widget-feed] .ph-wg__rssdot').first()
for (let i = 0; i < 25 && (await dot.evaluate((e) => getComputedStyle(e).visibility)) !== 'hidden'; i++) await page.waitForTimeout(200)
assert.equal(await dot.evaluate((e) => getComputedStyle(e).visibility), 'hidden', 'a read post has no dot in the widget')

step('Remove feed waits for the toast: Undo keeps it, otherwise it goes, also after a reload')
await page.goto(`${BASE}/#/feeds`); await page.reload(); await page.waitForSelector('.x-fl__row:has-text("Feed Two"):visible')
await page.click('.x-fl__row:has-text("Feed Two"):visible')
await tool('Remove feed')
await page.waitForSelector('.x-fl__row:has-text("Feed One"):visible')
assert.equal(await page.locator('.x-fl__row:has-text("Feed Two"):visible').count(), 0, 'gone from the list at once')
await page.click('[data-test=toast-action]:has-text("Undo")')
await page.waitForSelector('.x-fl__row:has-text("Feed Two"):visible')
await page.click('.x-fl__row:has-text("Feed Two"):visible')
await tool('Remove feed')
await page.waitForTimeout(4800)
await page.reload(); await page.waitForSelector('.x-fl__row:has-text("Feed One"):visible')
await page.waitForTimeout(1000)
assert.equal(await page.locator('.x-fl__row:has-text("Feed Two"):visible').count(), 0, 'removed on the server')
// a reload while the toast is up still removes it (before, the delete was dropped with the page and the feed came back)
await page.click('.x-fl__row:has-text("Feed One"):visible')
await tool('Remove feed')
await page.waitForTimeout(300)
await page.reload(); await page.waitForSelector('.x-fl')
await page.waitForTimeout(1500); await page.reload(); await page.waitForSelector('.x-fl')
await page.waitForTimeout(1000)
assert.equal(await page.locator('.x-fl__row:has-text("Feed One"):visible').count(), 0, 'removed although the page was reloaded mid-toast')
assert.deepEqual(errors, [], 'no page errors')
step('ok')
await browser.close()
srv.close()
