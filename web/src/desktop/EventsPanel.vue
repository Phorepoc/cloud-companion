<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- The desktop's Events panel (the Etesian desktop's Super+N view, after the N9 Events screen): the date, today's and tomorrow's
     calendar, unread mail and the latest chats, in one dark panel at the right edge under the menu bar. Each row opens its section
     as a window. Everything is read when the panel opens; a part whose service is not set up says so in one muted line. -->
<template>
  <aside class="ph-events" role="dialog" aria-label="Events" data-test="events" @keydown.esc="$emit('close')">
    <header class="ph-events__head">
      <p class="ph-events__day">{{ dayText }}</p>
      <q-btn flat round dense color="white" icon="sym_o_refresh" aria-label="Refresh" title="Refresh" :loading="loading" @click="load" />
    </header>

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
import { computed, onMounted, reactive, ref } from 'vue'
import type { CalendarEvent, MailItem, SmsConversation } from '../proto'
import { sections } from '../nav'
import { addDays, toDate, timeLabel, dayLabel } from '../dates'
import { quiet, loadEvents, eventsOn, loadUnread, loadChats, ago as agoAt } from './glance'
import SectionTile from '../components/SectionTile.vue'

const props = defineProps({ now: { type: Date, required: true } })
const emit = defineEmits(['open', 'close', 'unread'])
const byId = Object.fromEntries(sections.map((s) => [s.id, s]))

const loading = ref(false)
const cal = reactive({ items: [] as CalendarEvent[], error: '' })
const mail = reactive({ items: [] as MailItem[], unread: 0, error: '' })
const chats = reactive({ items: [] as SmsConversation[], error: '' })

const dayText = computed(() => dayLabel(props.now))
const calDays = computed(() => {
  return [{ label: 'Today', items: eventsOn(cal.items, props.now) }, { label: 'Tomorrow', items: eventsOn(cal.items, addDays(props.now, 1)) }]
})
const ago = (ts: number) => agoAt(props.now, ts)

async function load() {
  loading.value = true
  await Promise.all([
    loadEvents(props.now).then((ev) => { cal.items = ev; cal.error = '' }, (e) => { cal.error = quiet(e, 'The calendar') }),
    loadUnread().then((m) => { mail.unread = m.unread; mail.items = m.items; mail.error = '' }, (e) => { mail.error = quiet(e, 'Mail'); mail.unread = 0; mail.items = [] }),
    loadChats().then((c) => { chats.items = c; chats.error = '' }, (e) => { chats.error = quiet(e, 'Chats') }),
  ])
  emit('unread', mail.unread)
  loading.value = false
}
function go(id: string) { emit('open', id) }
onMounted(load)
</script>

<style scoped>
/* the N9 Events screen as a desktop panel: dark overlay surface, light big date, rows with lit tiles */
.ph-events {
  position: absolute; z-index: 9400; top: 0; right: 0; bottom: 0; width: 380px; overflow-y: auto; padding: var(--ph-s4);
  background: #141414; color: #fff; box-shadow: var(--hd-shadow-overlay), -1px 0 0 rgba(255, 255, 255, 0.1);
  font-family: var(--ph-font);
}
.ph-events__head { display: flex; align-items: center; justify-content: space-between; margin-bottom: var(--ph-s2); }
.ph-events__day { margin: 0; font: var(--hd-font-weight-light) var(--ph-text-title)/1.2 var(--ph-font-light); }
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
