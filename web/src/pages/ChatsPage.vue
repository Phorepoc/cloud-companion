<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="ph-page">
    <h1>Chats</h1>
    <p class="ph-muted">Text messages (SMS) from your own phones. Each phone syncs its own messages here; a message you send goes out through the phone you pick.</p>

    <div v-if="error" class="ph-error" role="alert" data-test="chat-error"><q-icon name="sym_o_error" size="20px" aria-hidden="true" /> {{ error }}</div>

    <p v-if="loaded && !devices.length" class="ph-note" role="note" data-test="no-sms-devices">
      <q-icon name="sym_o_info" size="20px" aria-hidden="true" /> No phone is syncing messages yet. Install the Phecloud messages service on a phone and pair it; its messages then appear here.
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
        <ul class="ph-msgs" aria-live="polite">
          <li v-for="m in messages" :key="m.id" :class="m.dir === 'out' ? 'ph-out' : 'ph-in'" data-test="msg">
            <span class="ph-bubble">{{ m.body }}</span>
            <span class="ph-muted ph-small">{{ when(m.ts) }} · {{ deviceName(m.device) }}<template v-if="m.dir === 'out'"> · <span :class="m.status === 'failed' ? 'ph-error' : ''" data-test="msg-status">{{ statusText(m) }}</span></template></span>
          </li>
        </ul>
        <div class="ph-compose">
          <q-select v-model="sendVia" :options="deviceOptions" emit-value map-options outlined dense label="Send through" data-test="send-via" :disable="!deviceOptions.length" />
          <q-input v-model="text" outlined dense autogrow label="Message" data-test="sms-text" @keyup.ctrl.enter="send" />
          <q-btn unelevated color="primary" icon="sym_o_send" label="Send" :disable="!text.trim() || !sendVia" :loading="sending" data-test="sms-send" @click="send" />
        </div>
      </section>
    </div>
  </q-page>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { apiCall } from '../cloud.js'

const convs = ref([])
const devices = ref([])
const current = ref(null)
const messages = ref([])
const loaded = ref(false)
const error = ref('')
const text = ref('')
const sendVia = ref('')
const sending = ref(false)
const newNumber = ref('')
let timer = null

const deviceName = (id) => (devices.value.find((d) => d.device === id) || {}).name || 'phone'
const deviceOptions = computed(() => devices.value.map((d) => ({ label: d.name || d.device, value: d.device })))
const statusText = (m) => ({ queued: 'waiting for the phone', sent: 'sent', delivered: 'delivered', failed: 'not sent' + (m.error ? ' (' + m.error + ')' : '') }[m.status] || m.status)
const when = (t) => new Date(t * 1000).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })

async function load() {
  try {
    const j = await apiCall('GET', '/sms/conversations')
    convs.value = j.conversations
    devices.value = j.devices
    if (!sendVia.value && devices.value.length) sendVia.value = devices.value[0].device
    if (current.value && current.value.key) await loadThread()
    error.value = ''
  } catch (e) { error.value = e.message } finally { loaded.value = true }
}
async function loadThread() {
  messages.value = (await apiCall('GET', '/sms/thread?key=' + encodeURIComponent(current.value.key))).messages
}
async function open(c) {
  current.value = c
  const own = (c.devices || []).find((d) => devices.value.some((x) => x.device === d))
  if (own) sendVia.value = own // answer through a phone that carries this conversation
  await loadThread()
}
function startNew() { current.value = { key: '', number: '', name: '' }; messages.value = []; newNumber.value = '' }
async function send() {
  const number = current.value.key ? current.value.number : newNumber.value.trim()
  if (!number) { error.value = 'Enter the phone number to send to.'; return }
  sending.value = true; error.value = ''
  try {
    await apiCall('POST', '/sms/send', { device: sendVia.value, number, body: text.value })
    text.value = ''
    await load()
    if (!current.value.key) {
      const c = convs.value.find((x) => x.number === number) || convs.value[0]
      if (c) await open(c)
    } else await loadThread()
  } catch (e) { error.value = e.message } finally { sending.value = false }
}

onMounted(() => { load(); timer = setInterval(load, 5000) })
onBeforeUnmount(() => clearInterval(timer))
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
.ph-note { display: flex; gap: var(--ph-s2); align-items: flex-start; padding: var(--ph-s3); border-radius: var(--ph-r-control); background: var(--ph-surface-2, var(--ph-bg)); }
.ph-back { display: none; }
@media (max-width: 719px) {
  .ph-chat { grid-template-columns: 1fr; }
  .ph-chat.ph-open .ph-conv { display: none; }
  .ph-back { display: inline-flex; }
}
</style>
