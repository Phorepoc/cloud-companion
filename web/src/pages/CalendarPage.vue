<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: AGPL-3.0-only -->
<template>
  <q-page class="ph-cal-page" :style-fn="fillPage">
   <div class="ph-cal-box">
   <div class="ph-cal" :class="{ 'is-agenda': view === 'agenda' }">
    <section class="ph-cal__month ph-scroll" aria-label="Month">
    <!-- the app's coloured header: the month, with the arrows and Today (Quasar/Examples, Calendar) -->
    <header class="ph-cal__head" role="group" aria-label="Month">
      <q-btn flat round text-color="white" icon="sym_o_chevron_left" aria-label="Previous month" data-test="cal-prev" @click="shift(-1)" />
      <h2 class="ph-month" aria-live="polite" data-test="cal-month">{{ monthLabel(month) }}</h2>
      <q-btn flat round text-color="white" icon="sym_o_chevron_right" aria-label="Next month" data-test="cal-next" @click="shift(1)" />
      <q-btn flat no-caps text-color="white" label="Today" data-test="cal-today" @click="today" />
      <!-- with a header (the classic bar, a desktop window) New event is there -->
      <q-btn v-if="!chrome.hosted" flat round text-color="white" icon="sym_o_add" aria-label="New event" data-test="event-new" @click="openNew()" />
    </header>
    <div class="ph-cal__bar">
      <q-btn-toggle v-model="view" no-caps unelevated toggle-color="primary"
                    :options="[{ label: 'Month', value: 'month', icon: 'sym_o_calendar_view_month' }, { label: 'Agenda', value: 'agenda', icon: 'sym_o_view_agenda' }]"
                    data-test="cal-view" />
    </div>

    <q-banner v-if="error" inline-actions class="ph-note is-error ph-cal__note" role="alert">
      <template #avatar><q-icon name="sym_o_error" aria-hidden="true" /></template>
      {{ error }}
      <template #action><q-btn flat no-caps color="primary" label="Try again" @click="load" /></template>
    </q-banner>

    <!-- month grid -->
    <div v-if="view === 'month'" class="ph-grid" role="grid" :aria-label="monthLabel(month)" data-test="cal-grid">
      <div class="ph-dow" role="row"><span v-for="d in dows" :key="d" role="columnheader">{{ d }}</span></div>
      <div v-for="(week, wi) in weeks" :key="wi" class="ph-week" role="row">
        <button v-for="d in week" :key="ymd(d)" type="button" role="gridcell" class="ph-cell"
                :class="{ 'is-out': d.getMonth() !== month.getMonth(), 'is-today': ymd(d) === ymd(new Date()), 'is-sel': ymd(d) === selected }"
                :aria-label="cellLabel(d)" :aria-selected="ymd(d) === selected" :data-test="'cal-day-' + ymd(d)" @click="selected = ymd(d)">
          <span class="ph-num hd-sqm">{{ d.getDate() }}</span>
          <span v-for="e in (byDay[ymd(d)] || []).slice(0, 2)" :key="e.href" class="ph-chip" :class="{ 'ph-chip-dot': phone }">{{ phone ? '' : e.title }}</span>
          <span v-if="(byDay[ymd(d)] || []).length > 2" class="ph-more">+{{ byDay[ymd(d)].length - 2 }}</span>
        </button>
      </div>
    </div>
    </section>
    <section v-if="view === 'month'" class="ph-day ph-cal__day ph-scroll" :aria-label="dayLabel(toDate(selected))" data-test="cal-selected">
      <h3 class="ph-day-head" :class="{ 'is-today': selected === ymd(new Date()) }">{{ dayLabel(toDate(selected)) }}</h3>
      <p v-if="!(byDay[selected] || []).length" class="ph-muted">Nothing planned.</p>
      <ul v-else class="ph-list">
        <li v-for="e in byDay[selected]" :key="e.href">
          <button type="button" class="ph-row" data-test="event-row" @click="openView(e)">
            <span class="ph-when ph-small ph-muted">{{ whenLabel(e) }}</span>
            <span class="ph-row-main"><span class="ph-row-title">{{ e.title }}</span><span v-if="e.location" class="ph-small ph-muted">{{ e.location }}</span></span>
            <q-badge v-if="e.recurs" outline color="primary" label="Repeats" />
          </button>
        </li>
      </ul>
    </section>

    <!-- agenda -->
    <section v-if="view === 'agenda'" class="ph-cal__day ph-scroll" aria-label="Agenda">
      <HdStatusState v-if="loading && !events.length" state="loading" title="Loading events" text="This takes a moment." />
      <HdStatusState v-else-if="!loading && !events.length && !error" icon="calendar" title="Nothing on this month"
                  text="Add an event. It will show on every device that syncs with your hub.">
        <q-btn outline color="primary" label="New event" @click="openNew()" />
      </HdStatusState>
      <section v-for="g in groups" :key="g.key" class="ph-day" :aria-label="g.label">
        <h3 class="ph-day-head" :class="{ 'is-today': g.today }">{{ g.label }}<span v-if="g.today" class="ph-small"> (today)</span></h3>
        <ul class="ph-list">
          <li v-for="e in g.items" :key="e.href">
            <button type="button" class="ph-row" data-test="event-row" @click="openView(e)">
              <span class="ph-when ph-small ph-muted">{{ whenLabel(e) }}</span>
              <span class="ph-row-main"><span class="ph-row-title">{{ e.title }}</span><span v-if="e.location" class="ph-small ph-muted">{{ e.location }}</span></span>
              <q-badge v-if="e.recurs" outline color="primary" label="Repeats" />
            </button>
          </li>
        </ul>
      </section>
    </section>
   </div>
   </div>

    <q-dialog v-model="viewOpen">
      <q-card v-if="current" class="ph-dialog" data-test="event-view">
        <q-card-section>
          <h2 data-test="event-view-title">{{ current.title }}</h2>
          <p class="ph-muted q-mb-sm">{{ fullWhen(current) }}</p>
          <p v-if="current.location"><q-icon name="sym_o_location_on" aria-hidden="true" /> {{ current.location }}</p>
          <p v-if="current.notes" class="ph-note-text">{{ current.notes }}</p>
          <p v-if="current.recurs" class="ph-small ph-muted">This event repeats. Editing changes the whole series.</p>
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat color="negative" label="Delete" data-test="event-delete" @click="remove(current)" />
          <q-btn flat label="Close" v-close-popup />
          <q-btn unelevated color="primary" label="Edit" data-test="event-edit" @click="openNew(current)" />
        </q-card-actions>
      </q-card>
    </q-dialog>

    <!-- the N9 time picker (HdTimePicker): hour and minute wheels, Done / Cancel -->
    <q-dialog v-model="timeOpen">
      <HdTimePicker v-model="timeValue" hour24 :title="timeField === 'startTime' ? 'Start time' : 'End time'" :minute-step="5"
                    @accept="(v: string) => { form[timeField] = v; timeOpen = false }" @cancel="timeOpen = false" />
    </q-dialog>

    <q-dialog v-model="editOpen" persistent>
      <q-card class="ph-dialog" data-test="event-form">
        <form @submit.prevent="save">
          <q-card-section class="ph-form">
            <h2>{{ form.href ? 'Edit event' : 'New event' }}</h2>
            <q-input v-model="form.title" label="Title" outlined autofocus data-test="event-title" />
            <q-toggle v-model="form.allday" label="All day" data-test="event-allday" />
            <div class="ph-two">
              <q-input v-model="form.startDate" label="Starts" type="date" outlined data-test="event-start-date" />
              <q-input v-if="!form.allday" v-model="form.startTime" label="Time" type="time" outlined data-test="event-start-time">
                <template #append><q-btn flat round dense icon="sym_o_schedule" aria-label="Pick the start time" @click="pickTime('startTime')" /></template>
              </q-input>
            </div>
            <div class="ph-two">
              <q-input v-model="form.endDate" label="Ends" type="date" outlined data-test="event-end-date" />
              <q-input v-if="!form.allday" v-model="form.endTime" label="Time" type="time" outlined data-test="event-end-time">
                <template #append><q-btn flat round dense icon="sym_o_schedule" aria-label="Pick the end time" @click="pickTime('endTime')" /></template>
              </q-input>
            </div>
            <p v-if="!form.allday" class="ph-small ph-muted" data-test="event-zone">Times are in your time zone ({{ zone }}).</p>
            <q-input v-model="form.location" label="Place" outlined data-test="event-location" />
            <q-input v-model="form.notes" label="Notes" type="textarea" outlined autogrow data-test="event-notes" />
            <p v-if="formError" class="ph-error" role="alert"><q-icon name="sym_o_error" size="20px" aria-hidden="true" /> {{ formError }}</p>
          </q-card-section>
          <q-card-actions align="right">
            <q-btn flat label="Cancel" :disable="saving" data-test="event-cancel" @click="editOpen = false" />
            <q-btn unelevated color="primary" label="Save" type="submit" :loading="saving" data-test="event-save" />
          </q-card-actions>
        </form>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { useQuasar } from 'quasar'
import { hubCall, NO_HUB } from '../cloud'
import { toast } from '../toast'
import { ymd, toDate, addDays, toFields, fromFields, monthLabel, dayLabel, timeLabel, gridDays, browserZone, type EventFields } from '../dates'
import type { CalendarEvent } from '../proto'

/** The edit form: the event's text fields and its start and end as date and time inputs. */
interface EventForm extends EventFields { href?: string; etag?: string; title: string; location: string; notes: string }
import { HdStatusState } from '@etesian-design/vue'
import { latest } from '../latest'
import { useChrome } from '@etesian-design/shell'
import { fillPage } from '../fillPage'
import { HdTimePicker } from '@etesian-design/vue'

const $q = useQuasar()
const phone = computed(() => $q.screen.lt.sm)
const now = new Date()
const month = ref(new Date(now.getFullYear(), now.getMonth(), 1))
const events = ref<CalendarEvent[]>([])
const loading = ref(false)
const error = ref('')
const current = ref<CalendarEvent | null>(null)
const viewOpen = ref(false)
const editOpen = ref(false)
const saving = ref(false)
const formError = ref('')
const form = ref<EventForm>({ title: '', location: '', notes: '', allday: false, startDate: '', startTime: '', endDate: '', endTime: '' })
const zone = browserZone()
const view = ref('month')

const chrome = useChrome(() => ({ title: 'Calendar', tools: [{ icon: 'add', label: 'New event' }] }))
chrome.on('tool', (l) => { if (l === 'New event') openNew() })
const selected = ref(ymd(now))
const dows = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']
try { view.value = localStorage.getItem('phecloud-cal-view') || 'month' } catch (e) { /* ignore */ }
watch(view, (v) => { try { localStorage.setItem('phecloud-cal-view', v) } catch (e) { /* ignore */ } })

const monthLoad = latest()
async function load() {
  const t = monthLoad.take()
  loading.value = true
  error.value = ''
  try {
    // the grid shows days of the neighbouring months too: ask for the whole grid
    const days = gridDays(month.value)
    const r = (await hubCall({ op: 'events.list', from: ymd(days[0]), to: ymd(addDays(days[41], 1)) })).events || []
    if (monthLoad.is(t)) events.value = r // › tapped again meanwhile: that month's events, not this one's
  } catch (e) {
    if (monthLoad.is(t)) error.value = /not set up/i.test((e as Error).message) ? NO_HUB : (e as Error).message
  } finally {
    if (monthLoad.is(t)) loading.value = false
  }
}
const shift = (n: number) => { month.value = new Date(month.value.getFullYear(), month.value.getMonth() + n, 1); selected.value = ymd(month.value); load() }
const today = () => { const d = new Date(); month.value = new Date(d.getFullYear(), d.getMonth(), 1); selected.value = ymd(d); load() }

const startOf = (e: CalendarEvent) => toDate(e.start)
// the days an event covers (a multi-day or all-day event appears on each of them)
function daysOf(e: CalendarEvent) {
  const s = startOf(e)
  let last = toDate(e.end || e.start)
  if (e.allday) last = addDays(last, -1)
  else if (last > s && last.getHours() === 0 && last.getMinutes() === 0) last = addDays(last, -1)
  const out = []
  for (let d = new Date(s.getFullYear(), s.getMonth(), s.getDate()); d <= last && out.length < 60; d = addDays(d, 1)) out.push(ymd(d))
  return out.length ? out : [ymd(s)]
}
// the events by start, sorted once for the grid and the list
const sorted = computed(() => [...events.value].sort((a, b) => startOf(a).getTime() - startOf(b).getTime()))
const byDay = computed(() => {
  const m: Record<string, CalendarEvent[]> = {}
  for (const e of sorted.value) for (const k of daysOf(e)) (m[k] = m[k] || []).push(e)
  return m
})
const weeks = computed(() => { const d = gridDays(month.value); return [0, 1, 2, 3, 4, 5].map((i) => d.slice(i * 7, i * 7 + 7)) })
const cellLabel = (d: Date) => `${dayLabel(d)}, ${(byDay.value[ymd(d)] || []).length} events`

// agenda: events of the shown month, by start day
const groups = computed(() => {
  const m0 = month.value
  const m1 = new Date(m0.getFullYear(), m0.getMonth() + 1, 1)
  const byStart = new Map()
  for (const e of sorted.value) {
    let d = startOf(e)
    if (d < m0) d = m0
    if (d >= m1) continue
    const key = ymd(d)
    if (!byStart.has(key)) byStart.set(key, { key, label: dayLabel(d), today: key === ymd(new Date()), items: [] })
    byStart.get(key).items.push(e)
  }
  return [...byStart.values()]
})
const whenLabel = (e: CalendarEvent) => (e.allday ? 'All day' : timeLabel(startOf(e)))
function fullWhen(e: CalendarEvent) {
  const s = startOf(e)
  if (e.allday) { const last = addDays(toDate(e.end || e.start), -1); return last > s ? `${dayLabel(s)} to ${dayLabel(last)}` : dayLabel(s) }
  const en = toDate(e.end || e.start)
  return `${dayLabel(s)}, ${timeLabel(s)} to ${ymd(en) === ymd(s) ? timeLabel(en) : dayLabel(en) + ', ' + timeLabel(en)}`
}

const openView = (e: CalendarEvent) => { current.value = e; viewOpen.value = true }
function openNew(ev?: CalendarEvent) {
  viewOpen.value = false
  formError.value = ''
  if (ev) form.value = { href: ev.href, etag: ev.etag, title: ev.title, location: ev.location || '', notes: ev.notes || '', ...toFields(ev) }
  else {
    const d = view.value === 'month' ? selected.value : ymd(month.value.getMonth() === now.getMonth() && month.value.getFullYear() === now.getFullYear() ? now : month.value)
    form.value = { href: '', etag: '', title: '', location: '', notes: '', allday: false, startDate: d, startTime: '09:00', endDate: d, endTime: '10:00' }
  }
  editOpen.value = true
}

const timeOpen = ref(false)
const timeField = ref<'startTime' | 'endTime'>('startTime')
const timeValue = ref('09:00')
function pickTime(f: 'startTime' | 'endTime') { timeField.value = f; timeValue.value = form.value[f] || '09:00'; timeOpen.value = true }

async function save() {
  const f = form.value
  if (!f.title.trim()) { formError.value = 'Enter a title for this event.'; return }
  if (!f.startDate) { formError.value = 'Choose a start date.'; return }
  const { start, end } = fromFields(f)
  if (end < start) { formError.value = 'The event cannot end before it starts.'; return }
  saving.value = true
  formError.value = ''
  try {
    await hubCall({ op: 'events.save', event: { href: f.href || undefined, etag: f.etag || undefined, title: f.title.trim(), start, end, allday: f.allday,
      tz: f.allday ? undefined : zone || undefined, location: f.location, notes: f.notes } })
    editOpen.value = false
    toast(f.href ? 'Event saved' : 'Event added')
    await load()
  } catch (e) {
    formError.value = (e as Error).message
    if (/another device/.test(formError.value)) load() // the newer version, so the next edit starts from it
  } finally {
    saving.value = false
  }
}

function remove(e: CalendarEvent) {
  viewOpen.value = false
  events.value = events.value.filter((x) => x.href !== e.href)
  toast('Event deleted', {
    resume: ['hub.call', { op: 'events.delete', href: e.href, etag: e.etag }],
    undo: () => load(), // from the server: the month may have changed or been reloaded since
    onExpire: async () => {
      try { await hubCall({ op: 'events.delete', href: e.href, etag: e.etag }) } catch (err) { error.value = (err as Error).message; await load() }
    },
  })
}

onMounted(load)
</script>

<style scoped>
/* month and the chosen day side by side from 860 px of room (the page or a window), else one under the other; each scrolls on its own */
.ph-cal-page { display: flex; flex-direction: column; }
.ph-cal-box { flex: 1; min-height: 0; display: flex; container-type: inline-size; }
.ph-cal { flex: 1; min-width: 0; min-height: 0; display: flex; flex-direction: column; overflow-y: auto; background: var(--hd-c-bg); }
.ph-cal__month { flex: none; overflow: visible; }
.ph-cal__day { flex: none; overflow: visible; padding: 8px 0 16px; }
@container (min-width: 860px) {
  .ph-cal { display: grid; grid-template-columns: 1fr minmax(300px, 34%); overflow: hidden; }
  .ph-cal__month, .ph-cal__day { min-height: 0; overflow-y: auto; }
  .ph-cal__day { border-left: 1px solid var(--hd-c-divider); }
  /* the agenda is one list under the month bar */
  .ph-cal.is-agenda { display: flex; overflow-y: auto; }
  .ph-cal.is-agenda .ph-cal__month, .ph-cal.is-agenda .ph-cal__day { overflow: visible; border-left: 0; }
}
.ph-cal__head { display: flex; align-items: center; gap: 4px; min-height: var(--hd-size-header); padding: 6px 8px; background: #EF5500; color: #fff; }
.ph-month { flex: 1; margin: 0 4px; font: var(--hd-font-weight-light) var(--hd-font-size-xlarge)/1.2 var(--hd-font-family-light); }
.ph-cal__bar { display: flex; justify-content: center; padding: 10px 12px 4px; }
.ph-cal__note { margin: 8px 12px; }
/* the month: N9 Calendar figures in light type on the page, today on an accent squircle, events as small chips */
.ph-grid { padding: 4px 8px 12px; }
.ph-dow, .ph-week { display: grid; grid-template-columns: repeat(7, 1fr); }
.ph-dow span { padding: 8px 0 6px; text-align: center; font: 700 var(--hd-font-size-xsmall)/1 var(--hd-font-family-base); color: var(--hd-c-fg-secondary); }
.ph-cell { min-height: 52px; min-width: 0; display: flex; flex-direction: column; gap: 2px; align-items: center; padding: 4px 2px; background: none; border: 0; border-top: 1px solid var(--hd-c-divider);
  font: inherit; color: var(--hd-c-fg); cursor: pointer; }
.ph-cell:hover { background: var(--hd-c-pressed); }
@container (min-width: 600px) { .ph-cell { min-height: 84px; align-items: stretch; } .ph-cell .ph-num { align-self: flex-start; } }
.ph-cell.is-out { color: var(--hd-c-fg-tertiary, var(--hd-c-fg-secondary)); }
.ph-num { width: 32px; height: 32px; display: grid; place-items: center; font: 300 1.125rem/1 var(--hd-font-family-light); }
.ph-cell.is-sel .ph-num { box-shadow: inset 0 0 0 2px var(--hd-accent); border-radius: 8px; }
.ph-cell.is-today .ph-num { background: linear-gradient(color-mix(in srgb, var(--hd-accent) 82%, #fff), var(--hd-accent)); color: #fff; box-shadow: none;
  -webkit-mask: var(--hd-squircle-mask) center / 100% 100% no-repeat; mask: var(--hd-squircle-mask) center / 100% 100% no-repeat; }
.ph-chip { font: 400 var(--hd-font-size-xsmall)/1.3 var(--hd-font-family-base); background: color-mix(in srgb, #EF5500 16%, transparent); color: var(--hd-c-fg);
  border-left: 2px solid #EF5500; border-radius: 2px; padding: 0 5px; text-align: left; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ph-chip-dot { width: 6px; height: 6px; padding: 0; background: #EF5500; box-shadow: none; display: inline-block; -webkit-mask: var(--hd-squircle-mask) center / 100% 100% no-repeat; mask: var(--hd-squircle-mask) center / 100% 100% no-repeat; }
.ph-more { font-size: var(--hd-font-size-xsmall); color: var(--hd-c-fg-secondary); }
/* the day: an N9 section header, then the events with their colour bar */
.ph-day { margin-bottom: 8px; }
.ph-day-head { display: flex; align-items: center; gap: 10px; margin: 0; padding: 10px 16px 6px; font: 700 var(--hd-font-size-xsmall)/1.2 var(--hd-font-family-base); color: var(--hd-c-fg-secondary); }
.ph-day-head::before { content: ''; flex: 1; height: 1px; background: var(--hd-c-divider); order: -1; }
.ph-day-head.is-today { color: var(--hd-c-accent-text, var(--hd-accent)); }
.ph-day > .ph-muted { margin: 4px 16px; }
.ph-list { list-style: none; padding: 0; margin: 0; }
.ph-row { width: 100%; min-height: 52px; display: flex; align-items: center; gap: 12px; padding: 6px 16px; background: none; border: 0; text-align: left; font: inherit; color: var(--hd-c-fg); cursor: pointer; }
.ph-row:hover { background: var(--hd-c-pressed); }
.ph-row::before { content: ''; align-self: stretch; width: 4px; flex: none; background: #EF5500; }
.ph-when { flex: none; width: 64px; order: 2; text-align: right; }
.ph-row-main { flex: 1; display: flex; flex-direction: column; min-width: 0; }
.ph-row-title { font: 700 1rem/1.25 var(--hd-font-family-base); overflow-wrap: anywhere; }
.ph-dialog { width: min(94vw, 480px); }
.ph-dialog h2 { margin-bottom: var(--ph-s2); overflow-wrap: anywhere; }
.ph-form { display: flex; flex-direction: column; gap: var(--ph-s3); }
.ph-two { display: grid; grid-template-columns: 1fr 1fr; gap: var(--ph-s3); }
.ph-note-text { white-space: pre-wrap; overflow-wrap: anywhere; }
.ph-error { display: flex; gap: var(--ph-s2); color: var(--ph-negative); margin: 0; }
</style>
