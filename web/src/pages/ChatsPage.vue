<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: AGPL-3.0-only -->
<template>
  <q-page class="ph-page">
    <h1 v-if="!chrome.hosted">Chats</h1>
    <p class="ph-muted">Text messages (SMS) from your own phones. Each phone syncs its own messages here; a message you send goes out through the phone you pick.</p>

    <div v-if="error" class="ph-error" role="alert" data-test="chat-error"><q-icon name="sym_o_error" size="20px" aria-hidden="true" /> {{ error }}</div>

    <p v-if="loaded && !devices.length" class="ph-note" role="note" data-test="no-sms-devices">
      <q-icon name="sym_o_info" size="20px" aria-hidden="true" /> No phone is sharing its text messages yet. Pair a phone that has the Cloud Companion app, and its messages will appear here.
    </p>

    <div class="ph-chat" :class="{ 'ph-open': !!current }">
      <section class="ph-card ph-conv" aria-label="Conversations">
        <div class="ph-row">
          <h2>Conversations</h2>
          <q-btn flat dense icon="sym_o_edit_square" label="New" no-caps data-test="new-chat" @click="startNew" />
        </div>
        <p v-if="loaded && !convs.length" class="ph-muted" data-test="no-convs">No messages yet.</p>
        <ul class="ph-list">
          <li v-for="c in convs" :key="c.key">
            <button type="button" class="ph-conv-item" :class="{ 'ph-sel': current && current.key === c.key }" data-test="conv" @click="open(c)">
              <span class="ph-conv-name">{{ c.name || c.number }}</span>
              <span class="ph-muted ph-small ph-conv-last">{{ c.last_dir === 'out' ? 'You: ' : '' }}{{ c.last }}</span>
              <span v-if="c.last_failed" class="ph-error ph-small">Not sent</span>
            </button>
          </li>
        </ul>
      </section>

      <section v-if="current" class="ph-card ph-thread" aria-label="Messages" data-test="thread">
        <div class="ph-row">
          <q-btn flat round dense icon="sym_o_arrow_back" aria-label="Back to the conversations" class="ph-back" @click="current = null" />
          <h2>{{ current.name || current.number || 'New message' }}</h2>
        </div>
        <q-input v-if="!current.key" v-model="newNumber" outlined dense label="To (phone number)" data-test="new-number" />
        <ul ref="list" class="ph-msgs" aria-live="polite">
          <li v-for="m in messages" :key="m.id" :class="m.dir === 'out' ? 'ph-out' : 'ph-in'" data-test="msg">
            <span class="ph-bubble">{{ m.body }}</span>
            <span class="ph-muted ph-small">{{ when(m.ts) }} · {{ deviceName(m.device) }}<template v-if="m.dir === 'out'"> · <span :class="m.status === 'failed' ? 'ph-error' : ''" data-test="msg-status">{{ statusText(m) }}</span></template></span>
          </li>
        </ul>
        <div class="ph-compose">
          <q-select v-model="sendVia" :options="deviceOptions" emit-value map-options outlined dense label="Send from" data-test="send-via" :disable="!deviceOptions.length" />
          <q-input v-model="text" outlined dense autogrow label="Message" hint="Ctrl+Enter to send" data-test="sms-text" @keyup.ctrl.enter="send" />
          <q-btn unelevated color="primary" icon="sym_o_send" label="Send" :disable="!text.trim() || !sendVia" :loading="sending" data-test="sms-send" @click="send" />
        </div>
      </section>
    </div>
  </q-page>
</template>

<script setup lang="ts">
import { ref, computed, nextTick, onMounted, onBeforeUnmount, watch } from 'vue'
import { useChrome } from '@etesian-design/shell'
import { apiCall } from '../cloud'
import { latest } from '../latest'
import type { SmsConversation, SmsDevice, SmsMessage } from '../proto'

// the title is in the host's header (the classic bar, a desktop window)
const chrome = useChrome(() => ({ title: 'Chats' }))

/** The open conversation: a stored one, or a new message (empty key) still to be addressed. */
type Current = Pick<SmsConversation, 'key' | 'number'> & Partial<SmsConversation>

const convs = ref<SmsConversation[]>([])
const devices = ref<SmsDevice[]>([])
const current = ref<Current | null>(null)
const messages = ref<SmsMessage[]>([])
const loaded = ref(false)
const error = ref('')
const text = ref('')
const sendVia = ref('')
const sending = ref(false)
const newNumber = ref('')
let timer: ReturnType<typeof setTimeout> | undefined
let alive = true
const list = ref<HTMLElement | null>(null)
const threadLoad = latest()

const deviceName = (id: string) => devices.value.find((d) => d.device === id)?.name || 'phone'
const deviceOptions = computed(() => devices.value.map((d) => ({ label: d.name || d.device, value: d.device })))
const statusText = (m: SmsMessage) => {
  const text: Partial<Record<SmsMessage['status'], string>> = { queued: 'waiting for the phone', sent: 'sent', delivered: 'delivered', failed: 'not sent' + (m.error ? ' (' + m.error + ')' : '') }
  return text[m.status] || m.status
}
const when = (t: number) => new Date(t * 1000).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })

// what the page has, by fingerprint: the server sends only {same: true} when nothing changed since
let convSeen = '', threadSeen = '', threadKey = ''
async function load() {
  try {
    const j = await apiCall('GET', '/sms/conversations' + (convSeen ? '?seen=' + convSeen : ''))
    convSeen = j.seen || ''
    if (!j.same) { convs.value = j.conversations; devices.value = j.devices }
    if (!sendVia.value && devices.value.length) sendVia.value = devices.value[0].device
    if (current.value && current.value.key) await loadThread()
    error.value = ''
  } catch (e) { error.value = (e as Error).message } finally { loaded.value = true }
}
async function loadThread() {
  const key = current.value!.key
  const t = threadLoad.take()
  const seen = key === threadKey ? threadSeen : ''
  const j = await apiCall('GET', '/sms/thread?key=' + encodeURIComponent(key) + (seen ? '&seen=' + seen : ''))
  if (!threadLoad.is(t) || current.value?.key !== key) return // another conversation opened meanwhile keeps its own
  threadKey = key; threadSeen = j.seen || ''
  if (!j.same) messages.value = j.messages
}

// the newest message in view: on opening a conversation, and when one arrives while the reader is at the bottom
let shownKey = ''
watch(messages, async () => {
  const el = list.value
  const opened = current.value?.key !== shownKey
  const atEnd = !el || el.scrollHeight - el.scrollTop - el.clientHeight < 40
  shownKey = current.value?.key ?? ''
  if (!opened && !atEnd) return
  await nextTick()
  if (list.value) list.value.scrollTop = list.value.scrollHeight
})
async function open(c: SmsConversation) {
  current.value = c
  const own = (c.devices || []).find((d) => devices.value.some((x) => x.device === d))
  if (own) sendVia.value = own // answer through a phone that carries this conversation
  await loadThread()
}
function startNew() { threadLoad.drop(); threadKey = ''; current.value = { key: '', number: '', name: '' }; messages.value = []; newNumber.value = '' }
async function send() {
  if (!current.value || sending.value) return // Ctrl+Enter twice, or with the button busy, sends once
  const number = current.value.key ? current.value.number : newNumber.value.trim()
  if (!number) { error.value = 'Enter the phone number to send to.'; return }
  sending.value = true; error.value = ''
  try {
    const r = await apiCall('POST', '/sms/send', { device: sendVia.value, number, body: text.value })
    text.value = ''
    await load()
    if (!current.value.key) {
      // the conversation the server filed it under: a number typed another way (+44 or 0, with spaces) is still that person's
      const c = convs.value.find((x) => x.key === r.key)
      if (c) await open(c)
    } else await loadThread()
  } catch (e) { error.value = (e as Error).message } finally { sending.value = false }
}

// every 5 s while the page is in view: each check waits for the one before (on a slow link they piled up), and a hidden tab
// does not ask at all; coming back to it checks at once
async function poll() {
  if (!document.hidden) await load()
  if (alive) timer = setTimeout(poll, 5000)
}
const onVisible = () => { if (!document.hidden) { clearTimeout(timer); poll() } }
onMounted(() => { poll(); document.addEventListener('visibilitychange', onVisible) })
onBeforeUnmount(() => { alive = false; clearTimeout(timer); document.removeEventListener('visibilitychange', onVisible) })
</script>

<style scoped>
.ph-chat { display: grid; grid-template-columns: minmax(220px, 320px) 1fr; gap: var(--ph-s4); margin-top: var(--ph-s4); align-items: start; }
.ph-conv, .ph-thread { display: flex; flex-direction: column; gap: var(--ph-s3); }
.ph-row { display: flex; align-items: center; justify-content: space-between; gap: var(--ph-s2); }
.ph-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: var(--ph-s1, 4px); }
.ph-conv-item { display: flex; flex-direction: column; align-items: flex-start; width: 100%; text-align: left; padding: var(--ph-s3); border: 1px solid var(--ph-border); border-radius: var(--ph-r-control); background: transparent; color: inherit; cursor: pointer; }
.ph-conv-item.ph-sel { border-color: var(--ph-accent, #0b5cab); }
.ph-conv-name { font-weight: 600; }
.ph-conv-last { max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ph-msgs { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: var(--ph-s3); max-height: 50vh; overflow: auto; }
.ph-msgs li { display: flex; flex-direction: column; gap: 2px; max-width: 80%; }
.ph-in { align-self: flex-start; }
.ph-out { align-self: flex-end; align-items: flex-end; }
.ph-bubble { padding: var(--ph-s2) var(--ph-s3); border-radius: var(--ph-r-control); background: var(--ph-surface-2, var(--ph-bg)); white-space: pre-wrap; overflow-wrap: anywhere; }
.ph-out .ph-bubble { background: var(--ph-accent, #0b5cab); color: var(--ph-on-accent, #fff); }
.ph-compose { display: flex; flex-direction: column; gap: var(--ph-s2); }
.ph-error { display: flex; gap: var(--ph-s2); align-items: flex-start; color: var(--ph-negative); }
.ph-note { display: flex; gap: var(--ph-s2); align-items: flex-start; padding: var(--ph-s3); border-radius: var(--hd-radius-card); background: var(--ph-surface-2, var(--ph-bg)); }
.ph-back { display: none; }
@media (max-width: 719px) {
  .ph-chat { grid-template-columns: 1fr; }
  .ph-chat.ph-open .ph-conv { display: none; }
  .ph-back { display: inline-flex; }
}
</style>
