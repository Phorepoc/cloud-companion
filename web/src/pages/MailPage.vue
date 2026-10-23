<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- Mail over the hub (JMAP through the server): folders, a message list, a reader with actions (mark read/unread, move, delete with
     Undo, reply) and a composer. Messages are shown as plain text; "Formatted view" shows the server-sanitised HTML in a sandboxed
     frame with no network access (no pictures, scripts or styles of the message load). Sending only reaches the hub's own addresses
     unless the server operator allows more. -->
<template>
  <q-page class="ph-page">
    <div class="ph-head">
      <h1>Mail</h1>
      <div class="ph-actions">
        <q-btn flat round icon="sym_o_refresh" aria-label="Refresh" data-test="mail-refresh" @click="refresh" />
        <q-btn unelevated color="primary" icon="sym_o_edit" label="Write" data-test="mail-compose" @click="openCompose()" />
      </div>
    </div>
    <q-select v-model="folder" :options="folderOptions" emit-value map-options outlined label="Folder" class="q-mb-md" data-test="mail-folder"
              @update:model-value="loadList(true)" />

    <q-banner v-if="error" class="ph-note is-error q-mb-md" rounded role="alert">
      <template #avatar><q-icon name="sym_o_error" aria-hidden="true" /></template>
      {{ error }}
      <template #action><q-btn flat label="Try again" @click="init" /></template>
    </q-banner>
    <EmptyState v-if="loading && !mails.length" state="loading" title="Loading mail" text="This takes a moment." />
    <EmptyState v-else-if="!loading && !mails.length && !error && folder" icon="sym_o_inbox" title="No messages here" text="This folder is empty." />

    <ul v-if="mails.length" class="ph-list ph-card" data-test="mail-list">
      <li v-for="(m, i) in mails" :key="m.id">
        <button type="button" class="ph-row" :class="{ 'is-unread': m.unread }" :data-test="'mail-row-' + i" @click="openMail(m)">
          <span class="ph-row-main">
            <span class="ph-row-top"><span class="ph-from">{{ m.from || '(no sender)' }}</span><span class="ph-small ph-muted">{{ when(m.date) }}</span></span>
            <span class="ph-subject">{{ m.subject || '(no subject)' }}<q-icon v-if="m.attach" name="sym_o_attach_file" size="16px" aria-label="Has attachments" /></span>
            <span class="ph-small ph-muted ph-preview">{{ m.preview }}</span>
          </span>
          <span v-if="m.unread" class="ph-dot" aria-label="Unread" data-test="mail-unread"></span>
        </button>
      </li>
    </ul>
    <div v-if="mails.length && mails.length < total" class="ph-more">
      <q-btn outline color="primary" label="Show older messages" :loading="loading" data-test="mail-more" @click="loadList(false)" />
    </div>

    <!-- reader -->
    <q-dialog v-model="open">
      <q-card class="ph-dialog" data-test="mail-view">
        <q-card-section v-if="reading"><EmptyState state="loading" title="Opening the message" text="" /></q-card-section>
        <q-card-section v-else-if="msg">
          <h2 data-test="mail-view-subject">{{ msg.subject || '(no subject)' }}</h2>
          <p class="ph-small ph-muted q-mb-xs">From {{ msg.from }}<span v-if="msg.date"> on {{ new Date(msg.date).toLocaleString() }}</span></p>
          <p v-if="msg.to && msg.to.length" class="ph-small ph-muted">To {{ msg.to.join(', ') }}</p>
          <q-btn-toggle v-if="msg.safe" v-model="formatted" no-caps unelevated dense toggle-color="primary" toggle-text-color="on-accent" color="surface-2" text-color="ph" class="q-mb-sm"
                        :options="[{ label: 'Plain text', value: false }, { label: 'Formatted view', value: true }]" data-test="mail-format" />
          <p v-if="msg.html && !formatted" class="ph-small ph-muted">Shown as plain text. Pictures and links in this message are not loaded.</p>
          <iframe v-if="formatted && msg.safe" class="ph-frame" sandbox="allow-popups allow-popups-to-escape-sandbox" :srcdoc="frameDoc" title="Message, formatted" data-test="mail-frame"></iframe>
          <pre v-else class="ph-body" data-test="mail-view-body">{{ msg.text }}</pre>
        </q-card-section>
        <q-card-actions v-if="msg" class="ph-reader-actions">
          <q-btn flat icon="sym_o_reply" label="Reply" data-test="mail-reply" @click="reply" />
          <q-btn flat :icon="current && current.unread ? 'sym_o_drafts' : 'sym_o_mark_email_unread'" :label="current && current.unread ? 'Mark as read' : 'Mark as unread'" data-test="mail-toggle-read" @click="toggleRead" />
          <q-btn flat icon="sym_o_drive_file_move" label="Move to" data-test="mail-move">
            <q-menu>
              <q-list style="min-width: 180px">
                <q-item v-for="t in moveTargets" :key="t.role" clickable v-close-popup :data-test="'mail-move-' + t.role" @click="move(t)"><q-item-section>{{ t.label }}</q-item-section></q-item>
              </q-list>
            </q-menu>
          </q-btn>
          <q-btn flat color="negative" icon="sym_o_delete" label="Delete" data-test="mail-delete" @click="remove" />
          <q-space />
          <q-btn flat label="Close" v-close-popup />
        </q-card-actions>
      </q-card>
    </q-dialog>

    <!-- composer -->
    <q-dialog v-model="composeOpen" persistent>
      <q-card class="ph-dialog" data-test="mail-compose-form">
        <form @submit.prevent="send">
          <q-card-section class="ph-formcol">
            <h2>{{ draft.replyTo ? 'Reply' : 'New message' }}</h2>
            <q-input v-model="draft.rcpt" label="To" outlined type="email" multiple autofocus hint="Separate addresses with commas." data-test="mail-to" />
            <q-input v-model="draft.subject" label="Subject" outlined data-test="mail-subject" />
            <q-input v-model="draft.body" label="Message" type="textarea" outlined autogrow :input-style="{ minHeight: '160px' }" data-test="mail-body" />
            <p v-if="sendError" class="ph-err" role="alert"><q-icon name="sym_o_error" size="20px" aria-hidden="true" /> {{ sendError }}</p>
          </q-card-section>
          <q-card-actions align="right">
            <q-btn flat label="Cancel" :disable="sending" data-test="mail-cancel" @click="composeOpen = false" />
            <q-btn unelevated color="primary" icon="sym_o_send" label="Send" type="submit" :loading="sending" data-test="mail-send" />
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
import EmptyState from '../components/EmptyState.vue'

const folders = ref([])
const folder = ref('')
const mails = ref([])
const total = ref(0)
const loading = ref(false)
const error = ref('')
const open = ref(false)
const reading = ref(false)
const msg = ref(null)
const current = ref(null)
const formatted = ref(false)
const composeOpen = ref(false)
const sending = ref(false)
const sendError = ref('')
const draft = ref({ rcpt: '', subject: '', body: '', replyTo: '' })

const order = { inbox: 0, drafts: 1, sent: 2, archive: 3, junk: 4, trash: 5 }
const folderOptions = computed(() => [...folders.value].sort((a, b) => (order[a.role] ?? 9) - (order[b.role] ?? 9) || a.name.localeCompare(b.name))
  .map((f) => ({ label: f.name + (f.unread ? ` (${f.unread} unread)` : ''), value: f.id })))
const role = computed(() => (folders.value.find((f) => f.id === folder.value) || {}).role || '')
const moveTargets = computed(() => [{ role: 'inbox', label: 'Inbox' }, { role: 'archive', label: 'Archive' }, { role: 'junk', label: 'Junk' }, { role: 'trash', label: 'Trash' }].filter((t) => t.role !== role.value && folders.value.some((f) => f.role === t.role)))
// the formatted view: sanitised by the server, shown in a sandboxed frame whose content policy forbids every load
const frameDoc = computed(() => `<!doctype html><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'"><base target="_blank"><style>body{font:15px system-ui,sans-serif;margin:8px;overflow-wrap:anywhere;color:#1a1d21;background:#fff}blockquote{margin:0 0 0 8px;padding-left:8px;border-left:3px solid #ccc}</style>${msg.value ? msg.value.safe : ''}`)

function when(iso) {
  const d = new Date(iso)
  return d.toDateString() === new Date().toDateString() ? d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }) : d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
}

async function init() {
  error.value = ''
  try {
    folders.value = (await hubCall({ op: 'mail.folders' })).folders || []
    if (!folder.value) folder.value = (folders.value.find((f) => f.role === 'inbox') || folders.value[0] || {}).id || ''
    if (folder.value) await loadList(true)
  } catch (e) {
    error.value = /not set up/i.test(e.message) ? 'The hub is not set up on this server, so there is no mail to show. See the hub guide to set it up.' : e.message
  }
}
async function refresh() {
  try { folders.value = (await hubCall({ op: 'mail.folders' })).folders || folders.value } catch (e) { /* the list shows the error */ }
  await loadList(true)
}

async function loadList(reset) {
  if (!folder.value) return
  loading.value = true
  error.value = ''
  try {
    const r = await hubCall({ op: 'mail.list', folder: folder.value, pos: reset ? 0 : mails.value.length })
    mails.value = reset ? r.mails || [] : [...mails.value, ...(r.mails || [])]
    total.value = r.total || mails.value.length
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

async function openMail(m) {
  current.value = m
  open.value = true
  reading.value = true
  formatted.value = false
  msg.value = null
  try {
    msg.value = (await hubCall({ op: 'mail.read', id2: m.id })).mail
  } catch (e) {
    open.value = false
    error.value = e.message
  } finally {
    reading.value = false
  }
}

async function toggleRead() {
  const m = current.value
  const toRead = m.unread
  try {
    await hubCall({ op: 'mail.mark', id2: m.id, flag: toRead ? 'read' : 'unread' })
    m.unread = !toRead
    toast(toRead ? 'Marked as read' : 'Marked as unread')
  } catch (e) {
    error.value = e.message
  }
}

// moving and deleting wait for the length of the Undo toast before they reach the hub
function leave(m, label, action) {
  open.value = false
  const i = mails.value.findIndex((x) => x.id === m.id)
  mails.value = mails.value.filter((x) => x.id !== m.id)
  toast(label, {
    undo: () => { const l = [...mails.value]; l.splice(Math.min(Math.max(i, 0), l.length), 0, m); mails.value = l },
    onExpire: async () => { try { await action(); await refreshFolders() } catch (e) { error.value = e.message; await loadList(true) } },
  })
}
async function refreshFolders() { try { folders.value = (await hubCall({ op: 'mail.folders' })).folders || folders.value } catch (e) { /* ignore */ } }
const remove = () => leave(current.value, role.value === 'trash' ? 'Message deleted for good' : 'Moved to Trash', () => hubCall({ op: 'mail.delete', id2: current.value.id }))
function move(t) {
  const m = current.value
  leave(m, `Moved to ${t.label}`, () => hubCall({ op: 'mail.move', id2: m.id, role: t.role }))
}

function openCompose(init) {
  draft.value = { rcpt: '', subject: '', body: '', replyTo: '', ...init }
  sendError.value = ''
  open.value = false
  composeOpen.value = true
}
function reply() {
  const m = msg.value
  const quoted = (m.text || '').split('\n').map((l) => '> ' + l).join('\n')
  openCompose({ rcpt: m.fromaddr || '', subject: /^re:/i.test(m.subject) ? m.subject : 'Re: ' + m.subject, replyTo: m.msgid || '',
    body: `\n\nOn ${new Date(m.date).toLocaleString()}, ${m.from} wrote:\n${quoted}` })
}
async function send() {
  sending.value = true
  sendError.value = ''
  try {
    await hubCall({ op: 'mail.send', rcpt: draft.value.rcpt, subject: draft.value.subject, body: draft.value.body, replyto: draft.value.replyTo || undefined })
    composeOpen.value = false
    toast('Message sent')
    await refresh()
  } catch (e) {
    sendError.value = e.message
  } finally {
    sending.value = false
  }
}

onMounted(init)
</script>

<style scoped>
.ph-head { display: flex; align-items: center; justify-content: space-between; gap: var(--ph-s3); margin-bottom: var(--ph-s3); }
.ph-actions { display: flex; align-items: center; gap: var(--ph-s2); }
.ph-list { list-style: none; padding: 0; margin: 0; }
.ph-list li { border-bottom: 1px solid var(--ph-divider); }
.ph-list li:last-child { border-bottom: 0; }
.ph-row { width: 100%; min-height: 64px; display: flex; align-items: center; gap: var(--ph-s3); padding: var(--ph-s3) var(--ph-s4); background: none; border: 0; text-align: left; font: inherit; color: var(--ph-text); cursor: pointer; }
.ph-row:hover { background: var(--ph-surface-2); }
.ph-row-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.ph-row-top { display: flex; justify-content: space-between; gap: var(--ph-s3); }
.ph-from, .ph-subject { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.is-unread .ph-from, .is-unread .ph-subject { font-weight: 600; }
.ph-preview { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ph-dot { flex: none; width: 10px; height: 10px; border-radius: 50%; background: var(--ph-accent); }
.ph-more { display: flex; justify-content: center; margin-top: var(--ph-s4); }
.ph-dialog { width: min(96vw, 640px); }
.ph-dialog h2 { margin-bottom: var(--ph-s2); overflow-wrap: anywhere; }
.ph-formcol { display: flex; flex-direction: column; gap: var(--ph-s3); }
.ph-reader-actions { flex-wrap: wrap; }
.ph-body { white-space: pre-wrap; overflow-wrap: anywhere; font-family: var(--ph-font); font-size: var(--ph-text-body); margin: var(--ph-s3) 0 0; max-height: 50vh; overflow: auto; }
.ph-frame { width: 100%; height: 50vh; border: 1px solid var(--ph-divider); border-radius: var(--ph-r-control); background: #fff; }
.ph-err { color: var(--ph-negative); margin: 0; display: flex; gap: var(--ph-s2); }
.ph-note { background: var(--ph-surface-2); color: var(--ph-text); border: 1px solid var(--ph-divider); }
.ph-note.is-error { border-color: var(--ph-negative); }
</style>
