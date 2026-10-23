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

    <q-banner v-if="error" class="ph-note is-error q-mb-md" rounded role="alert">
      <template #avatar><q-icon name="sym_o_error" aria-hidden="true" /></template>
      {{ error }}
      <template #action><q-btn flat label="Try again" @click="load" /></template>
    </q-banner>
    <EmptyState v-if="loading && !events.length" state="loading" title="Loading events" text="This takes a moment." />
    <EmptyState v-else-if="!loading && !events.length && !error" icon="sym_o_calendar_month" title="Nothing on this month"
                text="Add an event. It will show on every device that syncs with your hub.">
      <template #action><q-btn outline color="primary" label="New event" @click="openNew()" /></template>
    </EmptyState>

    <section v-for="g in groups" :key="g.key" class="ph-day" :aria-label="g.label">
      <h3 class="ph-day-head" :class="{ 'is-today': g.today }">{{ g.label }}<span v-if="g.today" class="ph-small"> (today)</span></h3>
      <ul class="ph-card ph-list">
        <li v-for="e in g.items" :key="e.href">
          <button type="button" class="ph-row" :data-test="'event-row'" @click="openView(e)">
            <span class="ph-when ph-small ph-muted">{{ whenLabel(e) }}</span>
            <span class="ph-row-main">
              <span class="ph-row-title">{{ e.title }}</span>
              <span v-if="e.location" class="ph-small ph-muted">{{ e.location }}</span>
            </span>
            <q-badge v-if="e.recurs" outline color="primary" label="Repeats" />
          </button>
        </li>
      </ul>
    </section>

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

<script setup>
import { ref, computed, onMounted } from 'vue'
import { hubCall } from '../cloud.js'
import { toast } from '../toast.js'
import { ymd, toDate, addDays, toFields, fromFields, monthLabel, dayLabel, timeLabel } from '../dates.js'
import EmptyState from '../components/EmptyState.vue'

const now = new Date()
const month = ref(new Date(now.getFullYear(), now.getMonth(), 1))
const events = ref([])
const loading = ref(false)
const error = ref('')
const current = ref(null)
const viewOpen = ref(false)
const editOpen = ref(false)
const saving = ref(false)
const formError = ref('')
const form = ref({})

async function load() {
  loading.value = true
  error.value = ''
  try {
    const from = ymd(month.value)
    const to = ymd(new Date(month.value.getFullYear(), month.value.getMonth() + 1, 1))
    events.value = (await hubCall({ op: 'events.list', from, to })).events || []
  } catch (e) {
    error.value = /not set up/i.test(e.message) ? 'The hub is not set up on this server, so there are no events to show. See the hub guide to set it up.' : e.message
  } finally {
    loading.value = false
  }
}
const shift = (n) => { month.value = new Date(month.value.getFullYear(), month.value.getMonth() + n, 1); load() }
const today = () => { const d = new Date(); month.value = new Date(d.getFullYear(), d.getMonth(), 1); load() }

// the day an event is listed under: its start day, clamped into the month shown
const startOf = (e) => toDate(e.start)
const groups = computed(() => {
  const m0 = month.value
  const m1 = new Date(m0.getFullYear(), m0.getMonth() + 1, 1)
  const byDay = new Map()
  for (const e of [...events.value].sort((a, b) => startOf(a) - startOf(b))) {
    let d = startOf(e)
    if (d < m0) d = m0
    if (d >= m1) continue
    const key = ymd(d)
    if (!byDay.has(key)) byDay.set(key, { key, label: dayLabel(d), today: key === ymd(new Date()), items: [] })
    byDay.get(key).items.push(e)
  }
  return [...byDay.values()]
})
const whenLabel = (e) => (e.allday ? 'All day' : timeLabel(startOf(e)))
function fullWhen(e) {
  const s = startOf(e)
  if (e.allday) { const last = addDays(toDate(e.end || e.start), -1); return last > s ? `${dayLabel(s)} to ${dayLabel(last)}` : dayLabel(s) }
  const en = toDate(e.end || e.start)
  return `${dayLabel(s)}, ${timeLabel(s)} to ${ymd(en) === ymd(s) ? timeLabel(en) : dayLabel(en) + ', ' + timeLabel(en)}`
}

const openView = (e) => { current.value = e; viewOpen.value = true }
function openNew(ev) {
  viewOpen.value = false
  formError.value = ''
  if (ev) form.value = { href: ev.href, etag: ev.etag, title: ev.title, location: ev.location || '', notes: ev.notes || '', ...toFields(ev) }
  else {
    const d = ymd(month.value.getMonth() === now.getMonth() && month.value.getFullYear() === now.getFullYear() ? now : month.value)
    form.value = { href: '', etag: '', title: '', location: '', notes: '', allday: false, startDate: d, startTime: '09:00', endDate: d, endTime: '10:00', utc: false }
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
    await hubCall({ op: 'events.save', event: { href: f.href || undefined, etag: f.etag || undefined, title: f.title.trim(), start, end, allday: f.allday, location: f.location, notes: f.notes } })
    editOpen.value = false
    toast(f.href ? 'Event saved' : 'Event added')
    await load()
  } catch (e) {
    formError.value = e.message
  } finally {
    saving.value = false
  }
}

function remove(e) {
  viewOpen.value = false
  events.value = events.value.filter((x) => x.href !== e.href)
  toast('Event deleted', {
    undo: () => { events.value = [...events.value, e] },
    onExpire: async () => {
      try { await hubCall({ op: 'events.delete', href: e.href, etag: e.etag }) } catch (err) { error.value = err.message; await load() }
    },
  })
}

onMounted(load)
</script>

<style scoped>
.ph-head { display: flex; align-items: center; justify-content: space-between; gap: var(--ph-s3); margin-bottom: var(--ph-s3); }
.ph-months { display: flex; align-items: center; gap: var(--ph-s1); margin-bottom: var(--ph-s4); }
.ph-month { flex: 1; text-align: center; font-size: var(--ph-text-title-s); }
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
