<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- Backups (owner only, on a board set up by the host manager). For people who are not technical: at the top, in plain words,
     whether their data is safe; turning backups on is three questions (where, the details of that place, a password); once on,
     a short list (where, when, how to restore, turn off). Underneath it is restic (internal/host/backup.go); its words stay out
     of the page except for the expert choice. A server set up by hand has no manager: the page says so. -->
<template>
  <q-page class="ph-page ph-bk" data-test="backups">
    <h1>Backups</h1>
    <HdStatusState v-if="!state.admin" icon="lock" title="Only admin devices can change backups" text="Open this page on an admin device." />
    <HdStatusState v-else-if="available === null" state="loading" title="Connecting to the server" text="One moment…" />
    <p v-else-if="!available" class="ph-muted" data-test="backups-by-hand">This server wasn't installed from a Cloud Companion image, so
      backups can't be managed here. Back up its Docker volumes with your own tools.</p>
    <template v-else>
      <section class="ph-bk__hero" :class="'ph-bk__hero--' + health" role="status" data-test="backups-status">
        <div class="ph-bk__mark" aria-hidden="true">{{ health === 'ok' ? '✓' : '!' }}</div>
        <div class="ph-bk__herotext">
          <h2>{{ heroTitle }}</h2>
          <p data-test="backups-last">{{ heroText }}</p>
          <p v-if="jobLine" class="ph-bk__job" :class="{ 'ph-err': job?.state === 'failed' }" data-test="backups-job">{{ jobLine }}</p>
          <HdButton v-if="on && !editing" variant="accent" :disabled="running" data-test="backups-now" @click="runNow">Back up now</HdButton>
        </div>
      </section>

      <!-- turning on, or changing where they go: three questions -->
      <form v-if="!on || editing" class="ph-bk__steps" data-test="backups-form" @submit.prevent="save">
        <section class="ph-card ph-bk__step">
          <h2><span class="ph-bk__num">1</span> Choose where to save backups</h2>
          <div class="ph-bk__choices" role="radiogroup" aria-label="Backup location">
            <label v-for="c in shownKinds" :key="c.value" class="ph-bk__choice" :class="{ 'ph-bk__choice--on': kind === c.value }" :data-test="'backups-kind-' + c.value">
              <HdRadioButton v-model="kind" :value="c.value" name="bk-kind" />
              <span><b>{{ c.title }}</b><small>{{ c.text }}</small></span>
            </label>
          </div>
          <button v-if="!moreKinds" type="button" class="ph-bk__more" data-test="backups-more" @click="moreKinds = true">Show more options</button>
        </section>

        <section class="ph-card ph-bk__step">
          <h2><span class="ph-bk__num">2</span> {{ stepTwoTitle }}</h2>
          <template v-if="kind === 'usb'">
            <div v-if="disks.length" class="ph-bk__choices" role="radiogroup" aria-label="USB drive">
              <label v-for="d in disks" :key="d.uuid" class="ph-bk__choice" :class="{ 'ph-bk__choice--on': usb === d.uuid }" data-test="backups-disk">
                <HdRadioButton v-model="usb" :value="d.uuid" name="bk-disk" />
                <span><b>{{ d.label || d.model || 'USB drive' }}</b><small>{{ d.size }}</small></span>
              </label>
            </div>
            <p v-else class="ph-muted">No USB drive found. Plug one into the server and select Refresh. Nothing on the drive is
              deleted.</p>
            <HdButton variant="plain" size="small" @click="loadDisks">Refresh</HdButton>
          </template>
          <template v-else-if="kind === 'sftp'">
            <HdTextField v-model="sftp.host" label="Hostname or IP address" placeholder="nas.local" autocomplete="off" data-test="backups-sftp-host" />
            <HdTextField v-model="sftp.user" label="Username" autocomplete="off" data-test="backups-sftp-user" />
            <HdTextField v-model="sftp.folder" label="Folder" helper="Created if it doesn't exist." data-test="backups-sftp-folder" />
            <div class="ph-bk__key">
              <p><b>Allow this server to connect.</b> Add this key to <code>~/.ssh/authorized_keys</code> on that computer. On a NAS, look
                for SSH keys in its settings.</p>
              <code data-test="backups-key">{{ sshKey }}</code>
              <HdButton variant="plain" size="small" @click="copyKey">{{ copied ? 'Copied' : 'Copy key' }}</HdButton>
            </div>
          </template>
          <template v-else-if="kind === 's3'">
            <HdTextField v-model="s3.endpoint" label="Endpoint" placeholder="s3.eu-central-003.backblazeb2.com" autocomplete="off"
                         helper="Your storage provider shows this next to the bucket." data-test="backups-s3-endpoint" />
            <HdTextField v-model="s3.bucket" label="Bucket" autocomplete="off" data-test="backups-s3-bucket" />
            <HdTextField v-model="s3.keyId" label="Access key ID" autocomplete="off" data-test="backups-s3-key" />
            <HdTextField v-model="s3.secret" label="Secret access key" type="password" autocomplete="off" :helper="editing ? 'Leave empty to keep the current key.' : ''" data-test="backups-s3-secret" />
          </template>
          <template v-else>
            <HdTextField v-model="rest" label="Server URL" placeholder="https://backup.example.com:8000/cloud" autocomplete="off"
                         helper="If the server needs a username and password, include them in the URL." data-test="backups-url" />
          </template>
        </section>

        <section class="ph-card ph-bk__step">
          <h2><span class="ph-bk__num">3</span> {{ editing ? 'Backup password' : 'Set a backup password' }}</h2>
          <p class="ph-muted">Backups are encrypted with this password. Keep it somewhere safe. Without it, nobody can restore your data,
            not even you.</p>
          <HdTextField v-model="pass1" type="password" autocomplete="new-password" label="Password"
                       :helper="editing ? 'Leave empty to keep the current password.' : 'At least 10 characters.'" data-test="backups-password" />
          <HdTextField v-model="pass2" type="password" autocomplete="new-password" label="Confirm password" data-test="backups-password2" />
        </section>

        <p v-if="error" class="ph-err" role="alert" data-test="backups-error">{{ error }}</p>
        <div class="ph-bk__actions">
          <HdButton type="submit" variant="accent" :disabled="running" data-test="backups-save">{{ editing ? 'Save' : 'Turn on backups' }}</HdButton>
          <HdButton v-if="editing" variant="plain" :disabled="running" @click="editing = false; error = ''">Cancel</HdButton>
        </div>
      </form>

      <!-- on: what is set, in a short list -->
      <section v-else class="ph-card ph-bk__list" data-test="backups-settings">
        <HdListItem title="Location" :subtitle="whereText" drilldown clickable data-test="backups-where" @click="startEdit" />
        <HdListItem title="Schedule" :subtitle="'Every night at ' + at" drilldown clickable data-test="backups-when" @click="editTime = !editTime" />
        <div v-if="editTime" class="ph-bk__time">
          <HdTextField v-model="atInput" type="time" label="Backup time" data-test="backups-at" />
          <HdButton variant="accent" size="small" :disabled="running" @click="saveTime">Save</HdButton>
        </div>
        <p class="ph-bk__restore"><b>Restoring</b><br>To restore your data, set up a new server and choose “Restore from a backup”.</p>
        <div class="ph-bk__off">
          <HdButton variant="negative" size="small" :disabled="running" data-test="backups-off" @click="confirmOff = true">Turn off backups</HdButton>
        </div>
        <p v-if="error" class="ph-err" role="alert" data-test="backups-error">{{ error }}</p>
      </section>

      <q-dialog v-model="confirmOff">
        <q-card class="ph-dialog">
          <q-card-section>
            <h2>Turn off backups?</h2>
            <p>Existing backups are kept, but no new ones will be made.</p>
          </q-card-section>
          <q-card-actions align="right">
            <HdButton variant="plain" @click="confirmOff = false">Keep on</HdButton>
            <HdButton variant="negative" data-test="backups-off-confirm" @click="turnOff">Turn off</HdButton>
          </q-card-actions>
        </q-card>
      </q-dialog>
    </template>
  </q-page>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { HdButton, HdListItem, HdRadioButton, HdStatusState, HdTextField } from '@etesian-design/vue'
import { state, apiCall } from '../cloud'

interface Dest { kind: string; uuid?: string; path?: string; url?: string; keyId?: string }
interface Run { at: string; ok: boolean; error?: string; snapshot?: string }
interface Job { id: string; kind: string; state: 'running' | 'done' | 'failed'; error?: string; log: string[] | null }
interface Disk { uuid: string; label?: string; size: string; fs: string; model?: string }

const KINDS = [
  { value: 'usb', title: 'USB drive', text: 'Plugged into this server. The simplest option.' },
  { value: 'sftp', title: 'Computer or NAS', text: 'Another computer on your home network.' },
  { value: 's3', title: 'Cloud storage', text: 'Backblaze B2, Wasabi or any S3-compatible service. Your data stays safe even if something happens at home.' },
  { value: 'rest', title: 'restic REST server', text: 'If you already run one.' },
]
const available = ref<boolean | null>(null)
const on = ref(false)
const at = ref('03:30')
const atInput = ref('03:30')
const last = ref<Run | null>(null)
const saved = ref<Dest | null>(null)
const sshKey = ref('')
const disks = ref<Disk[]>([])
const job = ref<Job | null>(null)
const error = ref('')
const editing = ref(false)
const editTime = ref(false)
const confirmOff = ref(false)
const moreKinds = ref(false)
const copied = ref(false)
const running = computed(() => job.value?.state === 'running')

// the form
const kind = ref('usb')
const usb = ref('')
const sftp = reactive({ host: '', user: '', folder: 'cloud-backups' })
const s3 = reactive({ endpoint: '', bucket: '', keyId: '', secret: '' })
const rest = ref('')
const pass1 = ref('')
const pass2 = ref('')
const shownKinds = computed(() => KINDS.filter((k) => k.value !== 'rest' || moreKinds.value || kind.value === 'rest'))
const stepTwoTitle = computed(() => ({ usb: 'Choose a USB drive', sftp: 'Computer details', s3: 'Cloud storage details', rest: 'Server details' } as Record<string, string>)[kind.value])

const health = computed(() => (!on.value ? 'off' : last.value && !last.value.ok ? 'bad' : last.value ? 'ok' : 'wait'))
const heroTitle = computed(() => ({ off: 'Backups are off', bad: 'The last backup failed', ok: 'Your data is backed up', wait: 'Backups are on' } as Record<string, string>)[health.value])
const whereText = computed(() => {
  const d = saved.value
  if (!d) return ''
  if (d.kind === 'usb') {
    const disk = disks.value.find((x) => x.uuid === d.uuid)
    return disk ? `USB drive ${disk.label || disk.model || ''} (${disk.size})`.replace('  ', ' ') : 'USB drive'
  }
  if (d.kind === 'sftp') return (d.path || '').replace(/^[^@]*@/, '').replace(/:.*/, '')
  if (d.kind === 's3') return (d.url || '').replace(/^https?:\/\//, '')
  return 'restic REST server'
})
function when(iso: string) {
  const t = new Date(iso), now = new Date()
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime()
  const diff = Math.round((day(now) - day(t)) / 864e5)
  const hm = t.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  if (diff === 0) return `today at ${hm}`
  if (diff === 1) return `yesterday at ${hm}`
  return t.toLocaleDateString([], { day: 'numeric', month: 'long' }) + ` at ${hm}`
}
const heroText = computed(() => {
  if (!on.value) return 'Your data is only stored on this server. If its memory card or disk fails, everything is lost. Turn on backups to save a copy somewhere else every night.'
  const l = last.value
  if (l && !l.ok) return `Backup failed ${when(l.at)}: ${l.error || 'no reason given'}. Fix the problem, then select Back up now.`
  return `Backups run every night at ${at.value}.` + (l ? ` Last backup ${when(l.at)}.` : ' The first backup runs tonight, or you can back up now.')
})
const jobLine = computed(() => {
  const j = job.value
  if (!j || j.state === 'done') return ''
  if (j.state === 'failed') return j.error || 'Something went wrong.'
  const log = j.log || []
  const paused = log.some((l) => /stopping Cloud Companion/.test(l)) && !log.some((l) => /starting Cloud Companion again/.test(l))
  if (j.kind === 'backup') return paused ? 'Backing up… Cloud Companion is paused for a minute or two.' : 'Backing up…'
  return 'Checking the backup location…'
})

async function loadDisks() {
  try { disks.value = (await apiCall('GET', '/phe/host/backup/usb')).disks || [] } catch { disks.value = [] }
  if (!disks.value.some((d) => d.uuid === usb.value) && disks.value.length) usb.value = disks.value[0].uuid
}
watch(kind, (k) => { if (k === 'usb') loadDisks() })

function fill(d: Dest) {
  kind.value = d.kind
  if (d.kind === 'rest') moreKinds.value = true
  if (d.kind === 'usb') usb.value = d.uuid || ''
  if (d.kind === 'sftp') {
    const m = /^([^@]+)@([^:]+):(.*)$/.exec(d.path || '')
    if (m) Object.assign(sftp, { user: m[1], host: m[2], folder: m[3] })
  }
  if (d.kind === 's3') {
    const m = /^https?:\/\/([^/]+)\/(.+)$/.exec(d.url || '')
    if (m) Object.assign(s3, { endpoint: m[1], bucket: m[2], keyId: d.keyId || '', secret: '' })
  }
  if (d.kind === 'rest') rest.value = d.url || ''
}
function dest() {
  const d: Record<string, string> = { kind: kind.value, password: pass1.value }
  if (kind.value === 'usb') d.uuid = usb.value
  if (kind.value === 'sftp') d.path = `${sftp.user.trim()}@${sftp.host.trim()}:${sftp.folder.trim() || 'cloud-backups'}`
  if (kind.value === 's3') {
    const host = s3.endpoint.trim().replace(/^https?:\/\//, '').replace(/\/$/, '')
    Object.assign(d, { url: `https://${host}/${s3.bucket.trim()}`, keyId: s3.keyId.trim(), secret: s3.secret })
  }
  if (kind.value === 'rest') d.url = rest.value.trim()
  return d
}
// mistakes caught on the page, before the server is asked
function problem() {
  if (kind.value === 'usb' && !usb.value) return 'Plug in a USB drive and select it.'
  if (kind.value === 'sftp' && (!sftp.host.trim() || !sftp.user.trim())) return 'Enter the hostname and your username.'
  if (kind.value === 's3' && (!s3.endpoint.trim() || !s3.bucket.trim() || !s3.keyId.trim() || (!s3.secret && !editing.value))) return 'Fill in all four fields.'
  if (kind.value === 'rest' && !rest.value.trim()) return 'Enter the server URL.'
  if (!(editing.value && !pass1.value) && pass1.value.length < 10) return 'The password must be at least 10 characters.'
  if (pass1.value !== pass2.value) return "The passwords don't match."
  return ''
}

async function load() {
  try {
    const h = await apiCall('GET', '/phe/host')
    available.value = !!h.available
    if (!h.available) return
    const b = await apiCall('GET', '/phe/host/backup')
    on.value = !!b.on
    at.value = atInput.value = b.at || '03:30'
    last.value = b.last || null
    sshKey.value = b.sshKey || ''
    saved.value = b.dest?.kind ? b.dest : null
    if (saved.value) fill(saved.value)
    loadDisks()
  } catch (e) {
    error.value = (e as Error).message
    if (available.value === null) available.value = false
  }
}

// a backup pauses Cloud Companion, this page's server included: the job is followed through that gap
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
      continue
    }
    job.value = j
  }
  if (j.state === 'failed') error.value = j.error || ''
  await load()
  return j.state === 'done'
}

function startEdit() { editing.value = true; pass1.value = pass2.value = ''; error.value = '' }
async function save() {
  error.value = problem()
  if (error.value) return
  try {
    const j = await apiCall('PUT', '/phe/host/backup', { on: true, at: at.value, dest: dest() })
    if (!j?.id || (await follow(j))) { editing.value = false; pass1.value = pass2.value = '' }
  } catch (e) { error.value = (e as Error).message }
}
async function saveTime() {
  error.value = ''
  try {
    const j = await apiCall('PUT', '/phe/host/backup', { on: true, at: atInput.value, dest: { ...saved.value } })
    if (!j?.id || (await follow(j))) editTime.value = false
  } catch (e) { error.value = (e as Error).message }
}
async function runNow() {
  error.value = ''
  try { await follow(await apiCall('POST', '/phe/host/backup/run')) } catch (e) { error.value = (e as Error).message }
}
async function turnOff() {
  confirmOff.value = false
  error.value = ''
  try { await apiCall('PUT', '/phe/host/backup', { on: false }); job.value = null; await load() } catch (e) { error.value = (e as Error).message }
}
async function copyKey() {
  try { await navigator.clipboard.writeText(sshKey.value); copied.value = true; setTimeout(() => (copied.value = false), 2000) } catch { /* the key is on screen to select */ }
}

// the page can open before the connection says this device is the owner (a reload, a link): load once it does
watch(() => state.admin, (a) => { if (a && available.value === null) load() }, { immediate: true })
</script>

<style scoped>
.ph-bk h2 { font-size: 1.1rem; margin: 0 0 var(--ph-s2); }
.ph-bk__hero { display: flex; gap: var(--ph-s4); align-items: flex-start; padding: var(--ph-s4); margin: var(--ph-s3) 0; border-radius: 16px; background: var(--ph-surface, rgba(127, 127, 127, 0.08)); }
.ph-bk__hero p { margin: 0 0 var(--ph-s3); }
.ph-bk__herotext { flex: 1; min-width: 0; }
.ph-bk__mark { flex: none; width: 44px; height: 44px; border-radius: 30%; display: grid; place-items: center; font-size: 22px; font-weight: 700; color: #fff; background: #8a8f98; }
.ph-bk__hero--ok .ph-bk__mark { background: #3E8E1E; }
.ph-bk__hero--bad .ph-bk__mark, .ph-bk__hero--off .ph-bk__mark { background: #D6222A; }
.ph-bk__hero--wait .ph-bk__mark { background: #1C51AF; }
.ph-bk__job { color: var(--ph-text-muted); }
.ph-bk__steps { display: flex; flex-direction: column; gap: var(--ph-s3); }
.ph-bk__step { display: flex; flex-direction: column; gap: var(--ph-s3); padding: var(--ph-s4); }
.ph-bk__num { display: inline-grid; place-items: center; width: 26px; height: 26px; margin-right: var(--ph-s2); border-radius: 30%; font-size: 14px; color: #fff; background: var(--ph-accent, #1C51AF); }
.ph-bk__choices { display: flex; flex-direction: column; gap: var(--ph-s2); }
.ph-bk__choice { display: flex; gap: var(--ph-s3); align-items: center; padding: var(--ph-s3); border-radius: 14px; border: 1px solid var(--ph-divider, rgba(127, 127, 127, 0.25)); cursor: pointer; }
.ph-bk__choice--on { border-color: var(--ph-accent, #1C51AF); background: color-mix(in srgb, var(--ph-accent, #1C51AF) 8%, transparent); }
.ph-bk__choice span { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.ph-bk__choice small { color: var(--ph-text-muted); line-height: 1.35; }
.ph-bk__more { align-self: flex-start; border: 0; background: none; padding: 0; color: var(--ph-accent, #1C51AF); font: inherit; cursor: pointer; }
.ph-bk__key { display: flex; flex-direction: column; gap: var(--ph-s2); align-items: flex-start; }
.ph-bk__key p { margin: 0; }
.ph-bk__key code { display: block; width: 100%; overflow-wrap: anywhere; font-size: 12px; padding: var(--ph-s3); border-radius: 12px; background: var(--ph-surface-2, rgba(127, 127, 127, 0.12)); }
.ph-bk__actions { display: flex; gap: var(--ph-s2); flex-wrap: wrap; }
.ph-bk__list { padding: var(--ph-s2) 0; }
.ph-bk__time { display: flex; gap: var(--ph-s2); align-items: flex-end; padding: 0 var(--ph-s4) var(--ph-s3); }
.ph-bk__off { padding: var(--ph-s3) var(--ph-s4); }
.ph-bk__restore { margin: var(--ph-s2) var(--ph-s4) 0; color: var(--ph-text-muted); }
.ph-bk__restore b { color: var(--ph-text); }
.ph-bk__list .ph-err { padding: 0 var(--ph-s4); }
.ph-err { color: var(--ph-error, #D6222A); }
</style>
