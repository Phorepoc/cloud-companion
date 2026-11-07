<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- Backups (owner only, on a board set up by the host manager): every night the board copies all of Cloud Companion's data, encrypted
     with the owner's backup password, to a USB disk, an SFTP server, S3-compatible storage or a restic REST server. A fresh board
     restores it from its setup page. A server set up by hand has no manager: the page says so. -->
<template>
  <q-page class="ph-page ph-bk" data-test="backups">
    <h1>Backups</h1>
    <HdStatusState v-if="!state.admin" icon="lock" title="Only an admin device can change backups" text="Open this page on the first device you connected." />
    <HdStatusState v-else-if="available === null" state="loading" title="Looking for the board" text="This takes a moment." />
    <p v-else-if="!available" class="ph-muted" data-test="backups-by-hand">This server was set up by hand: back up its Docker volumes with
      your own tools. On a board set up from a Cloud Companion image, backups are set up here.</p>
    <template v-else>
      <p class="ph-muted">Every night the board stops Cloud Companion for a minute or two and copies everything (chats, mail, files, features and
        your devices) to the place below, encrypted with your backup password. If the board or its card fails, a fresh board brings it
        all back from its setup page.</p>

      <div class="ph-card ph-bk__status" role="status" data-test="backups-status">
        <div class="ph-bk__title">{{ on ? 'On' : 'Off' }}<template v-if="on">, every day at {{ at }}</template></div>
        <div v-if="last" class="ph-small" :class="last.ok ? 'ph-muted' : 'ph-err'" data-test="backups-last">{{ lastText }}</div>
        <div v-else-if="on" class="ph-small ph-muted">No backup yet.</div>
        <div v-if="job" class="ph-small" :class="job.state === 'failed' ? 'ph-err' : 'ph-muted'" data-test="backups-job">{{ jobText }}</div>
        <div class="ph-bk__actions">
          <q-btn v-if="on" unelevated no-caps color="primary" label="Back up now" :disable="running" data-test="backups-now" @click="runNow" />
          <q-btn v-if="on" flat no-caps label="Turn off" :disable="running" data-test="backups-off" @click="turnOff" />
        </div>
      </div>

      <form class="ph-card ph-formcol ph-bk__form" data-test="backups-form" @submit.prevent="save">
        <h2>{{ on ? 'Change where they go' : 'Turn on' }}</h2>
        <q-select v-model="kind" :options="kinds" emit-value map-options outlined label="Where" data-test="backups-kind" />
        <template v-if="kind === 'usb'">
          <q-select v-model="dest.uuid" :options="diskOptions" emit-value map-options outlined label="USB disk" data-test="backups-disk"
                    :hint="diskOptions.length ? 'Plugged into the board' : 'No USB disk found: plug one in, then pick USB again'" />
          <q-input v-model="dest.path" outlined label="Folder on the disk (empty: the top)" />
        </template>
        <template v-if="kind === 'sftp'">
          <q-input v-model="dest.path" outlined label="user@host:/folder" autocapitalize="off" spellcheck="false" data-test="backups-sftp" />
          <p class="ph-small ph-muted">Add the board's key to the server's <code>~/.ssh/authorized_keys</code> first:</p>
          <code class="ph-bk__key" data-test="backups-key">{{ sshKey }}</code>
        </template>
        <template v-if="kind === 's3' || kind === 'rest'">
          <q-input v-model="dest.url" outlined :label="kind === 's3' ? 'Bucket address (https://s3.example.com/bucket/cloud)' : 'Server address (https://user:password@host:8000/cloud)'"
                   autocapitalize="off" spellcheck="false" data-test="backups-url" />
        </template>
        <template v-if="kind === 's3'">
          <q-input v-model="dest.keyId" outlined label="Access key ID" autocapitalize="off" spellcheck="false" />
          <q-input v-model="dest.secret" outlined type="password" :label="hasPassword ? 'Secret access key (empty: keep)' : 'Secret access key'" />
        </template>
        <q-input v-model="dest.password" outlined type="password" autocomplete="new-password" data-test="backups-password"
                 :label="hasPassword ? 'Backup password (empty: keep)' : 'Backup password (at least 10 characters)'"
                 hint="Keep it somewhere safe: without it nobody, not even you, can read the backups." />
        <q-input v-model="atInput" outlined label="Time of day" mask="##:##" data-test="backups-at" />
        <q-btn unelevated no-caps color="primary" type="submit" :label="on ? 'Save' : 'Turn on'" :disable="running" data-test="backups-save" />
        <p v-if="error" class="ph-err" role="alert" data-test="backups-error">{{ error }}</p>
      </form>
    </template>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { HdStatusState } from '@etesian-design/vue'
import { state, apiCall } from '../cloud'

interface Dest { kind: string; uuid?: string; path?: string; url?: string; keyId?: string; secret?: string; password?: string }
interface Run { at: string; ok: boolean; error?: string; snapshot?: string }
interface Job { id: string; kind: string; state: 'running' | 'done' | 'failed'; error?: string; log: string[] }
interface Disk { uuid: string; label?: string; size: string; fs: string; model?: string }

const kinds = [
  { value: 'usb', label: 'A USB disk plugged into the board' },
  { value: 'sftp', label: 'An SFTP server (a NAS or another computer)' },
  { value: 's3', label: 'S3-compatible storage (Backblaze B2, Wasabi, MinIO…)' },
  { value: 'rest', label: 'A restic REST server' },
]
const available = ref<boolean | null>(null)
const on = ref(false)
const at = ref('03:30')
const atInput = ref('03:30')
const last = ref<Run | null>(null)
const hasPassword = ref(false)
const sshKey = ref('')
const kind = ref('usb')
const dest = reactive<Dest>({ kind: 'usb' })
const disks = ref<Disk[]>([])
const job = ref<Job | null>(null)
const error = ref('')
const running = computed(() => job.value?.state === 'running')
const diskOptions = computed(() => disks.value.map((d) => ({ value: d.uuid, label: [d.label || d.model || 'USB disk', d.size, d.fs].filter(Boolean).join(', ') })))

const lastText = computed(() => {
  const l = last.value
  if (!l) return ''
  const when = new Date(l.at).toLocaleString()
  return l.ok ? `Last backup ${when}.` : `The backup on ${when} failed: ${l.error || 'no reason given'}.`
})
const jobText = computed(() => {
  const j = job.value
  if (!j) return ''
  if (j.state === 'failed') return 'Failed: ' + (j.error || 'the board did not say why')
  if (j.state === 'done') return j.kind === 'backup' ? 'Backed up.' : 'Saved.'
  const l = [...(j.log || [])].reverse().find((x) => x.trim() && !x.startsWith('{') && !x.startsWith('docker '))
  return 'Working on it' + (l ? ': ' + l.trim() : '…')
})

async function loadDisks() {
  try { disks.value = (await apiCall('GET', '/phe/host/backup/usb')).disks || [] } catch { disks.value = [] }
  if (!dest.uuid && disks.value.length) dest.uuid = disks.value[0].uuid
}
watch(kind, (k) => { if (k === 'usb') loadDisks() })

async function load() {
  try {
    const h = await apiCall('GET', '/phe/host')
    available.value = !!h.available
    if (!h.available) return
    const b = await apiCall('GET', '/phe/host/backup')
    on.value = !!b.on
    at.value = atInput.value = b.at || '03:30'
    last.value = b.last || null
    hasPassword.value = !!b.hasPassword
    sshKey.value = b.sshKey || ''
    if (b.dest?.kind) { kind.value = b.dest.kind; Object.assign(dest, b.dest, { password: '', secret: '' }) }
    if (kind.value === 'usb') loadDisks()
  } catch (e) {
    error.value = (e as Error).message
    if (available.value === null) available.value = false
  }
}

// a backup stops Cloud Companion for a minute or two, this page's server included: the job is followed through that gap
async function follow(j: Job) {
  job.value = j
  let away = 0
  while (j.state === 'running') {
    await new Promise((r) => setTimeout(r, 1000))
    try {
      j = await apiCall('GET', '/phe/host/jobs/' + j.id)
      away = 0
    } catch (e) {
      if (++away > 600) throw e // ten minutes without an answer
      job.value = { ...j, log: [...(j.log || []), 'Cloud Companion is stopped for the backup'] }
      continue
    }
    job.value = j
  }
  if (j.state === 'failed') error.value = j.error || ''
  await load()
}

async function save() {
  error.value = ''
  try {
    const j = await apiCall('PUT', '/phe/host/backup', { on: true, at: atInput.value, dest: { ...dest, kind: kind.value } })
    if (j?.id) await follow(j)
  } catch (e) { error.value = (e as Error).message }
}
async function runNow() {
  error.value = ''
  try { await follow(await apiCall('POST', '/phe/host/backup/run')) } catch (e) { error.value = (e as Error).message }
}
async function turnOff() {
  error.value = ''
  try { await apiCall('PUT', '/phe/host/backup', { on: false }); job.value = null; await load() } catch (e) { error.value = (e as Error).message }
}

onMounted(() => { if (state.admin) load() })
</script>

<style scoped>
.ph-bk__status { display: flex; flex-direction: column; gap: var(--ph-s2); padding: var(--ph-s4); margin-top: var(--ph-s3); }
.ph-bk__title { font-weight: 600; }
.ph-bk__actions { display: flex; gap: var(--ph-s2); flex-wrap: wrap; }
.ph-bk__form { padding: var(--ph-s4); margin-top: var(--ph-s3); }
.ph-bk__key { display: block; overflow-wrap: anywhere; font-size: 12px; padding: var(--ph-s2); border-radius: 8px; background: var(--ph-surface-2, rgba(127, 127, 127, 0.12)); }
.ph-err { color: var(--ph-error, #D6222A); }
</style>
