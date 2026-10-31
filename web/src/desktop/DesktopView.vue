<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- The desktop view (1024 px and up): the app's sections as windows, as if the server were a computer you are sitting at. It follows
     the Etesian desktop design: a global menu bar at the top (the focused window's name and its menus on the left, the connection and
     the clock on the right), a light floating dock on the left (launcher, then the sections, a bar marking the open ones), and
     overlapping windows that snap to halves or fill the screen (by dragging to an edge, or Super / Ctrl+Alt with the arrow keys).
     Super alone, or the dock's first button, opens the Overview, the main menu (Etesian Desktop v1, kept by v2): Events on the left,
     the open windows in the middle, the sections on the right; just type to search. Super+Space (or Ctrl+Alt+Space) opens the
     spotlight, a compact search added by Desktop v2: type to find a section, Enter opens it.
     The window arrangement and the layout choice are kept on this device (localStorage), like the theme. -->
<template>
  <div class="ph-desk" data-test="desktop">
    <a href="#ph-desk-area" class="ph-skip" @click.prevent="focusTop">Skip to the windows</a>
    <!-- global menu bar -->
    <header class="ph-menubar">
      <span class="ph-menubar__app" data-test="menubar-app">{{ focusedSection?.label || brand.name }}</span>
      <q-btn flat dense no-caps label="Window" class="ph-menubar__menu" :disable="!openWins.length">
        <q-menu class="ph-desk-menu" anchor="bottom left" self="top left">
          <q-list dense>
            <q-item v-close-popup clickable :disable="!focusedWin" @click="minimise(focusedWin)"><q-item-section>Minimise</q-item-section></q-item>
            <q-item v-close-popup clickable :disable="!focusedWin" @click="toggleMax(focusedWin)">
              <q-item-section>{{ focusedWin?.state === 'max' ? 'Restore' : 'Maximise' }}</q-item-section><q-item-section side>Super+Up</q-item-section>
            </q-item>
            <q-item v-close-popup clickable :disable="!focusedWin" @click="snap(focusedWin, 'left')"><q-item-section>Snap to the left half</q-item-section><q-item-section side>Super+Left</q-item-section></q-item>
            <q-item v-close-popup clickable :disable="!focusedWin" @click="snap(focusedWin, 'right')"><q-item-section>Snap to the right half</q-item-section><q-item-section side>Super+Right</q-item-section></q-item>
            <q-item v-close-popup clickable :disable="!focusedWin" @click="close(focusedWin)"><q-item-section>Close</q-item-section></q-item>
            <q-separator />
            <q-item v-for="w in openWins" :key="w.id" v-close-popup clickable @click="show(w)">
              <q-item-section>{{ byId[w.id].label }}</q-item-section>
              <q-item-section v-if="w.min" side>Minimised</q-item-section>
            </q-item>
          </q-list>
        </q-menu>
      </q-btn>
      <q-btn flat dense no-caps label="View" class="ph-menubar__menu">
        <q-menu class="ph-desk-menu" anchor="bottom left" self="top left">
          <q-list dense>
            <q-item v-close-popup clickable data-test="menu-overview" @click="overview = true"><q-item-section>Overview</q-item-section><q-item-section side>Super</q-item-section></q-item>
            <q-item v-close-popup clickable @click="launcher = true"><q-item-section>Search</q-item-section><q-item-section side>Super+Space</q-item-section></q-item>
            <q-item v-close-popup clickable @click="events = true"><q-item-section>Events</q-item-section><q-item-section side>Super+N</q-item-section></q-item>
            <q-item v-close-popup clickable data-test="show-desktop" @click="showDesktop"><q-item-section>Show desktop</q-item-section><q-item-section side>Super+D</q-item-section></q-item>
            <q-item v-close-popup clickable data-test="edit-widgets" @click="widgetEdit = true"><q-item-section>Edit widgets</q-item-section></q-item>
            <q-item v-close-popup clickable data-test="classic-view" @click="leave"><q-item-section>Classic view</q-item-section></q-item>
          </q-list>
        </q-menu>
      </q-btn>
      <q-space />
      <span class="ph-menubar__status" :class="'is-' + state.status" role="status">
        <q-icon :name="state.status === 'online' ? 'sym_o_cloud_done' : state.status === 'connecting' ? 'sym_o_sync' : 'sym_o_cloud_off'" size="16px" aria-hidden="true" />
        {{ statusText }}
      </span>
      <!-- the clock opens Events, as on the Etesian desktop; the dot says there is unread mail -->
      <button type="button" class="ph-menubar__clock" :class="{ 'is-on': events }" :aria-expanded="events" data-test="clock"
              :aria-label="`${dateText} ${timeText}, events${unread ? ', ' + unread + ' unread mail' : ''}`" title="Events (Super+N)" @click="events = !events">
        <time :datetime="now.toISOString()">{{ dateText }} <b>{{ timeText }}</b></time>
        <span v-if="unread" class="ph-menubar__dot" aria-hidden="true" />
      </button>
    </header>

    <div id="ph-desk-area" ref="areaEl" class="ph-desk__area" tabindex="-1">
      <!-- widgets on the wallpaper, under the windows; while they are edited the windows step aside -->
      <DesktopWidgets :now="now" :area="area" :editing="widgetEdit" @open="(id) => open(id)" @done="widgetEdit = false" />
      <DesktopWindow v-for="w in (widgetEdit ? [] : switcher ? viewWins : shownWins)" :key="w.id" :win="w" :rect="rectOf(w)" :section="byId[w.id]" :page="pages[w.id].component!"
                     :page-props="pages[w.id].props" :focused="focusedId === w.id" :busy="busy || !!switcher" :card="switcher ? cardOf(w.id) : null"
                     @focus="focus(w); events = false" @move="(p) => move(w, p)" @resize="(r) => resize(w, r)" @drag-zone="dragZone" @drop="(p) => drop(w, p)"
                     @busy="(b) => (busy = b)" @minimise="minimise(w)" @maximise="toggleMax(w)" @close="close(w)" />
      <!-- where a dragged window will snap -->
      <div v-if="zone || pickHot" class="ph-desk__zone" :style="zoneStyle" aria-hidden="true" />
      <LayoutPicker v-if="picker" :layout="picker.layout" :hot="pickHot" :drag="picker.drag" @pick="(l, z) => snapZone(focusedWin, l, z)" />

      <!-- the Windows view (Etesian desktop, Plasma's etesian_windows; the phone's Open apps): hold Super (or Ctrl+Alt) and tap Tab, the
           windows themselves become cards, one large at a time with its neighbours at half strength; letting go switches. Super / Ctrl +
           wheel zooms to 2, 3 or 4 per row and keeps the view open (Tab, arrows, Enter, a click, Esc). -->
      <div v-if="switcher" class="ph-wins" role="listbox" aria-label="Windows" data-test="switcher" @wheel="onViewWheel" @pointerdown.self="endSwitcher(false)">
        <header class="ph-wins__head">
          <h2>Windows</h2><span>{{ switcher.list.length }} open · {{ LEVEL_LABEL[switcher.level] }}</span>
          <span class="ph-wins__zoom" role="radiogroup" aria-label="Zoom (Super + wheel)">
            <button v-for="l in [1, 2, 3, 4]" :key="l" type="button" role="radio" class="hd-sq" :class="{ 'is-on': l === switcher.level }" :aria-checked="l === switcher.level"
                    :aria-label="LEVEL_LABEL[l]" :data-test="'wins-zoom-' + l" @click="setLevel(l)">
              <svg width="20" height="14" viewBox="0 0 20 14" aria-hidden="true"><rect v-for="k in l" :key="k" :x="l === 1 ? 4 : (k - 1) * (20 / l) + 0.5" :y="l === 1 ? 1 : 3" :width="l === 1 ? 12 : 20 / l - 1.5" :height="l === 1 ? 12 : 8" rx="1.5" fill="currentColor" /></svg>
            </button>
          </span>
        </header>
        <!-- one button over each card: picks it (the page under it does not take the pointer) -->
        <button v-for="(id, i) in switcher.list" :key="id" type="button" class="ph-wins__hit" role="option" :aria-selected="i === switcher.i" :aria-label="byId[id].label"
                :class="{ 'is-sel': i === switcher.i }" :data-test="'switch-' + id" :style="hitStyle(id)" @click="switcher.i = i; endSwitcher(true)">
          <span class="ph-wins__label"><SectionTile :section="byId[id]" :size="22" />{{ byId[id].label }}</span>
        </button>
      </div>

      <EventsPanel v-if="events" :now="now" @open="(id) => { events = false; open(id) }" @close="events = false" @unread="(n) => (unread = n)" />

      <!-- the Overview (Super): the main menu, Events | windows | sections -->
      <div v-if="overview" class="ph-ov" role="dialog" aria-label="Overview" data-test="overview" @pointerdown.self="overview = false">
        <div class="ph-ov__events"><EventsPanel :now="now" embedded @open="(id) => open(id)" @close="overview = false" @unread="(n) => (unread = n)" /></div>
        <section class="ph-ov__wins" aria-label="Open windows" @pointerdown.self="overview = false">
          <div v-for="w in [...wins].sort((a, b) => b.z - a.z)" :key="w.id" class="ph-ov__win" :data-test="'ov-win-' + w.id">
            <button type="button" class="ph-ov__card" :aria-label="'Show ' + byId[w.id].label" @click="open(w.id)">
              <SectionTile :section="byId[w.id]" :size="56" />
            </button>
            <span class="ph-ov__name"><SectionTile :section="byId[w.id]" :size="18" />{{ byId[w.id].label }}</span>
            <button type="button" class="ph-ov__close hd-squircle" :aria-label="'Close ' + byId[w.id].label" :data-test="'ov-close-' + w.id" @click="close(w)">
              <q-icon name="sym_o_close" size="14px" />
            </button>
          </div>
          <p v-if="!wins.length" class="ph-ov__none">No open windows</p>
        </section>
        <section class="ph-ov__apps" aria-label="Sections">
          <q-input ref="ovSearchEl" v-model="query" dark borderless dense placeholder="Type to search" aria-label="Search sections"
                   class="ph-launcher__search" @keydown.enter="openFirst" @keydown.esc="overview = false">
            <template #prepend><q-icon name="sym_o_search" /></template>
          </q-input>
          <p class="ph-ov__hint">{{ query ? `Matching “${query}”` : 'Just start typing' }}</p>
          <div class="ph-ov__grid">
            <button v-for="s in found" :key="s.id" type="button" class="ph-launcher__app" :data-test="'ov-launch-' + s.id" @click="open(s.id)">
              <SectionTile :section="s" :size="54" />
              <span>{{ s.label }}</span>
            </button>
          </div>
        </section>
      </div>

      <!-- spotlight (Super+Space): a compact search, added by Desktop v2 next to the Overview -->
      <div v-if="launcher" class="ph-launcher-scrim" @pointerdown.self="launcher = false">
        <div class="ph-launcher" role="dialog" aria-label="Search" data-test="launcher">
          <q-input ref="searchEl" v-model="query" dark borderless dense placeholder="Type to search" aria-label="Search sections"
                   class="ph-launcher__search" @keydown.enter="openFirst" @keydown.esc="launcher = false">
            <template #prepend><q-icon name="sym_o_search" /></template>
          </q-input>
          <div class="ph-launcher__grid">
            <button v-for="s in found" :key="s.id" type="button" class="ph-launcher__app" :data-test="'launch-' + s.id" @click="open(s.id)">
              <SectionTile :section="s" :size="56" />
              <span>{{ s.label }}</span>
            </button>
            <p v-if="!found.length" class="ph-launcher__none">Nothing matches "{{ query }}".</p>
          </div>
        </div>
      </div>
    </div>

    <!-- dock -->
    <nav class="ph-dock hd-on-home" aria-label="Sections">
      <button type="button" class="ph-dock__item" :class="{ 'is-active': overview }" aria-label="Overview" title="Overview (Super)"
              data-test="dock-launcher" @click="overview = !overview">
        <span class="ph-dock__launcher hd-squircle" aria-hidden="true"><q-icon name="sym_o_apps" size="22px" /></span>
      </button>
      <span class="ph-dock__sep" aria-hidden="true" />
      <button v-for="s in visible" :key="s.id" type="button" class="ph-dock__item"
              :class="{ 'is-open': isOpen(s.id), 'is-active': focusedId === s.id && !winOf(s.id)?.min }"
              :aria-label="s.label + (isOpen(s.id) ? ', open' : '')" :title="s.label" :data-test="'dock-' + s.id" @click="dockClick(s.id)">
        <SectionTile :section="s" :size="40" />
      </button>
    </nav>
  </div>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch, type Component } from 'vue'
import type { QInput } from 'quasar'
import type { Rect, Win, WinState } from './types'
import LayoutPicker from './LayoutPicker.vue'
import { LAYOUTS, zoneRect } from './snaplayouts'
import { useRoute, useRouter } from 'vue-router'
import { state } from '../cloud'
import { loadUnread } from './glance'
import { sections } from '../nav'
import { brand } from '../brand'
import SectionTile from '../components/SectionTile.vue'
import DesktopWindow from './DesktopWindow.vue'
import EventsPanel from './EventsPanel.vue'
import DesktopWidgets from './DesktopWidgets.vue'
import { getLayout, setLayout } from './layout'

const KEY = 'phecloud-desktop'
const GAP = 8
const EDGE = 6 // px from the desktop's edge that counts as "dropped on the edge"

const route = useRoute()
const router = useRouter()
const visible = computed(() => sections.filter((s) => !s.admin || state.admin))
const byId = Object.fromEntries(sections.map((s) => [s.id, s]))

// Each section's page, as the router knows it (lazy), plus the section id for the placeholder page.
const pages: Record<string, { component: Component | undefined; props: Record<string, unknown> }> = Object.fromEntries(sections.map((s) => {
  const rec = router.getRoutes().find((r) => r.path === s.path)
  const c = rec?.components?.default
  return [s.id, { component: typeof c === 'function' ? defineAsyncComponent(c as () => Promise<Component>) : c, props: rec?.meta?.id ? { sectionId: s.id } : {} }]
}))

// ---- windows ----
/** { id, x, y, w, h, state: 'float' | 'left' | 'right' | 'max', z, min } in desktop-area coordinates; x/y/w/h is the floating size */
const wins = reactive<Win[]>(load())
const focusedId = ref<string | null>(null)
const widgetEdit = ref(false) // the widgets' edit mode (View → Edit widgets)
const busy = ref(false)
const zone = ref<WinState | null>(null)
const areaEl = ref<HTMLElement | null>(null)
const area = reactive({ w: 0, h: 0, left: 0, top: 0 })
let topZ = Math.max(0, ...wins.map((w) => w.z))

const openWins = computed(() => wins)
const shownWins = computed(() => wins.filter((w) => !w.min && byId[w.id] && pages[w.id]?.component))
const focusedWin = computed(() => wins.find((w) => w.id === focusedId.value && !w.min) || null)
const focusedSection = computed(() => (focusedWin.value ? byId[focusedWin.value.id] : null))
const winOf = (id: string) => wins.find((w) => w.id === id)
const isOpen = (id: string) => !!winOf(id)

function load(): Win[] {
  try {
    const saved = JSON.parse(localStorage.getItem(KEY) || '[]')
    return Array.isArray(saved) ? saved.filter((w: Win) => w && byId[w.id]) : []
  } catch (e) { return [] }
}
watch(wins, () => { try { localStorage.setItem(KEY, JSON.stringify(wins)) } catch (e) { /* private mode */ } }, { deep: true })

function measure() {
  const r = areaEl.value?.getBoundingClientRect()
  if (r) Object.assign(area, { w: r.width, h: r.height, left: r.left, top: r.top })
}

/** Where a window is drawn: its floating rectangle kept on the desktop, or the zone it is snapped to. */
function rectOf(w: Win): Rect {
  const half = (area.w - 3 * GAP) / 2
  if (w.state === 'zone') { const r = zoneRect(w.lay ?? 0, w.zi ?? 0, area.w, area.h, GAP); if (r) return r }
  if (w.state === 'max') return { x: GAP, y: GAP, w: area.w - 2 * GAP, h: area.h - 2 * GAP }
  if (w.state === 'left') return { x: GAP, y: GAP, w: half, h: area.h - 2 * GAP }
  if (w.state === 'right') return { x: 2 * GAP + half, y: GAP, w: half, h: area.h - 2 * GAP }
  const ww = Math.min(w.w, area.w - 2 * GAP)
  const hh = Math.min(w.h, area.h - 2 * GAP)
  // keep at least the header bar reachable
  return { x: Math.min(Math.max(w.x, 120 - ww), area.w - 120), y: Math.min(Math.max(w.y, 0), area.h - 44), w: ww, h: hh }
}

function focus(w: Win) {
  if (focusedId.value === w.id && w.z === topZ) return
  w.z = ++topZ
  focusedId.value = w.id
}

function open(id: string) {
  launcher.value = false
  overview.value = false
  query.value = ''
  let w = winOf(id)
  if (!w) {
    const n = wins.length % 6
    // Android is a phone: its window opens tall and narrow, the screen filling it; the other sections open as wide pages
    const size = id === 'android' ? { w: Math.min(460, area.w - 120), h: area.h - 2 * GAP } : { w: Math.min(1040, area.w - 120), h: Math.min(720, area.h - 64) }
    w = reactive<Win>({ id, x: 32 + n * 36, y: id === 'android' ? GAP : 16 + n * 32, ...size, state: 'float', z: 0, min: false })
    wins.push(w)
    w = winOf(id)! // the reactive copy in the list
  }
  show(w)
}

function show(w: Win) { w.min = false; focus(w) }

function minimise(w: Win | null) {
  if (!w) return
  w.min = true
  if (focusedId.value === w.id) focusTop()
}

function close(w: Win | null) {
  if (!w) return
  wins.splice(wins.indexOf(w), 1)
  if (focusedId.value === w.id) focusTop()
}

/** Focus the topmost window still showing (after one is minimised or closed). */
/** Minimise every window, so the widgets show (Super+D). */
function showDesktop() {
  launcher.value = false; overview.value = false; events.value = false
  for (const w of wins) w.min = true
  focusTop()
}
function focusTop() {
  const top = [...shownWins.value].sort((a, b) => b.z - a.z)[0]
  focusedId.value = top ? top.id : null
  nextTick(() => document.querySelector<HTMLElement>(top ? `[data-test="win-${top.id}"] .ph-win__btn` : '#ph-desk-area')?.focus())
}

function toggleMax(w: Win | null) { if (w) w.state = w.state === 'max' ? 'float' : 'max' }
function snap(w: Win | null, s: WinState) { if (w) w.state = s }
/** Snap layouts: the picker (dragged to the top edge, or Super+Z / Ctrl+Alt+Z) and the zone under the pointer while dragging. */
const picker = ref<{ layout: number; drag: boolean } | null>(null)
const pickHot = ref<[number, number] | null>(null)
function snapZone(w: Win | null, layout: number, zi: number) {
  picker.value = null; pickHot.value = null
  if (!w) return
  w.state = 'zone'; w.lay = layout; w.zi = zi
  focus(w)
}

/** Where a dragged window was before the drag: dropped on an edge it snaps, and floats back there later. */
const dragFrom = new Map<string, { x: number; y: number } | null>()
function move(w: Win, p: { x: number; y: number }) {
  if (!dragFrom.has(w.id)) dragFrom.set(w.id, w.state === 'float' ? { x: w.x, y: w.y } : null)
  if (w.state !== 'float') w.state = 'float'
  w.x = p.x; w.y = p.y
}

function resize(w: Win, r: Rect) { Object.assign(w, { x: r.x, y: r.y, w: r.w, h: r.h, state: 'float' }) }

function zoneAt(px: number, py: number): WinState | null {
  if (px <= area.left + EDGE) return 'left'
  if (px >= area.left + area.w - EDGE) return 'right'
  if (py <= area.top + EDGE) return 'max'
  return null
}
// dragging a window to the top edge drops the snap-layout picker; it stays while the pointer is over it, and releasing over a zone snaps
const PICK_EDGE = 40
function dragZone({ px, py }: { px: number; py: number }) {
  if (py <= area.top + PICK_EDGE && !picker.value) picker.value = { layout: -1, drag: true }
  if (picker.value?.drag) {
    const el = document.elementFromPoint(px, py)?.closest<HTMLElement>('[data-lz]')
    const over = !!document.elementFromPoint(px, py)?.closest('[data-test=layout-picker]')
    pickHot.value = el ? (el.dataset.lz!.split(':').map(Number) as [number, number]) : null
    if (!over && py > area.top + PICK_EDGE) picker.value = null // dragged down off the picker: keep floating
  }
  zone.value = pickHot.value ? null : zoneAt(px, py)
}
function drop(w: Win, { px, py }: { px: number; py: number }) {
  const hot = pickHot.value
  picker.value = null; pickHot.value = null
  if (hot) { dragFrom.delete(w.id); zone.value = null; snapZone(w, hot[0], hot[1]); return }
  const z = zoneAt(px, py)
  const from = dragFrom.get(w.id)
  dragFrom.delete(w.id)
  zone.value = null
  if (!z) return
  if (from) { w.x = from.x; w.y = from.y }
  w.state = z
}
const zoneStyle = computed(() => {
  const h = pickHot.value
  const r = rectOf({ id: '', x: 0, y: 0, w: 0, h: 0, z: 0, min: false, state: h ? 'zone' : zone.value ?? 'max', lay: h?.[0], zi: h?.[1] })
  return { left: r.x + 'px', top: r.y + 'px', width: r.w + 'px', height: r.h + 'px' }
})

/** Dock: open a section; if it is the focused window, minimise it; if it is minimised or behind, bring it forward. */
function dockClick(id: string) {
  const w = winOf(id)
  if (w && !w.min && focusedId.value === id) minimise(w)
  else open(id)
}

// ---- the Overview (Super) and the spotlight (Super+Space) share the search ----
const launcher = ref(false)
const overview = ref(false)
const query = ref('')
const searchEl = ref<QInput | null>(null)
const ovSearchEl = ref<QInput | null>(null)
const found = computed(() => visible.value.filter((s) => s.label.toLowerCase().includes(query.value.trim().toLowerCase())))
function openFirst() { if (found.value.length) open(found.value[0].id) }
watch(launcher, (on) => { if (on) { overview.value = false; nextTick(() => searchEl.value?.focus()) } else query.value = '' })
watch(overview, (on) => { if (on) { launcher.value = false; events.value = false; nextTick(() => ovSearchEl.value?.focus()) } else query.value = '' })

// ---- leaving, and links between sections ----
let leaving = false
function leave() {
  leaving = true
  setLayout('classic')
  router.push(focusedWin.value ? byId[focusedWin.value.id].path : '/browse')
}
/** A page that links to another section opens it as a window instead of leaving the desktop. */
const stopGuard = router.beforeEach((to, from) => {
  if (leaving || !from.meta.desktop || getLayout() !== 'desktop') return
  const s = sections.find((x) => x.path === to.path)
  if (s) { open(s.id); return false }
})

// ---- quick switcher (Super+Tab, or Ctrl+Alt+Tab where the system leaves Super to the browser) ----
/** the Windows view: the window ids, most recently used first, the selected one, the zoom level, and whether it stays open */
const switcher = ref<{ list: string[]; i: number; level: number; held: boolean } | null>(null)
const LEVEL_LABEL: Record<number, string> = { 1: 'One at a time', 2: '2 per row', 3: '3 per row', 4: '4 per row' }
const viewWins = computed(() => (switcher.value ? switcher.value.list.map((id) => winOf(id)!).filter(Boolean) : []))
/** The cards (etesian-design desktop2/XWindows.vue): level 1 = one large card centred, the others in a row beside it at half strength;
 *  2–4 = a grid. Each card keeps its window's shape. */
const cards = computed(() => {
  const sw = switcher.value
  const out: Record<string, { x: number; y: number; w: number; h: number; s: number; z: number; side: boolean }> = {}
  if (!sw) return out
  const TOP = 64, TITLE = 34, PAD = 40, n = sw.level, avail = area.w - 2 * PAD
  const fit = (w: Win, bw: number, bh: number) => { const r = rectOf(w), s = Math.min(bw / r.w, bh / r.h); return { w: r.w * s, h: r.h * s, s } }
  const ws = viewWins.value
  if (n === 1) {
    const ch = area.h - TOP - TITLE - 40, cw = Math.min(ch * 1.5, avail - 260), G = 48
    const fs = ws.map((w) => fit(w, cw, ch)), xs: number[] = []
    const sel = Math.min(sw.i, fs.length - 1)
    xs[sel] = PAD + avail / 2 - fs[sel].w / 2
    for (let i = sel + 1; i < fs.length; i++) xs[i] = xs[i - 1] + fs[i - 1].w + G
    for (let i = sel - 1; i >= 0; i--) xs[i] = xs[i + 1] - G - fs[i].w
    ws.forEach((w, i) => { out[w.id] = { x: xs[i], y: TOP + (ch - fs[i].h) / 2, ...fs[i], z: 9210 + i, side: i !== sel } })
    return out
  }
  const gap = n === 2 ? 40 : n === 3 ? 32 : 26, rows = Math.ceil(ws.length / n)
  const cw = (avail - gap * (n - 1)) / n, ch = Math.min(cw * 0.62, (area.h - TOP - 16 - rows * (TITLE + gap)) / rows) // every row on screen
  ws.forEach((w, i) => {
    const c = i % n, r = Math.floor(i / n), f = fit(w, cw, ch)
    out[w.id] = { x: PAD + c * (cw + gap) + (cw - f.w) / 2, y: TOP + r * (ch + TITLE + gap) + (ch - f.h), ...f, z: 9210 + i, side: false }
  })
  return out
})
const cardOf = (id: string) => cards.value[id] || null
const hitStyle = (id: string) => { const c = cards.value[id]; return c ? { left: c.x + 'px', top: c.y + 'px', width: c.w + 'px', height: c.h + TITLE_H + 'px' } : {} }
const TITLE_H = 30
function setLevel(l: number) { if (switcher.value) { switcher.value.level = l; switcher.value.held = l === 1 && switcher.value.held } }
function onViewWheel(e: WheelEvent) {
  const sw = switcher.value
  if (!sw) return
  if (e.metaKey || e.ctrlKey) { e.preventDefault(); sw.level = Math.min(4, Math.max(1, sw.level + (e.deltaY > 0 ? 1 : -1))); sw.held = false }
  else if (sw.level === 1 && e.deltaY) sw.i = Math.min(sw.list.length - 1, Math.max(0, sw.i + (e.deltaY > 0 ? 1 : -1)))
}
function stepSwitcher(back: boolean) {
  if (!switcher.value) {
    const list = [...wins].sort((a, b) => b.z - a.z).map((w) => w.id)
    switcher.value = { list, i: list.length > 1 ? (back ? list.length - 1 : 1) : 0, level: 1, held: true }
  } else {
    const n = switcher.value.list.length
    switcher.value.i = (switcher.value.i + (back ? n - 1 : 1)) % n
  }
}
function endSwitcher(commit: boolean) {
  const sw = switcher.value
  switcher.value = null
  if (commit && sw) open(sw.list[sw.i])
}

// ---- keyboard: Super (or Ctrl+Alt) with the arrows snaps; Super alone opens the Overview, Super+Space the spotlight; Escape closes ----
let superAlone = false
function onKeyDown(e: KeyboardEvent) {
  if (e.key === 'Meta' || e.key === 'OS') { superAlone = true; return }
  superAlone = false
  if (e.key === 'Escape' && switcher.value) { e.preventDefault(); endSwitcher(false); return }
  if (switcher.value && !switcher.value.held) {
    // zoomed out, the view stays open as a task viewer: arrows / Tab move, Enter opens
    const sw = switcher.value, n = sw.list.length, per = sw.level
    const step: Record<string, number> = { ArrowRight: 1, ArrowLeft: -1, ArrowDown: per, ArrowUp: -per, Tab: e.shiftKey ? -1 : 1 }
    if (e.key === 'Enter') { e.preventDefault(); endSwitcher(true); return }
    if (step[e.key] !== undefined) { e.preventDefault(); sw.i = Math.min(n - 1, Math.max(0, sw.i + step[e.key])); return }
  }
  if (picker.value && !picker.value.drag) {
    // Super+Z: a digit picks the layout, the next one its zone
    if (e.key === 'Escape') { e.preventDefault(); picker.value = null; return }
    const n = Number(e.key) - 1
    if (Number.isInteger(n) && n >= 0) {
      e.preventDefault()
      if (picker.value.layout < 0) { if (n < LAYOUTS.length) picker.value.layout = n }
      else if (n < LAYOUTS[picker.value.layout].zones.length) snapZone(focusedWin.value, picker.value.layout, n)
      return
    }
  }
  if ((e.key === 'z' || e.key === 'Z') && (e.metaKey || (e.ctrlKey && e.altKey)) && focusedWin.value) {
    e.preventDefault(); picker.value = picker.value ? null : { layout: -1, drag: false }; return
  }
  if (e.key === 'Tab' && (e.metaKey || (e.ctrlKey && e.altKey))) {
    e.preventDefault()
    if (wins.length) { launcher.value = false; overview.value = false; stepSwitcher(e.shiftKey) }
    return
  }
  if (e.key === ' ' && (e.metaKey || (e.ctrlKey && e.altKey))) { e.preventDefault(); launcher.value = !launcher.value; return }
  if (e.key === 'Escape' && launcher.value) { launcher.value = false; return }
  if (e.key === 'Escape' && overview.value) { overview.value = false; return }
  if (e.key === 'Escape' && events.value) { events.value = false; return }
  if (e.key === 'Escape' && widgetEdit.value) { widgetEdit.value = false; return }
  if ((e.key === 'd' || e.key === 'D') && (e.metaKey || (e.ctrlKey && e.altKey))) { e.preventDefault(); showDesktop(); return }
  if ((e.key === 'n' || e.key === 'N') && (e.metaKey || (e.ctrlKey && e.altKey))) { e.preventDefault(); launcher.value = false; overview.value = false; events.value = !events.value; return }
  if (!(e.metaKey || (e.ctrlKey && e.altKey)) || !focusedWin.value) return
  const w = focusedWin.value
  // as in the Etesian desktop: Left / Right move to that zone, Up maximises, Down floats a snapped window again or minimises a floating one
  const keys: Record<string, () => void> = { ArrowLeft: () => snap(w, 'left'), ArrowRight: () => snap(w, 'right'), ArrowUp: () => snap(w, 'max'),
    ArrowDown: () => (w.state === 'float' ? minimise(w) : snap(w, 'float')) }
  const act = keys[e.key]
  if (act) { e.preventDefault(); act() }
}
function onKeyUp(e: KeyboardEvent) {
  if (switcher.value?.held && ['Meta', 'OS', 'Control', 'Alt'].includes(e.key)) { endSwitcher(true); superAlone = false; return }
  if ((e.key === 'Meta' || e.key === 'OS') && superAlone) overview.value = !overview.value
  superAlone = false
}

// ---- events: the panel, and the unread-mail dot on the clock (checked every two minutes; quiet when mail is not set up) ----
const events = ref(false)
const unread = ref(0)
async function checkUnread() {
  try { unread.value = (await loadUnread(0)).unread } catch (e) { unread.value = 0 }
}
watch(launcher, (on) => { if (on) events.value = false })

// ---- menu bar ----
const statusText = computed(() => ({ online: 'Connected', connecting: 'Connecting', offline: 'Offline' })[state.status] || state.status)
const now = ref(new Date())
const timeText = computed(() => now.value.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }))
const dateText = computed(() => now.value.toLocaleDateString([], { weekday: 'short', day: 'numeric', month: 'short' }))
let clock: ReturnType<typeof setInterval> | undefined, mailTimer: ReturnType<typeof setInterval> | undefined

onMounted(() => {
  measure()
  window.addEventListener('resize', measure)
  window.addEventListener('keydown', onKeyDown)
  window.addEventListener('keyup', onKeyUp)
  clock = setInterval(() => { now.value = new Date() }, 15000)
  checkUnread()
  mailTimer = setInterval(checkUnread, 120000)
  const want = String(route.query.open || '')
  if (byId[want]) open(want)
  else if (!wins.length) open('browse')
  else focusTop()
})
onBeforeUnmount(() => {
  stopGuard()
  window.removeEventListener('resize', measure)
  window.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('keyup', onKeyUp)
  clearInterval(clock)
  clearInterval(mailTimer)
})
</script>

<style scoped>
/* the desktop: the Etesian home surface (black), with a faint glow so windows stand out */
.ph-desk {
  position: fixed; inset: 0; display: grid; grid-template: 28px 1fr / 72px 1fr; overflow: hidden;
  background: radial-gradient(120% 90% at 85% 100%, #0e2238 0%, #04080d 55%, #000 100%); color: var(--hd-surface-home-fg);
  font-family: var(--ph-font-light);
}
/* global menu bar */
.ph-menubar {
  grid-column: 1 / -1; display: flex; align-items: center; gap: var(--ph-s1); padding: 0 var(--ph-s3);
  background: #000; color: #fff; font: 400 var(--hd-font-size-small)/1 var(--ph-font); border-bottom: 1px solid rgba(255, 255, 255, 0.08);
}
.ph-menubar__app { font-weight: var(--hd-font-weight-bold); margin-right: var(--ph-s2); }
.ph-menubar__menu { min-height: 26px !important; padding: 0 var(--ph-s2); color: #fff !important; font: inherit; }
.ph-menubar__status { display: inline-flex; align-items: center; gap: var(--ph-s1); color: var(--ph-chrome-fg-muted); margin-right: var(--ph-s4); }
.ph-menubar__status.is-online { color: #8FD65A; }
.ph-menubar__clock {
  position: relative; height: 24px; padding: 0 var(--ph-s2); border: 0; border-radius: 4px; background: none; color: inherit; font: inherit;
  white-space: nowrap; cursor: pointer;
}
.ph-menubar__clock:hover, .ph-menubar__clock.is-on { background: rgba(255, 255, 255, 0.16); }
.ph-menubar__clock b { margin-left: var(--ph-s1); }
.ph-menubar__dot { position: absolute; top: 3px; right: 1px; width: 6px; height: 6px; border-radius: 50%; background: #7FB0FF; }
/* windows live here */
.ph-desk__area { position: relative; grid-column: 2; grid-row: 2; outline: none; }
.ph-desk__zone {
  position: absolute; z-index: 9000; pointer-events: none; border-radius: var(--ph-r-card);
  background: color-mix(in srgb, #4591FF 22%, transparent); box-shadow: inset 0 0 0 2px #4591FF;
}
/* dock (as the design's XDock): a floating strip centred on the left edge, only as long as its icons; translucent and blurred */
.ph-dock {
  grid-column: 1; grid-row: 2; align-self: center; justify-self: center; z-index: 40;
  display: flex; flex-direction: column; align-items: center; gap: 4px; padding: 8px; border-radius: 14px;
  background: color-mix(in srgb, var(--hd-surface-overlay) 90%, transparent); color: var(--hd-c-fg);
  box-shadow: var(--hd-shadow-overlay), 0 0 0 1px var(--hd-c-divider); -webkit-backdrop-filter: blur(14px); backdrop-filter: blur(14px);
}
.ph-dock__sep { flex: none; width: 70%; height: 1px; margin: 4px 0; background: var(--hd-c-divider); }
.ph-dock__item { position: relative; display: grid; place-items: center; width: 48px; height: 48px; padding: 0; border: 0; background: none; cursor: pointer; }
/* hover and the focused window: a pressed squircle behind the icon (no scaling) */
.ph-dock__item::after { content: ''; position: absolute; inset: 0; z-index: -1; background: transparent; -webkit-mask: var(--ph-squircle) center / contain no-repeat; mask: var(--ph-squircle) center / contain no-repeat; }
.ph-dock__item:hover::after, .ph-dock__item.is-active::after { background: var(--hd-c-pressed); }
.ph-dock__item { isolation: isolate; }
.ph-dock__item:focus-visible { outline: 2px solid #7FB0FF; outline-offset: 0; }
/* the Overview button: a flat accent squircle */
.ph-dock__launcher { display: grid; place-items: center; width: 40px; height: 40px; background: var(--hd-accent, #4591FF); color: #fff; }
.ph-dock__item:hover .ph-dock__launcher, .ph-dock__item.is-active .ph-dock__launcher { background: color-mix(in srgb, var(--hd-accent, #4591FF) 75%, #fff); }
.ph-dock__item:has(.ph-dock__launcher)::after { display: none; }
/* running: a small dot on the screen-edge side; the focused one larger, in the accent */
.ph-dock__item.is-open::before {
  content: ''; position: absolute; left: -6px; top: 50%; width: 4px; height: 4px; transform: translateY(-50%);
  border-radius: 50%; background: var(--hd-c-fg-secondary);
}
.ph-dock__item.is-open.is-active::before { width: 5px; height: 5px; background: var(--hd-accent, #4591FF); }
/* quick switcher: a dark strip of the windows in the middle of the desktop */
.ph-wins { position: absolute; inset: 0; z-index: 9200; background: rgba(0, 0, 0, 0.72); -webkit-backdrop-filter: blur(6px); backdrop-filter: blur(6px); }
.ph-wins__head { position: absolute; left: 40px; right: 40px; top: 14px; display: flex; align-items: center; gap: 14px; color: #fff; font-family: var(--ph-font); }
.ph-wins__head h2 { margin: 0; font: 300 28px/1 var(--ph-font-light); }
.ph-wins__head > span { color: var(--ph-chrome-fg-muted); font-size: var(--hd-font-size-small); }
.ph-wins__zoom { margin-left: auto; display: flex; gap: 4px; }
.ph-wins__zoom button { display: grid; place-items: center; width: 34px; height: 30px; border: 0; background: rgba(255, 255, 255, 0.1); color: #C8C8C8; cursor: pointer; }
.ph-wins__zoom button.is-on { background: #4591FF; color: #fff; }
.ph-wins__hit { position: absolute; z-index: 9300; padding: 0; border: 0; border-radius: var(--ph-r-card); background: transparent; cursor: pointer; }
.ph-wins__hit.is-sel { box-shadow: 0 0 0 3px #7FB0FF; }
.ph-wins__label { position: absolute; left: 0; right: 0; bottom: 0; height: 30px; display: flex; align-items: center; justify-content: center; gap: 8px; color: #fff; font: 400 15px/1 var(--ph-font); }
/* the Overview: full screen over the desktop, slightly see-through and blurred */
.ph-ov {
  position: absolute; inset: 0; z-index: 9500; display: grid; grid-template-columns: 380px 1fr 480px;
  background: rgba(20, 20, 20, 0.92); color: #fff; backdrop-filter: blur(18px); font-family: var(--ph-font);
}
.ph-ov__events { position: relative; overflow: hidden; border-right: 1px solid rgba(255, 255, 255, 0.12); }
.ph-ov__wins { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); align-content: start; gap: var(--ph-s4); padding: var(--ph-s5, 32px) var(--ph-s5, 32px); overflow-y: auto; }
.ph-ov__win { position: relative; display: flex; flex-direction: column; gap: var(--ph-s2); }
.ph-ov__card {
  display: grid; place-items: center; aspect-ratio: 16 / 10; border: 0; border-radius: var(--ph-r-card); cursor: pointer;
  background: rgba(255, 255, 255, 0.08); box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.1);
}
.ph-ov__card:hover, .ph-ov__card:focus-visible { box-shadow: 0 0 0 2px #4591FF; outline: none; }
.ph-ov__name { display: flex; align-items: center; justify-content: center; gap: var(--ph-s2); font: 400 var(--hd-font-size-small)/1.2 var(--ph-font); }
.ph-ov__close {
  position: absolute; top: -8px; right: -8px; display: none; place-items: center; width: 24px; height: 24px; padding: 0; border: 0;
  background: #E5484D; color: #fff; cursor: pointer;
}
.ph-ov__win:hover .ph-ov__close, .ph-ov__close:focus-visible { display: grid; }
.ph-ov__none { grid-column: 1 / -1; align-self: center; text-align: center; color: var(--ph-chrome-fg-muted); }
.ph-ov__apps { padding: var(--ph-s4); border-left: 1px solid rgba(255, 255, 255, 0.12); overflow-y: auto; }
.ph-ov__hint { margin: var(--ph-s2) 4px 0; color: var(--ph-chrome-fg-muted); font-size: var(--hd-font-size-small); }
.ph-ov__grid { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: var(--ph-s1); margin-top: var(--ph-s3); }
/* spotlight */
.ph-launcher-scrim { position: absolute; inset: 0; z-index: 9500; display: grid; place-items: start center; padding-top: 10vh; background: rgba(0, 0, 0, 0.45); }
.ph-launcher {
  width: min(640px, 90%); padding: var(--ph-s4); border-radius: var(--ph-r-card);
  background: var(--hd-surface-overlay); color: #fff; box-shadow: var(--hd-shadow-overlay), 0 0 0 1px rgba(255, 255, 255, 0.1);
}
.ph-launcher__search { font-size: var(--hd-font-size-xlarge); padding: 0 var(--ph-s2); border-bottom: 1px solid rgba(255, 255, 255, 0.16); }
.ph-launcher__search :deep(.q-field__native), .ph-launcher__search :deep(.q-icon) { color: #fff; }
.ph-launcher__search :deep(.q-field__native::placeholder) { color: var(--ph-chrome-fg-muted); opacity: 1; }
.ph-launcher__grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(104px, 1fr)); gap: var(--ph-s2); margin-top: var(--ph-s4); }
.ph-launcher__app {
  display: flex; flex-direction: column; align-items: center; gap: var(--ph-s2); padding: var(--ph-s3) var(--ph-s1);
  border: 0; border-radius: var(--ph-r-card); background: none; color: #fff; font: 400 var(--hd-font-size-small)/1.2 var(--ph-font); cursor: pointer;
}
.ph-launcher__app:hover, .ph-launcher__app:focus-visible { background: rgba(255, 255, 255, 0.1); }
.ph-launcher__none { grid-column: 1 / -1; color: var(--ph-chrome-fg-muted); }
</style>
<style>
/* the menu bar's menus are teleported to <body>: dark, like the bar */
.ph-desk-menu { background: var(--hd-surface-overlay) !important; color: #fff !important; min-width: 240px; }
.ph-desk-menu .q-item__section--side { color: #aaa; font-size: var(--hd-font-size-xsmall); }
.ph-desk-menu .q-separator { background: rgba(255, 255, 255, 0.16); }
</style>
