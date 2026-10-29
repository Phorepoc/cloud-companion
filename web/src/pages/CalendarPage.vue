<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="ph-page">
    <div class="ph-head">
      <h1>Calendar</h1>
      <q-btn unelevated color="primary" icon="sym_o_add" label="New event" data-test="event-new" @click="openNew()" />
    </div>
    <div class="ph-months" role="group" aria-label="Month">
      <q-btn flat round icon="sym_o_chevron_left" aria-label="Previous month" data-test="cal-prev" @click="shift(-1)" />
      <h2 class="ph-month" aria-live="polite" data-test="cal-month">{{ monthLabel(month) }}</h2>
      <q-btn flat round icon="sym_o_chevron_right" aria-label="Next month" data-test="cal-next" @click="shift(1)" />
      <q-btn flat label="Today" data-test="cal-today" @click="today" />
    </div>
    <q-btn-toggle v-model="view" no-caps unelevated spread toggle-color="primary" toggle-text-color="on-accent" color="surface-2" text-color="ph" class="q-mb-md"
                  :options="[{ label: 'Month', value: 'month', icon: 'sym_o_calendar_view_month' }, { label: 'Agenda', value: 'agenda', icon: 'sym_o_view_agenda' }]"
                  data-test="cal-view" />

    <q-banner v-if="error" class="ph-note is-error q-mb-md" rounded role="alert">
      <template #avatar><q-icon name="sym_o_error" aria-hidden="true" /></template>
      {{ error }}
      <template #action><q-btn flat label="Try again" @click="load" /></template>
    </q-banner>

    <!-- month grid -->
    <div v-if="view === 'month'" class="ph-grid" role="grid" :aria-label="monthLabel(month)" data-test="cal-grid">
      <div class="ph-dow" role="row"><span v-for="d in dows" :key="d" role="columnheader">{{ d }}</span></div>
      <div v-for="(week, wi) in weeks" :key="wi" class="ph-week" role="row">
        <button v-for="d in week" :key="ymd(d)" type="button" role="gridcell" class="ph-cell"
                :class="{ 'is-out': d.getMonth() !== month.getMonth(), 'is-today': ymd(d) === ymd(new Date()), 'is-sel': ymd(d) === selected }"
                :aria-label="cellLabel(d)" :aria-selected="ymd(d) === selected" :data-test="'cal-day-' + ymd(d)" @click="selected = ymd(d)">
          <span class="ph-num">{{ d.getDate() }}</span>
          <span v-for="e in (byDay[ymd(d)] || []).slice(0, 2)" :key="e.href" class="ph-chip" :class="{ 'ph-chip-dot': phone }">{{ phone ? '' : e.title }}</span>
          <span v-if="(byDay[ymd(d)] || []).length > 2" class="ph-more">+{{ byDay[ymd(d)].length - 2 }}</span>
        </button>
      </div>
    </div>
    <section v-if="view === 'month'" class="ph-day" :aria-label="dayLabel(toDate(selected))" data-test="cal-selected">
      <h3 class="ph-day-head" :class="{ 'is-today': selected === ymd(new Date()) }">{{ dayLabel(toDate(selected)) }}</h3>
      <p v-if="!(byDay[selected] || []).length" class="ph-muted">Nothing planned.</p>
      <ul v-else class="ph-card ph-list">
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
    <template v-if="view === 'agenda'">
      <EmptyState v-if="loading && !events.length" state="loading" title="Loading events" text="This takes a moment." />
      <EmptyState v-else-if="!loading && !events.length && !error" icon="sym_o_calendar_month" title="Nothing on this month"
                  text="Add an event. It will show on every device that syncs with your hub.">
        <template #action><q-btn outline color="primary" label="New event" @click="openNew()" /></template>
      </EmptyState>
      <section v-for="g in groups" :key="g.key" class="ph-day" :aria-label="g.label">
        <h3 class="ph-day-head" :class="{ 'is-today': g.today }">{{ g.label }}<span v-if="g.today" class="ph-small"> (today)</span></h3>
        <ul class="ph-card ph-list">
          <li v-for="e in g.items" :key="e.href">
            <button type="button" class="ph-row" data-test="event-row" @click="openView(e)">
              <span class="ph-when ph-small ph-muted">{{ whenLabel(e) }}</span>
              <span class="ph-row-main"><span class="ph-row-title">{{ e.title }}</span><span v-if="e.location" class="ph-small ph-muted">{{ e.location }}</span></span>
              <q-badge v-if="e.recurs" outline color="primary" label="Repeats" />
            </button>
          </li>
        </ul>
      </section>
    </template>

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

    <q-dialog v-model="editOpen" persistent>
      <q-card class="ph-dialog" data-test="event-form">
        <form @submit.prevent="save">
          <q-card-section class="ph-form">
            <h2>{{ form.href ? 'Edit event' : 'New event' }}</h2>
            <q-input v-model="form.title" label="Title" outlined autofocus data-test="event-title" />
            <q-toggle v-model="form.allday" label="All day" data-test="event-allday" />
            <div class="ph-two">
              <q-input v-model="form.startDate" label="Starts" type="date" outlined data-test="event-start-date" />
              <q-input v-if="!form.allday" v-model="form.startTime" label="Time" type="time" outlined data-test="event-start-time" />
            </div>
            <div class="ph-two">
              <q-input v-model="form.endDate" label="Ends" type="date" outlined data-test="event-end-date" />
              <q-input v-if="!form.allday" v-model="form.endTime" label="Time" type="time" outlined data-test="event-end-time" />
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
import { hubCall } from '../cloud'
import { toast } from '../toast'
import { ymd, toDate, addDays, toFields, fromFields, monthLabel, dayLabel, timeLabel, gridDays, browserZone, type EventFields } from '../dates'
import type { CalendarEvent } from '../proto'

/** The edit form: the event's text fields and its start and end as date and time inputs. */
interface EventForm extends EventFields { href?: string; etag?: string; title: string; location: string; notes: string }
import EmptyState from '../components/EmptyState.vue'

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
const selected = ref(ymd(now))
const dows = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']
try { view.value = localStorage.getItem('phecloud-cal-view') || 'month' } catch (e) { /* ignore */ }
watch(view, (v) => { try { localStorage.setItem('phecloud-cal-view', v) } catch (e) { /* ignore */ } })

async function load() {
  loading.value = true
  error.value = ''
  try {
    // the grid shows days of the neighbouring months too: ask for the whole grid
    const days = gridDays(month.value)
    events.value = (await hubCall({ op: 'events.list', from: ymd(days[0]), to: ymd(addDays(days[41], 1)) })).events || []
  } catch (e) {
    error.value = /not set up/i.test((e as Error).message) ? 'The hub is not set up on this server, so there are no events to show. See the hub guide to set it up.' : (e as Error).message
  } finally {
    loading.value = false
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
const byDay = computed(() => {
  const m: Record<string, CalendarEvent[]> = {}
  for (const e of [...events.value].sort((a, b) => startOf(a).getTime() - startOf(b).getTime())) for (const k of daysOf(e)) (m[k] = m[k] || []).push(e)
  return m
})
const weeks = computed(() => { const d = gridDays(month.value); return [0, 1, 2, 3, 4, 5].map((i) => d.slice(i * 7, i * 7 + 7)) })
const cellLabel = (d: Date) => `${dayLabel(d)}, ${(byDay.value[ymd(d)] || []).length} events`

// agenda: events of the shown month, by start day
const groups = computed(() => {
  const m0 = month.value
  const m1 = new Date(m0.getFullYear(), m0.getMonth() + 1, 1)
  const byStart = new Map()
  for (const e of [...events.value].sort((a, b) => startOf(a).getTime() - startOf(b).getTime())) {
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
  } finally {
    saving.value = false
  }
}

function remove(e: CalendarEvent) {
  viewOpen.value = false
  events.value = events.value.filter((x) => x.href !== e.href)
  toast('Event deleted', {
    undo: () => { events.value = [...events.value, e] },
    onExpire: async () => {
      try { await hubCall({ op: 'events.delete', href: e.href, etag: e.etag }) } catch (err) { error.value = (err as Error).message; await load() }
    },
  })
}

onMounted(load)
</script>

<style scoped>
.ph-head { display: flex; align-items: center; justify-content: space-between; gap: var(--ph-s3); margin-bottom: var(--ph-s3); }
.ph-months { display: flex; align-items: center; gap: var(--ph-s1); margin-bottom: var(--ph-s3); }
.ph-month { flex: 1; text-align: center; font-size: var(--ph-text-title-s); }
.ph-grid { background: var(--ph-surface); border: 1px solid var(--ph-divider); border-radius: var(--ph-r-card); overflow: hidden; margin-bottom: var(--ph-s4); }
.ph-dow, .ph-week { display: grid; grid-template-columns: repeat(7, 1fr); }
.ph-dow span { padding: var(--ph-s2) 0; text-align: center; font-size: var(--ph-text-caption); color: var(--ph-text-muted); font-weight: 600; border-bottom: 1px solid var(--ph-divider); }
.ph-cell { min-height: 64px; min-width: 0; display: flex; flex-direction: column; gap: 2px; align-items: stretch; padding: var(--ph-s1); background: none; border: 0; border-right: 1px solid var(--ph-divider); border-bottom: 1px solid var(--ph-divider); font: inherit; color: var(--ph-text); cursor: pointer; text-align: left; }
.ph-week .ph-cell:last-child { border-right: 0; }
@media (min-width: 600px) { .ph-cell { min-height: 92px; } }
.ph-cell.is-out { color: var(--ph-text-muted); background: var(--ph-bg); }
.ph-cell.is-sel { outline: 2px solid var(--ph-accent); outline-offset: -2px; }
.ph-num { font-size: var(--ph-text-small); font-weight: 600; width: 24px; height: 24px; display: grid; place-items: center; border-radius: 50%; }
.ph-cell.is-today .ph-num { background: var(--ph-accent); color: var(--ph-on-accent); }
.ph-chip { font-size: var(--ph-text-caption); line-height: 1.3; background: var(--ph-accent-soft); color: var(--ph-accent); border-radius: 4px; padding: 0 4px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ph-chip-dot { width: 8px; height: 8px; padding: 0; border-radius: 50%; background: var(--ph-accent); display: inline-block; }
.ph-more { font-size: var(--ph-text-caption); color: var(--ph-text-muted); }
.ph-day { margin-bottom: var(--ph-s4); }
.ph-day-head { font-size: var(--ph-text-small); color: var(--ph-text-muted); font-weight: 600; margin: 0 0 var(--ph-s2) var(--ph-s1); }
.ph-day-head.is-today { color: var(--ph-accent); }
.ph-list { list-style: none; padding: 0; margin: 0; }
.ph-list li { border-bottom: 1px solid var(--ph-divider); }
.ph-list li:last-child { border-bottom: 0; }
.ph-row { width: 100%; min-height: 56px; display: flex; align-items: center; gap: var(--ph-s3); padding: var(--ph-s2) var(--ph-s4); background: none; border: 0; text-align: left; font: inherit; color: var(--ph-text); cursor: pointer; }
.ph-row:hover { background: var(--ph-surface-2); }
.ph-when { flex: none; width: 72px; }
.ph-row-main { flex: 1; display: flex; flex-direction: column; min-width: 0; }
.ph-row-title { font-weight: 600; overflow-wrap: anywhere; }
.ph-dialog { width: min(94vw, 480px); }
.ph-dialog h2 { margin-bottom: var(--ph-s2); overflow-wrap: anywhere; }
.ph-form { display: flex; flex-direction: column; gap: var(--ph-s3); }
.ph-two { display: grid; grid-template-columns: 1fr 1fr; gap: var(--ph-s3); }
.ph-note-text { white-space: pre-wrap; overflow-wrap: anywhere; }
.ph-note { background: var(--ph-surface-2); color: var(--ph-text); border: 1px solid var(--ph-divider); }
.ph-note.is-error { border-color: var(--ph-negative); }
.ph-error { display: flex; gap: var(--ph-s2); color: var(--ph-negative); margin: 0; }
</style>
