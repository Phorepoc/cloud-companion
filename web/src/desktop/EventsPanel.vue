<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- The desktop's Events panel (the Etesian desktop's Super+N view, after the N9 Events screen and Plasma's Events): the date (a click
     unfolds the month, days with entries dotted; a click on a day picks it), the time with the weather (a click picks the place), the
     picked day's calendar (today and tomorrow by default), unread mail, the latest chats and the newest posts of the feeds, in one dark panel at the right edge under the menu bar. Each row opens its section
     as a window. Everything is read when the panel opens; a part whose service is not set up says so in one muted line. -->
<template>
  <aside class="ph-events" :class="{ 'is-embedded': embedded }" :role="embedded ? 'region' : 'dialog'" aria-label="Events" data-test="events" @keydown.esc="$emit('close')">
    <header class="ph-events__head">
      <button type="button" class="ph-events__day" :aria-expanded="monthOpen" data-test="events-day" @click="monthOpen = !monthOpen">{{ dayText }}
        <q-icon :name="monthOpen ? 'sym_o_expand_less' : 'sym_o_expand_more'" size="22px" aria-hidden="true" /></button>
      <q-btn flat round dense color="white" icon="sym_o_refresh" aria-label="Refresh" title="Refresh" :loading="loading" @click="load" />
    </header>
    <div v-if="monthOpen" class="ph-events__month" data-test="events-month">
      <div class="ph-events__mhead">
        <q-btn flat round dense color="white" icon="sym_o_chevron_left" aria-label="Previous month" @click="shiftMonth(-1)" />
        <b>{{ monthTitle }}</b>
        <q-btn flat round dense color="white" icon="sym_o_chevron_right" aria-label="Next month" @click="shiftMonth(1)" />
      </div>
      <div class="ph-events__grid" role="grid">
        <span v-for="(d, i) in WEEKDAYS" :key="'w' + i" class="ph-events__wd">{{ d }}</span>
        <button v-for="d in monthDays" :key="d.key" type="button" class="ph-events__mday" data-test="events-mday"
                :class="{ 'is-out': !d.inMonth, 'is-today': d.today, 'is-picked': d.picked }" :aria-pressed="d.picked" @click="pick(d.date)">
          {{ d.n }}<i v-if="d.events" class="ph-events__dot" data-test="events-dot" /></button>
      </div>
    </div>
    <button type="button" class="ph-events__now" data-test="events-weather" @click="pickingPlace = true">
      <span class="ph-events__time">{{ timeLabel(now) }}</span>
      <template v-if="forecast"><q-icon :name="weatherIcon(forecast.code, forecast.day)" size="22px" aria-hidden="true" />
        <span>{{ forecast.temperature }}{{ forecast.unit }} · {{ forecast.place.name }}</span></template>
      <span v-else class="ph-events__meta">{{ noPlace ? 'Pick a place for the weather' : weatherError ? 'No weather' : '' }}</span>
    </button>
    <WeatherPlaceDialog v-model="pickingPlace" />

    <section aria-labelledby="ev-cal">
      <h2 id="ev-cal" class="ph-events__h">Calendar</h2>
      <p v-if="cal.error" class="ph-events__none">{{ cal.error }}</p>
      <template v-else>
        <template v-for="d in calDays" :key="d.label">
          <h3 class="ph-events__sub">{{ d.label }}</h3>
          <p v-if="!d.items.length" class="ph-events__none">Nothing planned.</p>
          <button v-for="e in d.items" :key="e.href" type="button" class="ph-events__row" data-test="events-cal" @click="go('calendar')">
            <SectionTile :section="byId.calendar" :size="32" />
            <span class="ph-events__main"><span class="ph-events__title">{{ e.title || '(no title)' }}</span>
              <span class="ph-events__meta">{{ e.allday ? 'All day' : timeLabel(toDate(e.start)) }}{{ e.location ? ' · ' + e.location : '' }}</span></span>
          </button>
        </template>
      </template>
    </section>

    <section aria-labelledby="ev-mail">
      <h2 id="ev-mail" class="ph-events__h">Mail<span v-if="mail.unread" class="ph-events__count" data-test="events-mail-count">{{ mail.unread }} unread</span></h2>
      <p v-if="mail.error" class="ph-events__none">{{ mail.error }}</p>
      <p v-else-if="!mail.items.length" class="ph-events__none">No unread mail.</p>
      <button v-for="m in mail.items" :key="m.id" type="button" class="ph-events__row" data-test="events-mail" @click="go('mail')">
        <SectionTile :section="byId.mail" :size="32" />
        <span class="ph-events__main"><span class="ph-events__title">{{ m.from || '(no sender)' }}</span>
          <span class="ph-events__meta">{{ m.subject || '(no subject)' }}</span></span>
      </button>
    </section>

    <section aria-labelledby="ev-feeds">
      <h2 id="ev-feeds" class="ph-events__h">Feeds
        <q-btn v-if="state.admin" flat dense no-caps size="sm" color="white" :label="editingFeeds ? 'Done' : 'Edit'" data-test="events-feeds-edit" @click="editingFeeds = !editingFeeds" /></h2>
      <p v-if="feedsError" class="ph-events__none">Could not load: {{ feedsError }}</p>
      <p v-else-if="!feedList.length" class="ph-events__none">{{ state.admin ? 'Paste a feed address below to add one.' : 'No feeds yet. The owner adds them here.' }}</p>
      <button v-for="p in posts.slice(0, 6)" :key="p.link + p.title" type="button" class="ph-events__row" data-test="events-feed" @click="openPost(p)">
        <span class="ph-events__rss" aria-hidden="true"><q-icon name="sym_o_rss_feed" size="20px" /></span>
        <span class="ph-events__main"><span class="ph-events__title">{{ p.title || '(no title)' }}</span>
          <span class="ph-events__meta">{{ p.feed }}</span></span>
        <span v-if="postTs(p)" class="ph-events__when">{{ ago(postTs(p)) }}</span>
      </button>
      <template v-if="state.admin && (editingFeeds || !feedList.length)">
        <div v-for="f in feedList" :key="f.url" class="ph-events__feed">
          <span class="ph-events__main"><span class="ph-events__title">{{ f.title || f.url }}</span>
            <span class="ph-events__meta">{{ feedErrors[f.url] ? 'Not read: ' + feedErrors[f.url] : f.url }}</span></span>
          <q-btn flat round dense color="white" icon="sym_o_close" :aria-label="'Remove ' + (f.title || f.url)" data-test="events-feed-remove" @click="removeFeed(f.url).catch(fail)" />
        </div>
        <form class="ph-events__add" @submit.prevent="add">
          <q-input v-model="newFeed" dense dark outlined placeholder="https://… (RSS or Atom)" aria-label="Feed address" data-test="events-feed-url" class="col" />
          <q-btn type="submit" unelevated color="primary" label="Add" :loading="adding" data-test="events-feed-add" />
        </form>
        <p v-if="feedFail" class="ph-events__none" role="alert" data-test="events-feed-error">{{ feedFail }}</p>
      </template>
    </section>

    <section aria-labelledby="ev-chats">
      <h2 id="ev-chats" class="ph-events__h">Chats</h2>
      <p v-if="chats.error" class="ph-events__none">{{ chats.error }}</p>
      <p v-else-if="!chats.items.length" class="ph-events__none">No messages yet.</p>
      <button v-for="c in chats.items" :key="c.key" type="button" class="ph-events__row" data-test="events-chat" @click="go('chats')">
        <SectionTile :section="byId.chats" :size="32" />
        <span class="ph-events__main"><span class="ph-events__title">{{ c.name || c.number }}</span>
          <span class="ph-events__meta">{{ c.last_dir === 'out' ? 'You: ' : '' }}{{ c.last }}</span></span>
        <span class="ph-events__when">{{ ago(c.last_ts) }}</span>
      </button>
    </section>
  </aside>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import type { CalendarEvent, MailItem, SmsConversation } from '../proto'
import { sections } from '../nav'
import { addDays, toDate, timeLabel, dayLabel, ymd } from '../dates'
import { quiet, loadEvents, eventsOn, loadUnread, loadChats, ago as agoAt } from './glance'
import SectionTile from '../components/SectionTile.vue'
import WeatherPlaceDialog from './WeatherPlaceDialog.vue'
import { forecast, noPlace, weatherError, loadWeather, weatherIcon } from './weather'
import { feedList, posts, feedErrors, feedsError, loadFeeds, addFeed, removeFeed, openPost, postTs } from './feeds'
import { state } from '../cloud'

const props = defineProps({ now: { type: Date, required: true }, /** inside the Overview: fills its column, no shadow */ embedded: { type: Boolean, default: false } })
const emit = defineEmits(['open', 'close', 'unread'])
const byId = Object.fromEntries(sections.map((s) => [s.id, s]))

const loading = ref(false)
const cal = reactive({ items: [] as CalendarEvent[], error: '' })
const mail = reactive({ items: [] as MailItem[], unread: 0, error: '' })
const chats = reactive({ items: [] as SmsConversation[], error: '' })

const dayText = computed(() => dayLabel(props.now))
const pickingPlace = ref(false)

// the month, unfolded from the date: six weeks from the Monday on or before the 1st, days with entries dotted
const WEEKDAYS = ['M', 'T', 'W', 'T', 'F', 'S', 'S']
const monthOpen = ref(false)
const shown = ref(new Date(props.now.getFullYear(), props.now.getMonth(), 1))
const picked = ref<Date | null>(null)
const monthEvents = ref<CalendarEvent[]>([])
const gridStart = computed(() => new Date(shown.value.getFullYear(), shown.value.getMonth(), 1 - ((shown.value.getDay() + 6) % 7)))
const monthTitle = computed(() => shown.value.toLocaleDateString(undefined, { month: 'long', year: 'numeric' }))
const monthDays = computed(() => {
  const busy = new Set(monthEvents.value.map((e) => ymd(toDate(e.start))))
  return Array.from({ length: 42 }, (_, i) => {
    const d = new Date(gridStart.value.getFullYear(), gridStart.value.getMonth(), gridStart.value.getDate() + i)
    return { key: ymd(d), date: d, n: d.getDate(), inMonth: d.getMonth() === shown.value.getMonth(), today: ymd(d) === ymd(props.now),
      picked: !!picked.value && ymd(d) === ymd(picked.value), events: busy.has(ymd(d)) }
  })
})
async function loadMonth() {
  try { monthEvents.value = await loadEvents(gridStart.value, 42) } catch { monthEvents.value = [] }
}
function shiftMonth(n: number) { shown.value = new Date(shown.value.getFullYear(), shown.value.getMonth() + n, 1); loadMonth() }
async function pick(d: Date) {
  picked.value = ymd(d) === ymd(props.now) ? null : d
  try { cal.items = await loadEvents(picked.value || props.now); cal.error = '' } catch (e) { cal.error = quiet(e as Error, 'The calendar') }
}
watch(monthOpen, (o) => { if (o) loadMonth() })

// feeds: the owner adds and removes them here (Plasma: paste an address to add one)
const editingFeeds = ref(false), newFeed = ref(''), adding = ref(false), feedFail = ref('')
const fail = (e: Error) => { feedFail.value = e.message }
async function add() {
  if (!newFeed.value.trim()) return
  adding.value = true; feedFail.value = ''
  try { await addFeed(newFeed.value); newFeed.value = '' } catch (e) { fail(e as Error) } finally { adding.value = false }
}
const calDays = computed(() => {
  if (picked.value && ymd(picked.value) !== ymd(props.now)) return [{ label: dayLabel(picked.value), items: eventsOn(cal.items, picked.value) }]
  return [{ label: 'Today', items: eventsOn(cal.items, props.now) }, { label: 'Tomorrow', items: eventsOn(cal.items, addDays(props.now, 1)) }]
})
const ago = (ts: number) => agoAt(props.now, ts)

async function load() {
  loading.value = true
  await Promise.all([
    loadEvents(props.now).then((ev) => { cal.items = ev; cal.error = '' }, (e) => { cal.error = quiet(e, 'The calendar') }),
    loadUnread().then((m) => { mail.unread = m.unread; mail.items = m.items; mail.error = '' }, (e) => { mail.error = quiet(e, 'Mail'); mail.unread = 0; mail.items = [] }),
    loadChats().then((c) => { chats.items = c; chats.error = '' }, (e) => { chats.error = quiet(e, 'Chats') }),
    loadWeather(), loadFeeds(),
  ])
  emit('unread', mail.unread)
  loading.value = false
}
function go(id: string) { emit('open', id) }
onMounted(load)
</script>

<style scoped>
/* the N9 Events screen as a desktop panel: dark overlay surface, light big date, rows with lit tiles */
.ph-events.is-embedded { inset: 0; width: auto; z-index: auto; background: none; box-shadow: none; }
.ph-events {
  position: absolute; z-index: 9400; top: 0; right: 0; bottom: 0; width: 380px; overflow-y: auto; padding: var(--ph-s4);
  background: #141414; color: #fff; box-shadow: var(--hd-shadow-overlay), -1px 0 0 rgba(255, 255, 255, 0.1);
  font-family: var(--ph-font);
}
.ph-events__head { display: flex; align-items: center; justify-content: space-between; margin-bottom: var(--ph-s2); }
.ph-events__day { display: inline-flex; align-items: center; gap: 4px; margin: 0; padding: 0; border: 0; background: none; color: inherit; cursor: pointer;
  font: var(--hd-font-weight-light) var(--ph-text-title)/1.2 var(--ph-font-light); }
.ph-events__now { display: flex; align-items: center; gap: var(--ph-s2); width: 100%; padding: var(--ph-s1) 0; border: 0; background: none; color: #fff; font: inherit; cursor: pointer; text-align: left; }
.ph-events__time { font: var(--hd-font-weight-light) 28px/1 var(--ph-font-light); margin-right: var(--ph-s2); }
.ph-events__month { margin: var(--ph-s2) 0; }
.ph-events__mhead { display: flex; align-items: center; justify-content: space-between; }
.ph-events__grid { display: grid; grid-template-columns: repeat(7, 1fr); gap: 2px; text-align: center; }
.ph-events__wd { font-size: var(--hd-font-size-xsmall); color: var(--ph-chrome-fg-muted); padding: 4px 0; }
.ph-events__mday { position: relative; height: 36px; border: 0; background: none; color: #fff; font: inherit; cursor: pointer; }
.ph-events__mday.is-out { color: rgba(255, 255, 255, 0.35); }
.ph-events__mday.is-today { font-weight: 700; color: #4591FF; }
.ph-events__mday.is-picked, .ph-events__mday:hover { background: linear-gradient(#6AA8FF, #2E72E0); color: #fff;
  -webkit-mask: var(--ph-squircle) center / contain no-repeat; mask: var(--ph-squircle) center / contain no-repeat; }
.ph-events__dot { position: absolute; left: 50%; bottom: 3px; width: 4px; height: 4px; margin-left: -2px; border-radius: 50%; background: #4591FF; }
.ph-events__rss { display: grid; place-items: center; width: 32px; height: 32px; color: #fff; background: linear-gradient(#F7A13A, #E5620F);
  -webkit-mask: var(--ph-squircle) center / contain no-repeat; mask: var(--ph-squircle) center / contain no-repeat; }
.ph-events__feed { display: flex; align-items: center; gap: var(--ph-s2); padding: var(--ph-s1) var(--ph-s2); }
.ph-events__add { display: flex; gap: var(--ph-s2); margin-top: var(--ph-s2); }
.ph-events__h {
  display: flex; align-items: baseline; justify-content: space-between; margin: var(--ph-s4) 0 var(--ph-s1); padding-bottom: var(--ph-s1);
  font: var(--hd-font-weight-bold) var(--hd-font-size-small)/1.2 var(--ph-font); color: var(--ph-chrome-fg-muted);
  border-bottom: 1px solid rgba(255, 255, 255, 0.16);
}
.ph-events__count { font-weight: 400; color: #fff; }
.ph-events__sub { margin: var(--ph-s2) 0 0; font: 400 var(--hd-font-size-xsmall)/1.2 var(--ph-font); color: var(--ph-chrome-fg-muted); }
.ph-events__none { margin: var(--ph-s1) 0; font-size: var(--hd-font-size-small); color: var(--ph-chrome-fg-muted); }
.ph-events__row {
  display: flex; align-items: center; gap: var(--ph-s3); width: 100%; padding: var(--ph-s2); border: 0; border-radius: var(--ph-r-card);
  background: none; color: inherit; font: inherit; text-align: left; cursor: pointer;
}
.ph-events__row:hover, .ph-events__row:focus-visible { background: rgba(255, 255, 255, 0.1); }
.ph-events__main { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.ph-events__title, .ph-events__meta { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.ph-events__title { font-size: var(--hd-font-size-default); }
.ph-events__meta, .ph-events__when { font-size: var(--hd-font-size-small); color: var(--ph-chrome-fg-muted); }
</style>
