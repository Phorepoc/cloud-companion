<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: AGPL-3.0-only -->
<template>
  <q-page class="ph-chats" :style-fn="fillPage">
    <SplitView :detail="!!current" list-label="Conversations" detail-label="Messages">
      <template #list>
        <div class="ph-chats__head">
          <HdPageHeader :level="2" title="Chats" :subtitle="subtitle" colored accent="#09A7CC" />
          <q-btn flat round text-color="white" icon="sym_o_edit_square" aria-label="New message" data-test="new-chat" @click="startNew" />
        </div>
        <div v-if="convs.length > 4 || showSearch" class="ph-list-search"><HdSearchField v-model="q" placeholder="Search chats" label="Search chats" /></div>
        <div class="ph-scroll ph-chats__list">
          <div v-if="error" class="ph-banner is-error ph-chats__note" role="alert" data-test="chat-error"><q-icon name="sym_o_error" aria-hidden="true" /><span>{{ error }}</span></div>
          <p v-if="loaded && !devices.length" class="ph-banner ph-chats__note" role="note" data-test="no-sms-devices">
            <q-icon name="sym_o_info" aria-hidden="true" /><span>Text messages (SMS) from your own phones show here: each phone syncs its own, and a message you send goes out through the phone you pick.
              No phone is sharing its text messages yet. Pair a phone that has the Cloud Companion app, and its messages will appear here.</span>
          </p>
          <HdEmptyState v-if="loaded && devices.length && !convs.length" text="No messages yet. Text messages from your phones show here; a message you send goes out through the phone you pick." data-test="no-convs" />
          <ul class="ph-rows">
            <li v-for="c in shown" :key="c.key">
              <button type="button" class="ph-row2" :class="{ 'is-selected': current && current.key === c.key }" data-test="conv" @click="open(c)">
                <HdAvatar v-if="c.name || !/\d/.test(c.number)" :name="c.name || c.number" :size="43" /><HdThumbnail v-else icon="person" :size="43" :color="tint(c.number)" />
                <span class="ph-row2__text">
                  <span class="ph-row2__top"><span class="ph-row2__title">{{ c.name || c.number }}</span><span class="ph-row2__time">{{ shortWhen(c.last_ts) }}</span></span>
                  <span class="ph-row2__sub">{{ c.last_dir === 'out' ? 'You: ' : '' }}{{ c.last }}</span>
                  <span v-if="c.last_failed" class="ph-chats__failed">Not sent</span>
                </span>
              </button>
            </li>
          </ul>
        </div>
      </template>

      <template #detail>
        <div v-if="current" class="ph-chats__thread" data-test="thread">
          <header class="ph-detail-head">
            <template v-if="current.key"><HdAvatar v-if="current.name || !/\d/.test(current.number)" :name="current.name || current.number" :size="43" /><HdThumbnail v-else icon="person" :size="43" :color="tint(current.number)" /></template>
            <div class="ph-detail-head__text">
              <b>{{ current.name || current.number || 'New message' }}</b>
              <span>{{ current.name ? current.number + ' · ' : '' }}<template v-if="deviceOptions.length === 1">sent through <span data-test="send-via">{{ deviceOptions[0].label }}</span></template>
                <!-- several phones: which one sends is a menu here, not a field in the composer -->
                <button v-else-if="deviceOptions.length > 1" type="button" class="ph-chats__via hd-focusable" aria-haspopup="menu" data-test="send-via">sent through {{ deviceName(sendVia) }} <q-icon name="sym_o_expand_more" size="16px" aria-hidden="true" />
                  <q-menu auto-close><q-list style="min-width: 180px"><q-item v-for="d in deviceOptions" :key="d.value" clickable :active="d.value === sendVia" @click="sendVia = d.value"><q-item-section>{{ d.label }}</q-item-section></q-item></q-list></q-menu>
                </button><template v-else>text messages</template></span>
            </div>
          </header>
          <div v-if="!current.key" class="ph-chats__to"><q-input v-model="newNumber" outlined dense label="To (phone number)" type="tel" data-test="new-number" /></div>
          <ul ref="list" class="ph-msgs ph-scroll" aria-live="polite">
            <li v-for="m in messages" :key="m.id" :class="m.dir === 'out' ? 'ph-out' : 'ph-in'" data-test="msg">
              <span class="ph-bubble">{{ m.body }}<span class="ph-stamp">{{ when(m.ts) }}<template v-if="devices.length > 1"> · {{ deviceName(m.device) }}</template></span></span>
              <span v-if="m.dir === 'out'" class="ph-state" :class="{ 'is-failed': m.status === 'failed' }" data-test="msg-status">{{ statusText(m) }}</span>
            </li>
          </ul>
          <!-- the N9 Messages composer strip (Quasar/Examples, Messaging app): Back on a phone, the dark field, the grey squircle Send -->
          <div class="q-footer ph-chats__compose">
            <q-toolbar class="hd-q-free">
              <q-btn flat class="hd-sq hd-q-back ph-split__back" aria-label="Back to the conversations" @click="current = null">
                <svg width="33" height="33" viewBox="0 0 24 24" aria-hidden="true"><path :d="BACK_ARROW_PATH" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
              </q-btn>
              <q-input v-model="text" dense outlined autogrow class="col ph-chats__field" placeholder="Write a message" aria-label="Message (Ctrl+Enter sends)" data-test="sms-text" @keyup.ctrl.enter="send" />
              <q-btn flat class="hd-sq ph-chats__send" icon="sym_o_chat_bubble" aria-label="Send" :disable="!text.trim() || !sendVia" :loading="sending" data-test="sms-send" @click="send" />
            </q-toolbar>
          </div>
        </div>
        <div v-else class="ph-split__empty">
          <div><q-icon name="sym_o_chat" size="48px" aria-hidden="true" /><p>Pick a conversation, or start a new one.</p></div>
        </div>
      </template>
    </SplitView>
  </q-page>
</template>

<script setup lang="ts">
import { ref, computed, nextTick, onMounted, onBeforeUnmount, watch } from 'vue'
import { useChrome } from '@etesian-design/shell'
import { HdAvatar, HdEmptyState, HdPageHeader, HdSearchField, HdThumbnail } from '@etesian-design/vue'
import SplitView from '../components/SplitView.vue'
import { BACK_ARROW_PATH } from '@etesian-design/vue/components/backArrow'
import { fillPage } from '../fillPage'
import { apiCall } from '../cloud'
import { latest } from '../latest'
import type { SmsConversation, SmsDevice, SmsMessage } from '../proto'

// the title is in the host's header (the classic bar, a desktop window)
// the window's (or the classic bar's) actions, as the Mail window in Desktop/Window actions
const chrome = useChrome(() => ({ title: 'Chats', tools: [{ icon: 'search', label: 'Search' }, { icon: 'refresh', label: 'Refresh' }] }))
chrome.on('tool', (l) => {
  if (l === 'Refresh') { convSeen = ''; threadSeen = ''; load() }
  else if (l === 'Search') { current.value = null; showSearch.value = true; nextTick(() => (document.querySelector('.ph-chats .hd-search input') as HTMLInputElement | null)?.focus()) }
})
const showSearch = ref(false)

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
const q = ref('')
const shown = computed(() => {
  const w = q.value.trim().toLowerCase()
  return w ? convs.value.filter((c) => [c.name, c.number, c.last].some((x) => x && x.toLowerCase().includes(w))) : convs.value
})
const subtitle = computed(() => !devices.value.length ? 'Text messages from your phones'
  : `${convs.value.length} conversation${convs.value.length === 1 ? '' : 's'} · ${devices.value.map((d) => d.name || 'phone').join(', ')}`)
/** a number without a name gets the person glyph on a squircle, in one of the avatar colours (HdAvatar's palette) picked by the number */
const TINTS = ['#007DC9', '#09A7CC', '#62B700', '#821DDE', '#CC09BA', '#EF5500', '#ED9507', '#0067BC']
const tint = (n: string) => TINTS[[...n.replace(/\D/g, '')].reduce((a, c) => a + c.charCodeAt(0), 0) % TINTS.length]
const shortWhen = (t: number) => {
  const d = new Date(t * 1000), now = new Date()
  if (d.toDateString() === now.toDateString()) return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
  if (now.getTime() - d.getTime() < 6 * 86400000) return d.toLocaleDateString(undefined, { weekday: 'short' })
  return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
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
.ph-chats { display: flex; flex-direction: column; }
.ph-chats__list { flex: 1; min-height: 0; }
.ph-chats__head { position: relative; flex: none; }
.ph-chats__head .hd-page-header { padding-right: 60px; }
.ph-chats__head > .q-btn { position: absolute; top: 50%; right: 8px; transform: translateY(-50%); color: #fff; }
.ph-chats__note { margin: 10px 11px; }
.ph-chats__failed { font-size: var(--hd-font-size-xsmall); color: var(--ph-negative); }
.ph-chats__thread { flex: 1; min-height: 0; display: flex; flex-direction: column; }
.ph-chats__to { flex: none; padding: 10px 16px; border-bottom: 1px solid var(--hd-c-divider); }
/* the conversation: bubbles as Quasar/Examples Messaging (q-chat-message in the Etesian theme), newest at the bottom */
.ph-msgs { flex: 1; min-height: 0; list-style: none; margin: 0; padding: 16px; display: flex; flex-direction: column; gap: 8px; }
.ph-msgs > :first-child { margin-top: auto; }
.ph-msgs li { display: flex; flex-direction: column; gap: 2px; max-width: min(80%, 560px); }
.ph-in { align-self: flex-start; }
.ph-out { align-self: flex-end; align-items: flex-end; }
.ph-bubble { display: flex; flex-direction: column; gap: 2px; padding: 7px 11px; border-radius: 12px; background: var(--hd-surface-card, var(--hd-c-surface)); color: var(--hd-surface-card-fg, var(--hd-c-fg));
  box-shadow: 0 0 0 1px var(--hd-c-divider); font: 400 var(--hd-font-size-default)/1.35 var(--hd-font-family-base); white-space: pre-wrap; overflow-wrap: anywhere; }
.ph-in .ph-bubble { border-bottom-left-radius: 0; }
.ph-out .ph-bubble { background: #268CE1; color: #fff; box-shadow: none; border-bottom-right-radius: 0; }
.ph-stamp { font-size: var(--hd-font-size-xsmall); opacity: .75; white-space: nowrap; }
.ph-state { font-size: var(--hd-font-size-xsmall); color: var(--hd-c-fg-secondary); }
.ph-state.is-failed { color: var(--ph-negative); }
/* the composer strip: the light N9 toolbar (dark in dark mode), a dark rounded field, a grey lit squircle Send */
.ph-chats__compose { position: relative; z-index: auto; flex: none; }
.ph-chats__compose .q-toolbar { gap: 8px; padding: 6px 8px; min-height: 56px; align-items: center; }
.ph-chats__field :deep(.q-field__control) { min-height: 38px; border-radius: 12px; background: #222; color: #fff; }
.ph-chats__field :deep(.q-field__control::before) { border-color: rgba(0,0,0,.6) !important; border-radius: 12px; }
.ph-chats__field :deep(.q-field__native) { color: #fff; }
.ph-chats__field :deep(.q-field__native::placeholder) { color: #9a9a9a; opacity: 1; }
.ph-chats__send.q-btn { --hd-sq-size: 44px; --hd-sq-fill: linear-gradient(#A6A6A8, #858587 50%, #6C6C6E); --hd-sq-ring: rgba(0,0,0,.3); color: #fff; width: 53px; min-width: 53px; height: 44px; }
.ph-chats__send.q-btn .q-btn__content { position: relative; z-index: 1; }
.ph-chats__send.q-btn.disabled { opacity: .55 !important; }
.ph-chats__via { all: unset; display: inline-flex; align-items: center; gap: 2px; color: var(--hd-c-accent-text, var(--hd-accent)); cursor: pointer; }
</style>
