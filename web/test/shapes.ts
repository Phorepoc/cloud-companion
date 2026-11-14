// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Shape audit of the app (Firefox, the user's browser): every section in the classic layout (wide and phone) and in the desktop, with
// the Etesian shape rules: buttons, text fields, chips and badges take squircle-half ends, icon-only buttons and highlights full
// squircles; never a pill or a rounded rectangle (the allowed exceptions are listed below). The same checks as etesian-design's
// scripts/shape-audit.mjs, over this app's pages instead of the Storybook stories. Fails on any finding.
//   BASE=http://phecloud:8080 CODE=ABCD-EFGH OUT=/out node --experimental-strip-types shapes.ts
import { firefox, type Page } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const CODE = process.env.CODE
assert(CODE, 'CODE (a pairing code from `phecloud paircode --admin`) is required')
const ALLOWED = ['q-toggle', 'hd-switch', 'q-radio', 'hd-radio', 'q-linear-progress', 'hd-progress', 'hd-page-indicator',
  'q-slider', 'q-range', 'q-scrollarea', // Belle widget artwork as on the 701 (panel heads, LCD digits, the music bar, the date bubble)
  'bw-mail__head', 'bw-rss__head', 'bw-music__ctl', 'bw-datebub', 'bw-lcd__d', 'q-carousel__navigation', 'q-spinner', 'hd-busy', 'hd-squircle', 'hd-sq',
  'x-dock__run', 'x-ev__dot', 'md-finger', 'q-tab__indicator', 'q-field__control', 'q-focus-helper',
  // Surfaces, not controls: windows, chat bubbles (their own rounded speech-bubble shape)
  'x-win__bar', 'x-msg__bubble', 'q-message-text', 'xm-bub', 'p2-wt', 'q-tooltip', 'hd-tooltip', 'hd-selection__item',
  // Small radii by the guide: checkboxes; key caps; slider parts; placeholders; the clock face's dots
  'q-checkbox', 'hd-check', 'xm-key', 'hd-colorpicker__hue', 'hd-colorpicker__hmark', 'q-skeleton', 'q-time__clock'];
// Large surfaces (cards, sheets, panels, hints, tiles) keep --hd-radius-card; scaled-down previews can make them look like pills.
const CARD_RADIUS = 8;

interface Finding { name: string; kind: string; why: string; w: number; h: number }

function one(el: HTMLElement): Finding | null {
  const masked = (e: Element) => { const m = (v: string) => v && v !== 'none'; return m(getComputedStyle(e).maskImage) || m(getComputedStyle(e, '::before').maskImage) || m(getComputedStyle(e, '::after').maskImage); };
  const allowed = (e: Element) => { for (let x: Element | null = e; x && x !== document.body; x = x.parentElement) { const c = typeof x.className === 'string' ? x.className : ''; if (window.__ALLOWED.some((a: string) => c.includes(a))) return true; } return false; };
  if (allowed(el) || masked(el)) return null;
  for (const pe of [null, '::before', '::after']) {
    const cs = getComputedStyle(el, pe); const rad = parseFloat(cs.borderTopLeftRadius) || 0;
    if (pe && (cs.content === 'none' || cs.content === 'normal')) continue;
    const painted = (cs.backgroundColor !== 'rgba(0, 0, 0, 0)' && cs.backgroundColor !== 'transparent') || cs.backgroundImage !== 'none';
    if (rad > 2 && painted && Math.abs(rad - window.__CARD) > 0.6) {
      const H = el.offsetHeight, W = el.offsetWidth;
      return { name: `${el.tagName.toLowerCase()}.${String(el.className).trim().split(/\s+/).slice(0, 3).join('.')}${pe ?? ''}`, kind: 'hover',
        why: rad >= H / 2 - 1 ? (W > H * 1.15 ? 'pill' : 'circle') : `rounded ${rad}px`, w: W, h: H };
    }
  }
  return null;
}
function audit() {
  const out: Finding[] = [];
  const allowed = (el: Element) => { for (let e: Element | null = el; e && e !== document.body; e = e.parentElement) {
    const c = typeof e.className === 'string' ? e.className : ''; if (window.__ALLOWED.some((a: string) => c.includes(a))) return true; } return false; };
  const masked = (el: Element) => { const m = (s: string) => s && s !== 'none';
    const a = getComputedStyle(el), b = getComputedStyle(el, '::before'), c = getComputedStyle(el, '::after');
    return m(a.maskImage) || m(b.maskImage) || m(c.maskImage); };
  const painted = (cs: CSSStyleDeclaration) => (cs.backgroundColor !== 'rgba(0, 0, 0, 0)' && cs.backgroundColor !== 'transparent') || cs.backgroundImage !== 'none'
    || (parseFloat(cs.borderTopWidth) > 0 && cs.borderTopStyle !== 'none' && !/rgba\(.*, 0\)$|transparent/.test(cs.borderTopColor))
    || cs.boxShadow !== 'none';
  for (const el of document.querySelectorAll<HTMLElement>('body *')) {
    const r = el.getBoundingClientRect(); if (r.width < 6 || r.height < 6 || r.height > 64) continue;
    const cs = getComputedStyle(el); if (cs.visibility === 'hidden' || cs.display === 'none' || +cs.opacity === 0) continue;
    const rad = parseFloat(cs.borderTopLeftRadius) || 0;
    const kind = el.matches('.q-chip') ? 'chip' : el.matches('.q-badge, .hd-count, [class*="badge"], [class*="__count"], [class*="tag"]') ? 'badge'
      : el.matches('button, .q-btn, a[role="button"], [class*="btn"]') ? 'button' : el.matches('input, textarea, .q-field') ? 'field' : '';
    if (allowed(el) || el.tagName === 'KBD') continue;   // key caps: small radii, like checkboxes
    const W = el.offsetWidth || r.width, H = el.offsetHeight || r.height;   // the element's own size, not a scaled preview's
    if (Math.abs(rad - window.__CARD) < 0.6 && (!kind || H > 56)) continue;   // cards and tiles
    if (!kind && (H <= 8 || (W <= 16 && H <= 16))) continue;   // progress/slider tracks, page and status dots
    const name = `${el.tagName.toLowerCase()}.${(typeof el.className === 'string' ? el.className : '').trim().split(/\s+/).slice(0, 3).join('.')}`;
    if (rad > 2 && painted(cs) && !masked(el)) {
      out.push({ name, why: rad >= H / 2 - 1 ? (W > H * 1.15 ? 'pill' : 'circle') : `rounded ${rad}px`, kind, w: Math.round(r.width), h: Math.round(r.height) });
    } else if ((kind === 'chip' || kind === 'badge') && painted(cs) && !masked(el)) {
      out.push({ name, why: 'chip/badge without squircle mask', kind, w: Math.round(r.width), h: Math.round(r.height) });
    }
  }
  return out;
}

// Hover states: links, buttons and tabs often paint a highlight only under the pointer (2026-11-05: the site
// header's links showed a rounded rectangle). Hover each one and audit it and its pseudo-elements then.
async function hovers(p: Page) {
  const out: Finding[] = [];
  const n = await p.evaluate(() => { const els = [...document.querySelectorAll('a, button, [role="button"], [role="tab"], .q-tab, .q-item--clickable')]
    .filter((e) => { const r = e.getBoundingClientRect(); return r.width >= 6 && r.height >= 6 && r.height <= 64 && r.bottom > 0 && r.top < innerHeight; });
    els.forEach((e, i) => e.setAttribute('data-hv', String(i))); return Math.min(els.length, 60); });
  for (let i = 0; i < n; i++) {
    const ok = await p.hover(`[data-hv="${i}"]`, { timeout: 800, force: true }).then(() => true).catch(() => false);
    if (!ok) continue;
    await p.waitForTimeout(160);   // let hover transitions finish
    const f = await p.evaluate((i: number) => { const el = document.querySelector(`[data-hv="${i}"]`); return el ? window.__one(el as HTMLElement) : null; }, i).catch(() => null);
    if (f) out.push({ ...f, why: 'on hover: ' + f.why });
  }
  await p.mouse.move(0, 0);
  return out;
}

const SECTIONS = ['browse', 'chats', 'mail', 'calendar', 'contacts', 'files', 'photos', 'music', 'settings', 'settings/connections', 'settings/devices', 'settings/catalogue', 'settings/security']
const browser = await firefox.launch()
const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 }, deviceScaleFactor: 1, serviceWorkers: 'block' })
const p = await ctx.newPage()
await p.addInitScript(([a, c, f]) => { (window as any).__ALLOWED = a; (window as any).__CARD = c; (window as any).__one = new Function('return ' + f)() }, [ALLOWED, CARD_RADIUS, one.toString()] as const)
await p.goto(BASE + '/#/pair')
await p.fill('[data-test=code]', CODE)
await p.click('[data-test=pair]')
for (let i = 0; !(await p.evaluate(() => (window as any).__phe?.state.status === 'online')); i++) { assert(i < 100, 'online'); await p.waitForTimeout(200) }
const found = new Map<string, any>()
const look = async (where: string) => {
  await p.waitForTimeout(700)
  for (const f of [...await p.evaluate(audit).catch(() => []), ...await hovers(p)]) {
    const k = `${f.name} | ${f.why}`
    if (!found.has(k)) found.set(k, { ...f, where: [] })
    found.get(k).where.push(where)
  }
}
for (const [vp, size] of [['wide', { width: 1280, height: 900 }], ['phone', { width: 390, height: 844 }]] as const) {
  await p.setViewportSize(size)
  for (const s of SECTIONS) { await p.goto(`${BASE}/#/${s}`); await look(`${vp} ${s}`) }
  // the apps menu
  await p.click('.x-cl__apps'); await look(`${vp} apps menu`); await p.keyboard.press('Escape')
}
await p.setViewportSize({ width: 1440, height: 900 })
await p.goto(`${BASE}/#/desktop?open=mail`)
await look('desktop')
await browser.close()
for (const f of [...found.values()].sort((a, b) => a.kind.localeCompare(b.kind) || a.name.localeCompare(b.name)))
  console.log(`${(f.kind || '-').padEnd(6)} ${f.why.padEnd(34)} ${f.w}x${f.h}  ${f.name}  (${f.where.length} views, e.g. ${f.where[0]})`)
console.log(`${found.size} finding(s)`)
process.exit(found.size ? 1 : 0)
