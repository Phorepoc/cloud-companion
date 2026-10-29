<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- The desktop's widgets (after the Etesian Belle widgets): clocks, unread mail, the agenda and the latest chats, on the wallpaper
     under every window. A row opens its section as a window. View → Edit widgets turns on the phone's edit chrome: an accent
     outline, a red ✕ squircle to remove, a faint 90 px grid to snap to, and the Add widgets sheet. In edit mode a widget moves by
     dragging or, focused, with the arrow keys; Delete removes it; Done or Esc ends editing. No long-press, no context menu. -->
<template>
  <div class="ph-wg" :class="{ 'is-editing': editing }" data-test="widgets">
    <div v-for="w in list" :key="w.id" class="ph-wg__w" :class="['ph-wg__w--' + w.kind, { 'is-dragging': drag?.id === w.id }]" :style="boxOf(w)"
         :tabindex="editing ? 0 : undefined" :role="editing ? 'group' : undefined" :aria-label="editing ? KINDS[w.kind].label + ' widget, arrow keys move it' : undefined"
         :data-test="'widget-' + w.kind" @pointerdown="startDrag($event, w)" @keydown="onKey($event, w)">
      <!-- clocks -->
      <div v-if="w.kind === 'clock-digital'" class="ph-wg__panel ph-wg__digital">
        <time :datetime="now.toISOString()"><b>{{ clock.hour }}</b><span class="ph-wg__colon">:</span><b>{{ clock.minute }}</b><small v-if="clock.period" class="ph-wg__period">{{ clock.period }}</small></time>
        <span class="ph-wg__sec">{{ dateText }}</span>
      </div>
      <div v-else-if="w.kind === 'clock-analog'" class="ph-wg__analog" role="img" :aria-label="'Clock, ' + clock.text">
        <svg viewBox="0 0 100 100" aria-hidden="true">
          <circle cx="50" cy="50" r="46" class="ph-wg__face" />
          <line v-for="i in 12" :key="i" x1="50" y1="9" x2="50" :y2="i % 3 ? 13 : 16" class="ph-wg__tick" :transform="`rotate(${i * 30} 50 50)`" />
          <line x1="50" y1="50" x2="50" y2="28" class="ph-wg__hand ph-wg__hand--h" :transform="`rotate(${hourDeg} 50 50)`" />
          <line x1="50" y1="50" x2="50" y2="17" class="ph-wg__hand" :transform="`rotate(${minDeg} 50 50)`" />
          <circle cx="50" cy="50" r="3" fill="#4591FF" />
        </svg>
      </div>

      <!-- mail -->
      <div v-else-if="w.kind === 'mail'" class="ph-wg__panel">
        <header class="ph-wg__head"><SectionTile :section="byId.mail" :size="32" /><b>Mail</b>
          <span v-if="mail.unread" class="ph-wg__count" data-test="widget-mail-count">{{ mail.unread }} unread</span></header>
        <p v-if="mail.error" class="ph-wg__none">{{ mail.error }}</p>
        <p v-else-if="!mail.items.length" class="ph-wg__none">No unread mail.</p>
        <button v-for="m in mail.items" :key="m.id" type="button" class="ph-wg__row" :disabled="editing" @click="emit('open', 'mail')">
          <span class="ph-wg__title">{{ m.from || '(no sender)' }}</span><span class="ph-wg__sec ph-wg__line">{{ m.subject || '(no subject)' }}</span>
        </button>
      </div>

      <!-- agenda -->
      <div v-else-if="w.kind === 'agenda'" class="ph-wg__panel">
        <header class="ph-wg__head"><SectionTile :section="byId.calendar" :size="32" /><b>Today</b></header>
        <p v-if="cal.error" class="ph-wg__none">{{ cal.error }}</p>
        <p v-else-if="!today.length" class="ph-wg__none">Nothing planned today.</p>
        <button v-for="e in today.slice(0, 4)" :key="e.href || e.title + e.start" type="button" class="ph-wg__row" :disabled="editing" @click="emit('open', 'calendar')">
          <span class="ph-wg__title">{{ e.title || '(no title)' }}</span>
          <span class="ph-wg__sec ph-wg__line">{{ e.allday ? 'All day' : timeLabel(toDate(e.start)) }}{{ e.location ? ' · ' + e.location : '' }}</span>
        </button>
      </div>

      <!-- chats -->
      <div v-else-if="w.kind === 'chats'" class="ph-wg__panel">
        <header class="ph-wg__head"><SectionTile :section="byId.chats" :size="32" /><b>Chats</b></header>
        <p v-if="chats.error" class="ph-wg__none">{{ chats.error }}</p>
        <p v-else-if="!chats.items.length" class="ph-wg__none">No messages yet.</p>
        <button v-for="c in chats.items" :key="c.key" type="button" class="ph-wg__row" :disabled="editing" @click="emit('open', 'chats')">
          <span class="ph-wg__title">{{ c.name || c.number }}<span class="ph-wg__sec ph-wg__when">{{ ago(now, c.last_ts) }}</span></span>
          <span class="ph-wg__sec ph-wg__line">{{ c.last_dir === 'out' ? 'You: ' : '' }}{{ c.last }}</span>
        </button>
      </div>

      <template v-if="editing">
        <i class="ph-wg__outline" aria-hidden="true" />
        <button type="button" class="ph-wg__x" :aria-label="'Remove ' + KINDS[w.kind].label" title="Remove" :data-test="'widget-remove-' + w.kind"
                @pointerdown.stop @click="remove(w)">
          <span class="hd-squircle" aria-hidden="true"><q-icon name="sym_o_close" size="18px" /></span>
        </button>
      </template>
    </div>

    <!-- edit mode: add widgets -->
    <aside v-if="editing" class="ph-wg__sheet" aria-label="Add widgets" data-test="widget-sheet" @keydown.esc="emit('done')">
      <h2>Add widgets</h2>
      <section v-for="g in groups" :key="g.group">
        <h3>{{ g.group }}</h3>
        <button v-for="k in g.kinds" :key="k" type="button" class="ph-wg__add" :data-test="'widget-add-' + k" @click="add(k)">
          {{ KINDS[k].label }}<q-icon name="sym_o_add" size="20px" aria-hidden="true" />
        </button>
      </section>
      <p v-if="full" class="ph-wg__none" role="status">There is no room left. Remove or move a widget first.</p>
      <q-btn unelevated no-caps color="primary" label="Done" class="full-width q-mt-md" data-test="widgets-done" @click="emit('done')" />
    </aside>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { state } from '../cloud'
import { sections } from '../nav'
import { timeLabel, toDate, dayLabel } from '../dates'
import type { CalendarEvent, MailItem, SmsConversation } from '../proto'
import SectionTile from '../components/SectionTile.vue'
import { quiet, loadEvents, eventsOn, loadUnread, loadChats, ago } from './glance'
import { CELL, MARGIN, KINDS, loadWidgets, saveWidgets, gridSize, fits, freeSpot, type Widget, type WidgetKind } from './widgets'

const props = defineProps<{ now: Date; area: { w: number; h: number }; editing: boolean }>()
const emit = defineEmits<{ open: [id: string]; done: [] }>()
const byId = Object.fromEntries(sections.map((s) => [s.id, s]))

const list = reactive<Widget[]>(loadWidgets())
watch(list, () => saveWidgets(list), { deep: true })
const grid = computed(() => gridSize(props.area.w, props.area.h))
const full = ref(false)

const groups = computed(() => {
  const out: { group: string; kinds: WidgetKind[] }[] = []
  for (const k of Object.keys(KINDS) as WidgetKind[]) {
    const g = KINDS[k].group
    const at = out.find((x) => x.group === g)
    if (at) at.kinds.push(k); else out.push({ group: g, kinds: [k] })
  }
  return out
})

// ---- placing ----
const drag = ref<{ id: string; dx: number; dy: number } | null>(null) // the widget being dragged and its offset in px
const boxOf = (w: Widget) => {
  const k = KINDS[w.kind]
  const d = drag.value?.id === w.id ? drag.value : null
  return { left: MARGIN + w.x * CELL + (d ? d.dx : 0) + 'px', top: MARGIN + w.y * CELL + (d ? d.dy : 0) + 'px', width: k.w * CELL - 12 + 'px', height: k.h * CELL - 12 + 'px' }
}
function moveTo(w: Widget, x: number, y: number) {
  if (fits({ ...w, x, y }, list, grid.value.cols, grid.value.rows)) { w.x = x; w.y = y; return true }
  return false
}
function startDrag(e: PointerEvent, w: Widget) {
  if (!props.editing || e.button !== 0) return
  const sx = e.clientX, sy = e.clientY
  drag.value = { id: w.id, dx: 0, dy: 0 }
  const move = (ev: PointerEvent) => { if (drag.value) Object.assign(drag.value, { dx: ev.clientX - sx, dy: ev.clientY - sy }) }
  const up = (ev: PointerEvent) => {
    removeEventListener('pointermove', move); removeEventListener('pointerup', up)
    drag.value = null
    // snap to the nearest cell; a spot that is taken or off the grid leaves the widget where it was
    moveTo(w, w.x + Math.round((ev.clientX - sx) / CELL), w.y + Math.round((ev.clientY - sy) / CELL))
  }
  addEventListener('pointermove', move); addEventListener('pointerup', up)
}
function onKey(e: KeyboardEvent, w: Widget) {
  if (!props.editing || e.target !== e.currentTarget) return
  const step: Record<string, [number, number]> = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }
  if (step[e.key]) { e.preventDefault(); moveTo(w, w.x + step[e.key][0], w.y + step[e.key][1]) }
  else if (e.key === 'Delete' || e.key === 'Backspace') { e.preventDefault(); remove(w) }
  else if (e.key === 'Escape') emit('done')
}
function add(kind: WidgetKind) {
  const at = freeSpot(kind, list, grid.value.cols, grid.value.rows)
  full.value = !at
  if (at) list.push({ id: 'w' + Date.now().toString(36) + Math.random().toString(36).slice(2, 6), kind, ...at })
}
function remove(w: Widget) {
  const i = list.findIndex((x) => x.id === w.id)
  if (i >= 0) list.splice(i, 1)
  full.value = false
}

// ---- clocks ----
// the time as the menu bar shows it (the browser's locale: 24 h, or 12 h with AM / PM)
const clock = computed(() => {
  const parts = new Intl.DateTimeFormat([], { hour: '2-digit', minute: '2-digit' }).formatToParts(props.now)
  const part = (t: string) => parts.find((p) => p.type === t)?.value || ''
  return { hour: part('hour'), minute: part('minute'), period: part('dayPeriod'), text: parts.map((p) => p.value).join('') }
})
const dateText = computed(() => dayLabel(props.now))
const minDeg = computed(() => props.now.getMinutes() * 6)
const hourDeg = computed(() => (props.now.getHours() % 12) * 30 + props.now.getMinutes() / 2)

// ---- data: loaded for the kinds on the desktop, refreshed every two minutes ----
const mail = reactive({ items: [] as MailItem[], unread: 0, error: '' })
const cal = reactive({ items: [] as CalendarEvent[], error: '' })
const chats = reactive({ items: [] as SmsConversation[], error: '' })
const today = computed(() => eventsOn(cal.items, props.now))
const has = (k: WidgetKind) => list.some((w) => w.kind === k)
function refresh() {
  if (has('mail')) loadUnread().then((m) => { mail.unread = m.unread; mail.items = m.items; mail.error = '' }, (e) => { mail.error = quiet(e, 'Mail') })
  if (has('agenda')) loadEvents(props.now, 1).then((ev) => { cal.items = ev; cal.error = '' }, (e) => { cal.error = quiet(e, 'The calendar') })
  if (has('chats')) loadChats().then((c) => { chats.items = c; chats.error = '' }, (e) => { chats.error = quiet(e, 'Chats') })
}
// a newly added kind loads at once
watch(() => list.map((w) => w.kind).sort().join(), refresh)
let timer: ReturnType<typeof setInterval> | undefined
// after a dropped connection comes back, catch up
watch(() => state.status, (s, old) => { if (s === 'online' && old !== 'online') refresh() })
onMounted(() => { refresh(); timer = setInterval(refresh, 120000) })
onBeforeUnmount(() => clearInterval(timer))
</script>

<style scoped>
/* the wallpaper layer: under the windows (which stack from z-index 1) */
.ph-wg { position: absolute; inset: 0; z-index: 0; }
.ph-wg.is-editing {
  background-image: linear-gradient(rgba(127, 127, 127, 0.14) 1px, transparent 1px), linear-gradient(90deg, rgba(127, 127, 127, 0.14) 1px, transparent 1px);
  background-size: 90px 90px; background-position: 24px 24px;
}
.ph-wg__w { position: absolute; color: #fff; font-family: var(--ph-font); border-radius: 12px; }
.ph-wg.is-editing .ph-wg__w { cursor: grab; touch-action: none; }
.ph-wg__w.is-dragging { z-index: 1; cursor: grabbing; opacity: 0.9; }
.ph-wg__w:focus-visible { outline: 2px solid #fff; outline-offset: 4px; }
/* the Belle panel: dark glass with a light top edge */
.ph-wg__panel {
  height: 100%; box-sizing: border-box; display: flex; flex-direction: column; gap: 4px; padding: 12px 14px; overflow: hidden;
  border-radius: 12px; border: 1px solid rgba(255, 255, 255, 0.15);
  background: linear-gradient(rgba(255, 255, 255, 0.12), rgba(255, 255, 255, 0) 50%), rgba(0, 0, 0, 0.72);
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.35);
}
.ph-wg__head { display: flex; align-items: center; gap: 10px; margin-bottom: 4px; font-size: var(--hd-font-size-default); }
.ph-wg__count { margin-left: auto; padding: 1px 10px; border-radius: 12px; background: #4591FF; font-size: var(--hd-font-size-xsmall); font-weight: 700; }
.ph-wg__none { margin: 4px 0; font-size: var(--hd-font-size-small); color: rgba(255, 255, 255, 0.6); }
.ph-wg__row {
  display: flex; flex-direction: column; min-width: 0; padding: 6px 4px; border: 0; border-top: 1px solid rgba(255, 255, 255, 0.12);
  background: none; color: inherit; font: inherit; text-align: left; cursor: pointer;
}
.ph-wg__row:hover:not(:disabled), .ph-wg__row:focus-visible { background: rgba(255, 255, 255, 0.1); }
.ph-wg__row:disabled { cursor: inherit; }
.ph-wg__title { display: flex; font-weight: 700; font-size: var(--hd-font-size-small); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.ph-wg__when { margin-left: auto; padding-left: 8px; font-weight: 400; }
.ph-wg__sec { color: rgba(255, 255, 255, 0.6); }
.ph-wg__line { font-size: var(--hd-font-size-small); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
/* big clock: light numerals, the date under them */
.ph-wg__digital { align-items: center; justify-content: center; gap: 2px; }
.ph-wg__digital time { font: 300 72px/1 var(--ph-font-light); letter-spacing: 1px; }
.ph-wg__colon { opacity: 0.6; margin: 0 2px; }
.ph-wg__period { margin-left: 8px; font-size: 22px; opacity: 0.7; }
.ph-wg__digital b { font-weight: 300; }
/* analog: the Belle dial on its own */
.ph-wg__analog { height: 100%; display: grid; place-items: center; }
.ph-wg__analog svg { width: 100%; height: 100%; filter: drop-shadow(0 4px 10px rgba(0, 0, 0, 0.4)); }
.ph-wg__face { fill: #1E1E21; stroke: rgba(255, 255, 255, 0.25); stroke-width: 1.5; }
.ph-wg__tick { stroke: rgba(255, 255, 255, 0.8); stroke-width: 2; stroke-linecap: round; }
.ph-wg__hand { stroke: #fff; stroke-width: 3; stroke-linecap: round; }
.ph-wg__hand--h { stroke-width: 4.5; }
/* edit chrome, as on the phone: accent outline and a red ✕ squircle at the top right */
.ph-wg__outline { position: absolute; inset: -4px; border: 2px solid #4591FF; border-radius: 14px; pointer-events: none; }
.ph-wg__x { position: absolute; top: -14px; right: -14px; padding: 0; border: 0; background: none; cursor: pointer; }
.ph-wg__x .hd-squircle { display: grid; place-items: center; width: 30px; height: 30px; color: #fff; background: linear-gradient(#F78673, #EF2C10); }
.ph-wg__x:focus-visible { outline: 2px solid #fff; outline-offset: 2px; border-radius: 8px; }
/* the Add widgets sheet: an overlay card at the right edge */
.ph-wg__sheet {
  position: absolute; z-index: 9300; top: 12px; right: 12px; bottom: 12px; width: 300px; overflow-y: auto; padding: var(--ph-s4);
  border-radius: var(--ph-r-card); background: #141414; color: #fff; box-shadow: var(--hd-shadow-overlay);
}
.ph-wg__sheet h2 { margin: 0 0 var(--ph-s2); font: 300 var(--ph-text-title)/1.2 var(--ph-font-light); }
.ph-wg__sheet h3 { margin: var(--ph-s3) 0 var(--ph-s1); font: 700 var(--hd-font-size-small)/1.2 var(--ph-font); color: var(--ph-chrome-fg-muted); }
.ph-wg__add {
  display: flex; align-items: center; justify-content: space-between; width: 100%; padding: 8px 10px; border: 0; border-radius: 8px;
  background: none; color: inherit; font: inherit; text-align: left; cursor: pointer;
}
.ph-wg__add:hover, .ph-wg__add:focus-visible { background: rgba(255, 255, 255, 0.1); }
.ph-wg__add .q-icon { color: #4591FF; }
</style>
