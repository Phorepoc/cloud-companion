<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- Mail, read-only: folders, a message list and a reader, over the hub (JMAP through the server). Messages are shown as plain text:
     no pictures, links or scripts of a message are ever loaded. -->
<template>
  <q-page class="ph-page">
    <div class="ph-head">
      <h1>Mail</h1>
      <q-btn flat round icon="sym_o_refresh" aria-label="Refresh" data-test="mail-refresh" @click="loadList(true)" />
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
          <span v-if="m.unread" class="ph-dot" aria-label="Unread"></span>
        </button>
      </li>
    </ul>
    <div v-if="mails.length && mails.length < total" class="ph-more">
      <q-btn outline color="primary" label="Show older messages" :loading="loading" data-test="mail-more" @click="loadList(false)" />
    </div>

    <q-dialog v-model="open">
      <q-card class="ph-dialog" data-test="mail-view">
        <q-card-section v-if="reading"><EmptyState state="loading" title="Opening the message" text="" /></q-card-section>
        <q-card-section v-else-if="msg">
          <h2 data-test="mail-view-subject">{{ msg.subject || '(no subject)' }}</h2>
          <p class="ph-small ph-muted q-mb-xs">From {{ msg.from }}<span v-if="msg.date"> on {{ new Date(msg.date).toLocaleString() }}</span></p>
          <p v-if="msg.to && msg.to.length" class="ph-small ph-muted">To {{ msg.to.join(', ') }}</p>
          <p v-if="msg.html" class="ph-small ph-muted">Shown as plain text. Pictures and links in this message are not loaded.</p>
          <pre class="ph-body" data-test="mail-view-body">{{ msg.text }}</pre>
        </q-card-section>
        <q-card-actions align="right"><q-btn flat label="Close" v-close-popup /></q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { hubCall } from '../cloud.js'
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

const order = { inbox: 0, drafts: 1, sent: 2, archive: 3, junk: 4, trash: 5 }
const folderOptions = computed(() => [...folders.value].sort((a, b) => (order[a.role] ?? 9) - (order[b.role] ?? 9) || a.name.localeCompare(b.name))
  .map((f) => ({ label: f.name + (f.unread ? ` (${f.unread} unread)` : ''), value: f.id })))

function when(iso) {
  const d = new Date(iso)
  const now = new Date()
  return d.toDateString() === now.toDateString() ? d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }) : d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
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
  open.value = true
  reading.value = true
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

onMounted(init)
</script>

<style scoped>
.ph-head { display: flex; align-items: center; justify-content: space-between; gap: var(--ph-s3); margin-bottom: var(--ph-s3); }
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
.ph-body { white-space: pre-wrap; overflow-wrap: anywhere; font-family: var(--ph-font); font-size: var(--ph-text-body); margin: var(--ph-s3) 0 0; max-height: 60vh; overflow: auto; }
.ph-note { background: var(--ph-surface-2); color: var(--ph-text); border: 1px solid var(--ph-divider); }
.ph-note.is-error { border-color: var(--ph-negative); }
</style>
