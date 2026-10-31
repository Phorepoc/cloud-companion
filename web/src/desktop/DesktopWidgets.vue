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
        <p v-else-if="!mail.items.length" class="ph-wg__none">No mail.</p>
        <!-- the newest 20, read or not (unread marked), scrolling: as the Belle Mail widget -->
        <div v-else class="ph-wg__list" data-test="widget-mail-list">
          <button v-for="m in mail.items" :key="m.id" type="button" class="ph-wg__row" :class="{ 'is-unread': m.unread }" :disabled="editing" @click="emit('open', 'mail')">
            <span class="ph-wg__title">{{ m.from || '(no sender)' }}<span class="ph-wg__sec ph-wg__when">{{ mailTime(m.date) }}</span></span>
            <span class="ph-wg__sec ph-wg__line">{{ m.subject || '(no subject)' }}</span>
          </button>
        </div>
      </div>

      <!-- agenda -->
      <div v-else-if="w.kind === 'agenda'" class="ph-wg__panel">
        <header class="ph-wg__head"><SectionTile :section="byId.calendar" :size="32" /><b>Today</b></header>
        <p v-if="cal.error" class="ph-wg__none">{{ cal.error }}</p>
        <p v-else-if="!today.length" class="ph-wg__none">Nothing planned today.</p>
        <div v-else class="ph-wg__list">
          <button v-for="e in today" :key="e.href || e.title + e.start" type="button" class="ph-wg__row" :disabled="editing" @click="emit('open', 'calendar')">
            <span class="ph-wg__title">{{ e.title || '(no title)' }}</span>
            <span class="ph-wg__sec ph-wg__line">{{ e.allday ? 'All day' : timeLabel(toDate(e.start)) }}{{ e.location ? ' · ' + e.location : '' }}</span>
          </button>
        </div>
      </div>

      <!-- chats -->
      <div v-else-if="w.kind === 'chats'" class="ph-wg__panel">
        <header class="ph-wg__head"><SectionTile :section="byId.chats" :size="32" /><b>Chats</b></header>
        <p v-if="chats.error" class="ph-wg__none">{{ chats.error }}</p>
        <p v-else-if="!chats.items.length" class="ph-wg__none">No messages yet.</p>
        <div v-else class="ph-wg__list">
          <button v-for="c in chats.items" :key="c.key" type="button" class="ph-wg__row" :disabled="editing" @click="emit('open', 'chats')">
            <span class="ph-wg__title">{{ c.name || c.number }}<span class="ph-wg__sec ph-wg__when">{{ ago(now, c.last_ts) }}</span></span>
            <span class="ph-wg__sec ph-wg__line">{{ c.last_dir === 'out' ? 'You: ' : '' }}{{ c.last }}</span>
          </button>
        </div>
      </div>

      <!-- latest mail, one at a time (Belle "Latest mail"): ▲ ▼ step through, disabled at the ends -->
      <div v-else-if="w.kind === 'mail-stack'" class="ph-wg__panel ph-wg__stack">
        <header class="ph-wg__head"><SectionTile :section="byId.mail" :size="32" /><b>Latest mail</b>
          <span class="ph-wg__nav">
            <button type="button" class="ph-wg__arrow" aria-label="Newer mail" :disabled="editing || stackAt === 0" @click="stackAt--"><q-icon name="sym_o_keyboard_arrow_up" size="22px" /></button>
            <button type="button" class="ph-wg__arrow" aria-label="Older mail" data-test="widget-stack-older" :disabled="editing || stackAt >= mail.items.length - 1" @click="stackAt++"><q-icon name="sym_o_keyboard_arrow_down" size="22px" /></button>
          </span></header>
        <p v-if="mail.error" class="ph-wg__none">{{ mail.error }}</p>
        <p v-else-if="!mail.items.length" class="ph-wg__none">No mail.</p>
        <button v-else type="button" class="ph-wg__row ph-wg__one" :disabled="editing" data-test="widget-stack-mail" @click="emit('open', 'mail')">
          <span class="ph-wg__title">{{ stackMail?.from || '(no sender)' }}<span class="ph-wg__sec ph-wg__when">{{ stackAt + 1 }} / {{ mail.items.length }}</span></span>
          <span class="ph-wg__line">{{ stackMail?.subject || '(no subject)' }}</span>
          <span class="ph-wg__sec ph-wg__clamp">{{ stackMail?.preview }}</span>
        </button>
      </div>

      <!-- month (Belle calendar): today lit, a dot under days with events; tap opens Calendar -->
      <button v-else-if="w.kind === 'month'" type="button" class="ph-wg__panel ph-wg__month" :disabled="editing" @click="emit('open', 'calendar')">
        <b class="ph-wg__mtitle">{{ monthTitle }}</b>
        <span class="ph-wg__mgrid">
          <span v-for="d in WEEKDAYS" :key="'h' + d" class="ph-wg__sec">{{ d }}</span>
          <span v-for="(d, i) in monthDays" :key="i" class="ph-wg__day" :class="{ 'is-out': !d.inMonth, 'is-today': d.today }">{{ d.n }}<i v-if="d.events" /></span>
        </span>
      </button>

      <!-- contacts (Belle "Contacts, group"): squircle initials; tap opens Contacts -->
      <div v-else-if="w.kind === 'contacts'" class="ph-wg__panel ph-wg__people">
        <p v-if="people.error" class="ph-wg__none">{{ people.error }}</p>
        <p v-else-if="!people.items.length" class="ph-wg__none">No contacts yet.</p>
        <div v-else class="ph-wg__hlist">
          <button v-for="c in people.items" :key="c.href || c.name" type="button" class="ph-wg__person" :disabled="editing" :title="c.name" @click="emit('open', 'contacts')">
            <span class="hd-squircle ph-wg__avatar" :style="{ background: tint(c.name) }">{{ initials(c.name) }}</span><small>{{ c.given || c.name.split(' ')[0] }}</small>
          </button>
        </div>
      </div>

      <!-- notes (Belle Notes): yellow paper; ▲ ▼ browse, + writes a new one, the bin deletes the one shown. Kept in this browser. -->
      <div v-else-if="w.kind === 'notes'" class="ph-wg__notes">
        <textarea v-model="notes.list[notes.at]" class="ph-wg__paper" :readonly="editing" aria-label="Note" placeholder="Tap + to write a note." data-test="widget-note" @input="saveNotes" />
        <span class="ph-wg__notebar">
          <button type="button" aria-label="Previous note" :disabled="editing || notes.at === 0" @click="notes.at--"><q-icon name="sym_o_arrow_drop_up" size="28px" /></button>
          <button type="button" aria-label="Next note" :disabled="editing || notes.at >= notes.list.length - 1" @click="notes.at++"><q-icon name="sym_o_arrow_drop_down" size="28px" /></button>
          <button type="button" aria-label="New note" data-test="widget-note-add" :disabled="editing" @click="addNote"><q-icon name="sym_o_add" size="24px" /></button>
          <button type="button" aria-label="Delete note" :disabled="editing" @click="deleteNote"><q-icon name="sym_o_delete" size="22px" /></button>
        </span>
      </div>

      <!-- shortcuts (Belle shortcuts): four sections on their lit squircles -->
      <div v-else-if="w.kind === 'shortcuts'" class="ph-wg__panel ph-wg__shorts">
        <button v-for="sec in shortcutSections" :key="sec.id" type="button" class="ph-wg__short" :disabled="editing" @click="emit('open', sec.id)">
          <SectionTile :section="sec" :size="40" /><small>{{ sec.label }}</small>
        </button>
      </div>

      <!-- flip clock: the hours and minutes on two lit tiles, the date below -->
      <div v-else-if="w.kind === 'clock-flip'" class="ph-wg__flip">
        <span class="ph-wg__tile">{{ clock.hour }}</span><span class="ph-wg__tile">{{ clock.minute }}</span>
        <span class="ph-wg__sec ph-wg__flipdate">{{ dateText }}</span>
      </div>

      <!-- the time in words (Belle text clock) -->
      <div v-else-if="w.kind === 'clock-word'" class="ph-wg__word" role="img" :aria-label="'Clock, ' + clock.text">
        <b>{{ words[0] }}</b><span>{{ words[1] }}</span><small class="ph-wg__sec">{{ dateText }}</small>
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
import type { CalendarEvent, Contact, MailItem, SmsConversation } from '../proto'
import SectionTile from '../components/SectionTile.vue'
import { quiet, loadEvents, eventsOn, loadMail, loadChats, loadContacts, ago } from './glance'
import { ymd } from '../dates'
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
const people = reactive({ items: [] as Contact[], error: '' })
const today = computed(() => eventsOn(cal.items, props.now))
const has = (...k: WidgetKind[]) => list.some((w) => k.includes(w.kind))
function refresh() {
  if (has('mail', 'mail-stack')) loadMail(20).then((m) => { mail.unread = m.unread; mail.items = m.items; mail.error = '' }, (e) => { mail.error = quiet(e, 'Mail') })
  if (has('agenda', 'month')) loadEvents(monthStart.value, 42).then((ev) => { cal.items = ev; cal.error = '' }, (e) => { cal.error = quiet(e, 'The calendar') })
  if (has('chats')) loadChats(20).then((c) => { chats.items = c; chats.error = '' }, (e) => { chats.error = quiet(e, 'Chats') })
  if (has('contacts')) loadContacts().then((c) => { people.items = c; people.error = '' }, (e) => { people.error = quiet(e, 'Contacts') })
}
/** a mail's time: the time today, else the day and month */
function mailTime(date: string) {
  const d = new Date(date)
  if (isNaN(d.getTime())) return ''
  return ymd(d) === ymd(props.now) ? timeLabel(d) : d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
}

// latest mail, one at a time
const stackAt = ref(0)
const stackMail = computed(() => mail.items[Math.min(stackAt.value, mail.items.length - 1)])
watch(() => mail.items.length, (n) => { if (stackAt.value >= n) stackAt.value = Math.max(0, n - 1) })

// month: six weeks from the Monday on or before the 1st
const WEEKDAYS = ['M', 'T', 'W', 'T', 'F', 'S', 'S']
const monthStart = computed(() => { const f = new Date(props.now.getFullYear(), props.now.getMonth(), 1); return new Date(f.getFullYear(), f.getMonth(), 1 - ((f.getDay() + 6) % 7)) })
const monthTitle = computed(() => props.now.toLocaleDateString(undefined, { month: 'long', year: 'numeric' }))
const monthDays = computed(() => {
  const busy = new Set(cal.items.map((e) => ymd(toDate(e.start))))
  return Array.from({ length: 42 }, (_, i) => {
    const d = new Date(monthStart.value.getFullYear(), monthStart.value.getMonth(), monthStart.value.getDate() + i)
    return { n: d.getDate(), inMonth: d.getMonth() === props.now.getMonth(), today: ymd(d) === ymd(props.now), events: busy.has(ymd(d)) }
  })
})

// contacts: initials on a squircle, a steady colour per name
const initials = (n: string) => n.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0]).join('').toUpperCase()
const TINTS = ['#0067BC', '#09A7CC', '#EF5500', '#62B700', '#8E44AD', '#D81B60']
const tint = (n: string) => TINTS[[...n].reduce((a, c) => a + c.charCodeAt(0), 0) % TINTS.length]

// notes, kept in this browser
const NOTES_KEY = 'phecloud-desktop-notes'
const notes = reactive({ list: [''] as string[], at: 0 })
try { const saved = JSON.parse(localStorage.getItem(NOTES_KEY) || 'null'); if (Array.isArray(saved) && saved.length) notes.list = saved.map(String) } catch (e) { /* none yet */ }
function saveNotes() { try { localStorage.setItem(NOTES_KEY, JSON.stringify(notes.list)) } catch (e) { /* private mode */ } }
function addNote() { notes.list.push(''); notes.at = notes.list.length - 1; saveNotes() }
function deleteNote() { notes.list.splice(notes.at, 1); if (!notes.list.length) notes.list.push(''); notes.at = Math.min(notes.at, notes.list.length - 1); saveNotes() }

// shortcuts: the four primary sections
const shortcutSections = sections.filter((x) => x.primary).slice(0, 4)

// the time in words: "ten past" / "nine", or "quarter to" / "ten"
const NUM = ['twelve', 'one', 'two', 'three', 'four', 'five', 'six', 'seven', 'eight', 'nine', 'ten', 'eleven']
const words = computed(() => {
  const m = Math.round(props.now.getMinutes() / 5) * 5, h = props.now.getHours() + (m > 30 ? 1 : 0), hour = NUM[h % 12]
  const MIN: Record<number, string> = { 5: 'five', 10: 'ten', 15: 'quarter', 20: 'twenty', 25: 'twenty-five', 30: 'half' }
  if (m === 0 || m === 60) return [hour, "o'clock"]
  return m <= 30 ? [MIN[m] + ' past', hour] : [MIN[60 - m] + ' to', hour]
})
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
/* the list widgets hold more than fits (up to 20) and scroll, with a thin scrollbar so there is clearly more */
.ph-wg__list { flex: 1; min-height: 0; display: flex; flex-direction: column; overflow-y: auto; overscroll-behavior: contain; margin: 0 -8px; padding: 0 8px;
  scrollbar-width: thin; scrollbar-color: rgba(255, 255, 255, 0.35) transparent; }
.ph-wg__list > .ph-wg__row { flex: none; }
.ph-wg__row.is-unread .ph-wg__title { font-weight: 600; }
.ph-wg__row.is-unread::before { content: ''; position: absolute; left: -2px; top: 14px; width: 6px; height: 6px; border-radius: 50%; background: #4591FF; }
.ph-wg__list .ph-wg__row { position: relative; }
.ph-wg__nav { margin-left: auto; display: flex; gap: 2px; }
.ph-wg__arrow { display: grid; place-items: center; width: 30px; height: 30px; padding: 0; border: 0; -webkit-mask: var(--ph-squircle) center / contain no-repeat; mask: var(--ph-squircle) center / contain no-repeat; background: rgba(255, 255, 255, 0.1); color: #fff; cursor: pointer; }
.ph-wg__arrow:disabled { opacity: 0.3; cursor: default; }
.ph-wg__one { flex: 1; }
.ph-wg__clamp { display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; white-space: normal; }
/* month */
.ph-wg__month { width: 100%; text-align: left; color: #fff; font: inherit; cursor: pointer; }
.ph-wg__mtitle { font-weight: 400; font-size: var(--hd-font-size-default); }
.ph-wg__mgrid { flex: 1; display: grid; grid-template-columns: repeat(7, 1fr); align-items: center; justify-items: center; font-size: 13px; }
.ph-wg__day { position: relative; display: grid; place-items: center; width: 26px; height: 22px; }
.ph-wg__day.is-out { color: rgba(255, 255, 255, 0.35); }
.ph-wg__day.is-today { background: linear-gradient(#6AA8FF, #2E72E0); -webkit-mask: var(--ph-squircle) center / contain no-repeat; mask: var(--ph-squircle) center / contain no-repeat; }
.ph-wg__day i { position: absolute; bottom: 0; width: 4px; height: 4px; border-radius: 50%; background: #4591FF; }
/* contacts */
.ph-wg__people { justify-content: center; padding: 6px 10px; }
.ph-wg__hlist { display: flex; gap: 6px; overflow-x: auto; scrollbar-width: none; }
.ph-wg__person { flex: none; display: flex; flex-direction: column; align-items: center; gap: 2px; width: 58px; padding: 0; border: 0; background: none; color: #fff; font: inherit; cursor: pointer; }
.ph-wg__avatar { display: grid; place-items: center; width: 44px; height: 44px; font-size: 16px; font-weight: 600; }
.ph-wg__person small { max-width: 100%; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; font-size: 12px; }
/* notes: yellow paper and a dark bar */
.ph-wg__notes { height: 100%; display: flex; flex-direction: column; filter: drop-shadow(0 2px 3px rgba(0, 0, 0, 0.5)); }
.ph-wg__paper { flex: 1; resize: none; padding: 12px 14px; border: 0; border-radius: 4px 4px 0 0; background: linear-gradient(#FBF1BE, #F0DA85); color: #222; font: inherit; font-size: 16px; outline: none; }
.ph-wg__notebar { display: flex; justify-content: space-around; height: 40px; margin: 0 8px; border-radius: 0 0 12px 12px; background: linear-gradient(#2A2A2C, #121214); }
.ph-wg__notebar button { flex: 1; padding: 0; border: 0; background: none; color: #fff; cursor: pointer; } .ph-wg__notebar button:disabled { opacity: 0.3; cursor: default; }
/* shortcuts */
.ph-wg__shorts { flex-direction: row; align-items: center; justify-content: space-around; padding: 6px; }
.ph-wg__short { display: flex; flex-direction: column; align-items: center; gap: 2px; padding: 0; border: 0; background: none; color: #fff; font: inherit; font-size: 11px; cursor: pointer; }
/* flip clock: two lit tiles split by a hinge line */
.ph-wg__flip { height: 100%; display: grid; grid-template-columns: 1fr 1fr; grid-template-rows: 1fr auto; gap: 8px; }
.ph-wg__tile { position: relative; display: grid; place-items: center; border-radius: 14px; border: 1.5px solid #000; font-size: 72px; font-weight: 300;
  background: linear-gradient(rgba(255, 255, 255, 0.12), rgba(255, 255, 255, 0) 50%, rgba(0, 0, 0, 0.3) 50%), linear-gradient(#3A3B3F, #151518); }
.ph-wg__tile::after { content: ''; position: absolute; left: 0; right: 0; top: 50%; height: 1px; background: rgba(0, 0, 0, 0.8); }
.ph-wg__flipdate { grid-column: span 2; text-align: center; }
/* clock in words */
.ph-wg__word { height: 100%; display: flex; flex-direction: column; justify-content: center; padding: 0 8px; text-shadow: 0 1px 3px rgba(0, 0, 0, 0.6); }
.ph-wg__word b { font-size: 40px; font-weight: 300; line-height: 1.05; } .ph-wg__word span { font-size: 40px; font-weight: 600; line-height: 1.05; }
</style>
