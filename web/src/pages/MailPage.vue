<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: AGPL-3.0-only -->
<!-- Mail over the hub (JMAP through the server): folders, a message list, a reader with actions (mark read/unread, move, delete with
     Undo, reply) and a composer. Messages are shown as plain text; "Formatted view" shows the server-sanitised HTML in a sandboxed
     frame with no network access (no pictures, scripts or styles of the message load). Sending only reaches the hub's own addresses
     unless the server operator allows more. -->
<template>
  <q-page class="ph-mail" :style-fn="fillPage" @keydown.esc="open = false">
    <SplitView :detail="open" list-label="Messages" detail-label="Message">
      <template #list>
        <HdPageHeader :level="2" :title="folderTitle" :subtitle="folderSub" colored accent="#0067BC" />
        <!-- in a window (or a host with a header) the title is a menu of the folders and the buttons sit in the header: useChrome below -->
        <div v-if="!chrome.hosted" class="ph-mail__bar">
          <q-select v-model="folder" :options="folderOptions" emit-value map-options outlined dense label="Folder" class="col" data-test="mail-folder"
                    @update:model-value="loadList(true)" />
          <q-btn flat round icon="sym_o_refresh" aria-label="Refresh" data-test="mail-refresh" @click="refresh" />
          <q-btn unelevated color="primary" icon="sym_o_edit" label="Write" data-test="mail-compose" @click="openCompose()" />
        </div>
        <div class="ph-scroll ph-mail__list">
          <q-banner v-if="error" inline-actions class="ph-note is-error ph-mail__note" role="alert">
            <template #avatar><q-icon name="sym_o_error" aria-hidden="true" /></template>
            {{ error }}
            <template #action><q-btn flat no-caps color="primary" label="Try again" @click="init" /></template>
          </q-banner>
          <HdStatusState v-if="loading && !mails.length" state="loading" title="Loading mail" text="This takes a moment." />
          <HdStatusState v-else-if="!loading && !mails.length && !error && folder" icon="mail" title="No messages here" text="This folder is empty." />

          <ul v-if="mails.length" class="ph-rows" data-test="mail-list">
            <li v-for="(m, i) in mails" :key="m.id">
              <button type="button" class="ph-row2" :class="{ 'is-unread': m.unread, 'is-selected': open && current && current.id === m.id }" :data-test="'mail-row-' + i" @click="openMail(m)">
                <HdAvatar :name="m.from || '?'" :size="32" />
                <span class="ph-row2__text">
                  <span class="ph-row2__top"><span class="ph-row2__title">{{ m.from || '(no sender)' }}</span><span class="ph-row2__time">{{ when(m.date) }}</span></span>
                  <span class="ph-row2__line">{{ m.subject || '(no subject)' }}<q-icon v-if="m.attach" name="sym_o_attach_file" size="16px" aria-label="Has attachments" /></span>
                  <span class="ph-row2__sub">{{ m.preview }}</span>
                </span>
                <span v-if="m.unread" class="ph-dot" aria-label="Unread" data-test="mail-unread"></span>
              </button>
            </li>
          </ul>
          <div v-if="mails.length && mails.length < total" class="ph-more">
            <q-btn outline no-caps color="primary" label="Show older messages" :loading="loading" data-test="mail-more" @click="loadList(false)" />
          </div>
        </div>
      </template>

      <!-- the reader: beside the list when there is room, else in its place with Back -->
      <template #detail>
        <article v-if="open" class="ph-mail__reader ph-scroll" data-test="mail-view">
          <q-btn flat no-caps icon="sym_o_arrow_back" :label="folderTitle" class="ph-split__back ph-mail__back" @click="open = false" />
          <HdStatusState v-if="reading" state="loading" title="Opening the message" text="" />
          <template v-else-if="msg">
            <h2 class="ph-detail-title" data-test="mail-view-subject">{{ msg.subject || '(no subject)' }}</h2>
            <div class="ph-mail__from">
              <HdAvatar :name="msg.from || '?'" :size="43" />
              <div><b>{{ msg.from }}</b><span v-if="msg.to && msg.to.length">to {{ msg.to.join(', ') }}</span><span v-if="msg.date"> · {{ new Date(msg.date).toLocaleString() }}</span></div>
            </div>
            <q-btn-toggle v-if="msg.safe" v-model="formatted" no-caps unelevated dense toggle-color="primary" class="q-mb-sm"
                          :options="[{ label: 'Plain text', value: false }, { label: 'Formatted view', value: true }]" data-test="mail-format" />
            <p v-if="msg.html && !formatted" class="ph-small ph-muted">Shown as plain text. Pictures and links in this message are not loaded.</p>
            <iframe v-if="formatted && msg.safe" class="ph-frame" sandbox="allow-popups allow-popups-to-escape-sandbox" :srcdoc="frameDoc" title="Message, formatted" data-test="mail-frame"></iframe>
            <pre v-else class="ph-body" data-test="mail-view-body">{{ msg.text }}</pre>
            <div class="ph-mail__actions">
              <q-btn unelevated no-caps color="primary" icon="sym_o_reply" label="Reply" data-test="mail-reply" @click="reply" />
              <q-btn outline no-caps :icon="current && current.unread ? 'sym_o_drafts' : 'sym_o_mark_email_unread'" :label="current && current.unread ? 'Mark as read' : 'Mark as unread'" data-test="mail-toggle-read" @click="toggleRead" />
              <q-btn outline no-caps icon="sym_o_drive_file_move" label="Move to" data-test="mail-move">
                <q-menu>
                  <q-list style="min-width: 180px">
                    <q-item v-for="t in moveTargets" :key="t.role" clickable v-close-popup :data-test="'mail-move-' + t.role" @click="move(t)"><q-item-section>{{ t.label }}</q-item-section></q-item>
                  </q-list>
                </q-menu>
              </q-btn>
              <q-btn flat round icon="sym_o_delete" aria-label="Delete" data-test="mail-delete" @click="remove" />
            </div>
          </template>
        </article>
        <div v-else class="ph-split__empty"><div><q-icon name="sym_o_mail" size="48px" aria-hidden="true" /><p>{{ mails.length ? 'Pick a message to read it.' : 'No message open.' }}</p></div></div>
      </template>
    </SplitView>

    <!-- composer -->
    <q-dialog v-model="composeOpen" persistent>
      <q-card class="ph-dialog" data-test="mail-compose-form">
        <form novalidate @submit.prevent="send">
          <q-card-section class="ph-formcol">
            <h2>{{ draft.replyTo ? 'Reply' : 'New message' }}</h2>
            <q-select v-if="accounts.length" v-model="draft.account" :options="fromOptions" emit-value map-options outlined label="From" data-test="mail-from"
                      hint="Mail through a connected account goes out through that account's own server and is filed in its Sent folder." />
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

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { hubCall, NO_HUB } from '../cloud'
import type { HubRequest } from '../proto'
import type { MailAccount, MailBody, MailFolder, MailItem } from '../proto'

/** The message being written: recipients, subject, text, the Message-ID it answers, and the account it goes out through. */
interface Draft { rcpt: string; subject: string; body: string; replyTo: string; account: string }
import { toast } from '../toast'
import { HdAvatar, HdPageHeader, HdStatusState } from '@etesian-design/vue'
import SplitView from '../components/SplitView.vue'
import { fillPage } from '../fillPage'
import { useChrome } from '@etesian-design/shell'
import { latest } from '../latest'

const folders = ref<MailFolder[]>([])
const folder = ref('')
const mails = ref<MailItem[]>([])
const total = ref(0)
const loading = ref(false)
const error = ref('')
const open = ref(false)
const reading = ref(false)
const msg = ref<MailBody | null>(null)
const current = ref<MailItem | null>(null)
const formatted = ref(false)
const composeOpen = ref(false)
const sending = ref(false)
const sendError = ref('')
const draft = ref<Draft>({ rcpt: '', subject: '', body: '', replyTo: '', account: '' })
const accounts = ref<MailAccount[]>([]) // connected accounts that can send (Connections)
const fromOptions = computed(() => [{ label: 'Hub address (stays on this server)', value: '' }, ...accounts.value.map((a) => ({ label: `${a.name} <${a.from}>`, value: a.id }))])

const order: Record<string, number> = { inbox: 0, drafts: 1, sent: 2, archive: 3, junk: 4, trash: 5 }
const folderOptions = computed(() => [...folders.value].sort((a, b) => (order[a.role || ''] ?? 9) - (order[b.role || ''] ?? 9) || a.name.localeCompare(b.name))
  .map((f) => ({ label: f.name + (f.unread ? ` (${f.unread} unread)` : ''), value: f.id })))
const role = computed(() => (folders.value.find((f) => f.id === folder.value) || {}).role || '')
const moveTargets = computed(() => [{ role: 'inbox', label: 'Inbox' }, { role: 'archive', label: 'Archive' }, { role: 'junk', label: 'Junk' }, { role: 'trash', label: 'Trash' }].filter((t) => t.role !== role.value && folders.value.some((f) => f.role === t.role)))
// the formatted view: sanitised by the server, shown in a sandboxed frame whose content policy forbids every load
const frameDoc = computed(() => `<!doctype html><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'"><base target="_blank"><style>body{font:15px system-ui,sans-serif;margin:8px;overflow-wrap:anywhere;color:#1a1d21;background:#fff}blockquote{margin:0 0 0 8px;padding-left:8px;border-left:3px solid #ccc}</style>${msg.value ? msg.value.safe : ''}`)

function when(iso: string) {
  const d = new Date(iso)
  return d.toDateString() === new Date().toDateString() ? d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }) : d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
}

async function init() {
  error.value = ''
  try {
    folders.value = (await hubCall({ op: 'mail.folders' })).folders || []
    try { accounts.value = (await hubCall({ op: 'mail.accounts' })).accounts || [] } catch (e) { accounts.value = [] }
    if (!folder.value) folder.value = (folders.value.find((f) => f.role === 'inbox') || folders.value[0] || {}).id || ''
    if (folder.value) await loadList(true)
  } catch (e) {
    error.value = /not set up/i.test((e as Error).message) ? NO_HUB : (e as Error).message
  }
}
// the header: the folders as the title's view menu, then Write and Refresh (most important first; the host folds the rest into ⋮)
const folderName = (id: string) => folderOptions.value.find((f) => f.value === id)?.label ?? 'Mail'
const folderTitle = computed(() => folders.value.find((f) => f.id === folder.value)?.name ?? 'Mail')
const folderSub = computed(() => {
  const f = folders.value.find((x) => x.id === folder.value)
  if (!f) return error.value ? 'Not available' : 'Loading'
  return f.unread ? `${f.unread} unread · ${total.value} messages` : `${total.value} message${total.value === 1 ? '' : 's'}`
})
const chrome = useChrome(() => ({
  title: 'Mail',
  views: folderOptions.value.length ? folderOptions.value.map((f) => f.label) : undefined,
  view: folder.value ? folderName(folder.value) : undefined,
  tools: [{ icon: 'edit', label: 'Write' }, { icon: 'refresh', label: 'Refresh' }],
}))
chrome.on('tool', (l) => { if (l === 'Write') openCompose(); else if (l === 'Refresh') refresh() })
chrome.on('view', (v) => { const f = folderOptions.value.find((o) => o.label === v); if (f && f.value !== folder.value) { folder.value = f.value; loadList(true) } })

async function refresh() {
  try { folders.value = (await hubCall({ op: 'mail.folders' })).folders || folders.value } catch (e) { /* the list shows the error */ }
  await loadList(true)
}

const listLoad = latest()
const mailLoad = latest()
async function loadList(reset: boolean) {
  if (!folder.value) return
  if (!reset && loading.value) return // "Show older" while the list reloads: the offset would be wrong
  const t = listLoad.take()
  loading.value = true
  error.value = ''
  try {
    const r = await hubCall({ op: 'mail.list', folder: folder.value, pos: reset ? 0 : mails.value.length })
    if (!listLoad.is(t)) return // another folder (or a refresh) was asked for meanwhile
    mails.value = reset ? r.mails || [] : [...mails.value, ...(r.mails || [])]
    total.value = r.total || mails.value.length
  } catch (e) {
    if (listLoad.is(t)) error.value = (e as Error).message
  } finally {
    if (listLoad.is(t)) loading.value = false
  }
}

async function openMail(m: MailItem) {
  current.value = m
  open.value = true
  reading.value = true
  formatted.value = false
  msg.value = null
  const t = mailLoad.take()
  try {
    const r = (await hubCall({ op: 'mail.read', id2: m.id })).mail || null
    if (mailLoad.is(t)) msg.value = r // another message opened meanwhile shows its own body, not this one
  } catch (e) {
    if (!mailLoad.is(t)) return
    open.value = false
    error.value = (e as Error).message
  } finally {
    if (mailLoad.is(t)) reading.value = false
  }
}

async function toggleRead() {
  const m = current.value
  if (!m) return
  const toRead = m.unread
  try {
    await hubCall({ op: 'mail.mark', id2: m.id, flag: toRead ? 'read' : 'unread' })
    m.unread = !toRead
    toast(toRead ? 'Marked as read' : 'Marked as unread')
  } catch (e) {
    error.value = (e as Error).message
  }
}

// moving and deleting wait for the length of the Undo toast before they reach the hub
function leave(m: MailItem | null, label: string, req: HubRequest) {
  const action = () => hubCall(req)
  if (!m) return
  open.value = false
  const i = mails.value.findIndex((x) => x.id === m.id)
  const from = folder.value
  mails.value = mails.value.filter((x) => x.id !== m.id)
  toast(label, {
    resume: ['hub.call', req],
    // back where it was, if that folder is still the one shown (another folder's list never gets it)
    undo: () => { if (folder.value !== from || mails.value.some((x) => x.id === m.id)) return; const l = [...mails.value]; l.splice(Math.min(Math.max(i, 0), l.length), 0, m); mails.value = l },
    onExpire: async () => { try { await action(); await refreshFolders() } catch (e) { error.value = (e as Error).message; await loadList(true) } },
  })
}
async function refreshFolders() { try { folders.value = (await hubCall({ op: 'mail.folders' })).folders || folders.value } catch (e) { /* ignore */ } }
// the message is taken when Delete is tapped: the deletion runs after the Undo toast, when another message may be open
function remove() {
  const m = current.value
  if (m) leave(m, role.value === 'trash' ? 'Message deleted for good' : 'Moved to Trash', { op: 'mail.delete', id2: m.id })
}
function move(t: { label: string; role: string }) {
  const m = current.value
  if (m) leave(m, `Moved to ${t.label}`, { op: 'mail.move', id2: m.id, role: t.role })
}

function openCompose(init?: Partial<Draft>) {
  draft.value = { rcpt: '', subject: '', body: '', replyTo: '', account: '', ...init }
  sendError.value = ''
  open.value = false
  composeOpen.value = true
}
function reply() {
  const m = msg.value
  if (!m) return
  const quoted = (m.text || '').split('\n').map((l) => '> ' + l).join('\n')
  // an answer goes out through the account the message came from (its hub folder is named "<account> / <folder>")
  const fname = (folders.value.find((f) => f.id === folder.value) || {}).name || ''
  const acct = accounts.value.find((a) => fname.startsWith(a.name + ' / '))
  openCompose({ account: acct ? acct.id : '', rcpt: m.fromaddr || '', subject: /^re:/i.test(m.subject) ? m.subject : 'Re: ' + m.subject, replyTo: m.msgid || '',
    body: `\n\nOn ${new Date(m.date).toLocaleString()}, ${m.from} wrote:\n${quoted}` })
}
async function send() {
  if (sending.value) return
  const to = draft.value.rcpt.split(',').map((a) => a.trim()).filter(Boolean)
  const bad = to.find((a) => !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(a))
  sendError.value = !to.length ? 'Enter who the message is to.' : bad ? `"${bad}" isn't an email address.` : ''
  if (sendError.value) return
  sending.value = true
  try {
    await hubCall({ op: 'mail.send', rcpt: draft.value.rcpt, subject: draft.value.subject, body: draft.value.body, replyto: draft.value.replyTo || undefined, acct: draft.value.account || undefined })
    composeOpen.value = false
    toast('Message sent')
    await refresh()
  } catch (e) {
    sendError.value = (e as Error).message
  } finally {
    sending.value = false
  }
}

onMounted(init)
</script>

<style scoped>
.ph-mail { display: flex; flex-direction: column; }
.ph-mail__bar { flex: none; display: flex; align-items: center; gap: 8px; padding: 8px 11px; border-bottom: 1px solid var(--hd-c-divider); }
.ph-mail__list { flex: 1; min-height: 0; }
.ph-mail__note { margin: 10px 11px; }
.ph-dot { flex: none; width: 10px; height: 10px; background: var(--hd-accent); -webkit-mask: var(--hd-squircle-mask) center / 100% 100% no-repeat; mask: var(--hd-squircle-mask) center / 100% 100% no-repeat; }
.ph-more { display: flex; justify-content: center; padding: 16px; }
/* the reader (Web/Classic layout, Mail): light subject, the sender with a squircle avatar, the text, the actions */
.ph-mail__reader { flex: 1; min-height: 0; padding: 20px 28px; }
.ph-mail__back { margin: -12px 0 8px -12px; }
.ph-mail__from { display: flex; gap: 11px; align-items: center; margin: 14px 0; font-size: var(--hd-font-size-small); color: var(--hd-c-fg-secondary); }
.ph-mail__from b { display: block; color: var(--hd-c-fg); font: 700 var(--hd-font-size-default)/1.3 var(--hd-font-family-base); }
.ph-mail__actions { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 16px; }
.ph-body { max-width: 68ch; white-space: pre-wrap; overflow-wrap: anywhere; font: 400 var(--ph-text-body)/1.5 var(--ph-font); margin: var(--ph-s3) 0 0; }
.ph-frame { width: 100%; height: 60vh; border: 1px solid var(--ph-divider); border-radius: var(--ph-r-control); background: #fff; }
.ph-dialog { width: min(96vw, 640px); }
.ph-dialog h2 { margin-bottom: var(--ph-s2); overflow-wrap: anywhere; }
.ph-formcol { display: flex; flex-direction: column; gap: var(--ph-s3); }
.ph-err { color: var(--ph-negative); margin: 0; display: flex; gap: var(--ph-s2); }
@container (max-width: 520px) { .ph-mail__reader { padding: 16px; } }
</style>
