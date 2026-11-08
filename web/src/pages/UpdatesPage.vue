<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- Updates (owner only, on a board set up by the host manager): which version runs, whether a newer one is out (from the
     project's GitHub releases, internal/host/update.go), installing it now or at night after the backup. A failed update puts
     the board back as it was; the page says so in plain words. A server set up by hand updates with git and compose. -->
<template>
  <q-page class="ph-page ph-up" data-test="updates">
    <h1>Updates</h1>
    <HdStatusState v-if="!state.admin" icon="lock" title="Only admin devices can install updates" text="Open this page on an admin device." />
    <HdStatusState v-else-if="available === null" state="loading" title="Connecting to the server" text="One moment…" />
    <p v-else-if="!available" class="ph-muted" data-test="updates-by-hand">This server wasn't installed from a Cloud Companion image.
      Update it with <code>git pull</code> and <code>docker compose pull &amp;&amp; docker compose up -d</code>.</p>
    <template v-else>
      <section class="ph-up__hero" :class="'ph-up__hero--' + health" role="status" data-test="updates-status">
        <div class="ph-up__mark" aria-hidden="true">{{ health === 'new' ? '↓' : health === 'ok' ? '✓' : '!' }}</div>
        <div class="ph-up__text">
          <h2>{{ heroTitle }}</h2>
          <p data-test="updates-text">{{ heroText }}</p>
          <p v-if="jobLine" class="ph-up__job" :class="{ 'ph-err': job?.state === 'failed' }" data-test="updates-job">{{ jobLine }}</p>
          <div class="ph-up__actions">
            <HdButton v-if="info.available" variant="accent" :disabled="running" data-test="updates-now" @click="updateNow">Update now</HdButton>
            <HdButton variant="plain" :disabled="running || checking" data-test="updates-check" @click="check">{{ checking ? 'Checking…' : 'Check for updates' }}</HdButton>
          </div>
        </div>
      </section>

      <section v-if="info.available?.notes" class="ph-card ph-up__notes" data-test="updates-notes">
        <h2>What's new in {{ info.available.version }}</h2>
        <p>{{ info.available.notes }}</p>
      </section>

      <section class="ph-card ph-up__list">
        <HdListItem title="Automatic updates" :subtitle="autoText" data-test="updates-auto">
          <template #trailing><HdSwitch :model-value="info.auto" aria-label="Automatic updates" @update:model-value="setAuto" /></template>
        </HdListItem>
        <HdListItem title="Version" :subtitle="info.version ? 'Cloud Companion ' + info.version : 'Development build'" data-test="updates-version" />
        <HdListItem v-if="info.last?.ok" title="Last update" :subtitle="lastText" data-test="updates-last" />
        <p v-else-if="info.last" class="ph-up__failed" data-test="updates-last"><b>The last update failed.</b> {{ lastText }}</p>
      </section>
      <p v-if="error" class="ph-err" role="alert" data-test="updates-error">{{ error }}</p>
    </template>
  </q-page>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { HdButton, HdListItem, HdStatusState, HdSwitch } from '@etesian-design/vue'
import { state, apiCall } from '../cloud'

interface Release { version: string; notes?: string }
interface Run { at: string; from: string; to: string; ok: boolean; error?: string }
interface Info { version: string; auto: boolean; checked?: string; available?: Release | null; last?: Run | null; error?: string; at?: string; backups?: boolean }
interface Job { id: string; state: 'running' | 'done' | 'failed'; error?: string; log: string[] | null }

const available = ref<boolean | null>(null)
const info = reactive<Info>({ version: '', auto: true })
const job = ref<Job | null>(null)
const error = ref('')
const checking = ref(false)
const running = computed(() => job.value?.state === 'running')

const health = computed(() => (info.available ? 'new' : info.last && !info.last.ok ? 'bad' : info.error ? 'bad' : 'ok'))
const heroTitle = computed(() => (info.available ? `Version ${info.available.version} is available` : info.error ? "Couldn't check for updates" : 'Cloud Companion is up to date'))
function when(iso?: string) {
  if (!iso || iso.startsWith('0001')) return ''
  const t = new Date(iso)
  return 'on ' + t.toLocaleDateString([], { day: 'numeric', month: 'long' }) + ' at ' + t.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}
const night = computed(() => info.at || '03:30')
const heroText = computed(() => {
  if (info.available) {
    const how = info.auto ? `It will be installed tonight at ${night.value}${info.backups ? ', after the backup' : ''}.` : 'Install it whenever suits you.'
    return `${how} Cloud Companion will be unavailable for a few minutes while it restarts. If something goes wrong, the previous version is restored automatically.`
  }
  if (info.error) return info.error + '. It will try again tomorrow.'
  const c = when(info.checked)
  return c ? `Last checked ${c}.` : 'Updates are checked once a day.'
})
const autoText = computed(() => (info.auto ? `Installed at ${night.value}${info.backups ? ', after the backup' : ''}` : 'Off. Install updates from this page.'))
const lastText = computed(() => {
  const l = info.last
  if (!l) return ''
  if (l.ok) return `${l.from || 'Earlier version'} → ${l.to}, ${when(l.at)}`
  return `Version ${l.to} didn't start ${when(l.at)}, so ${l.from} was restored. No data was lost.` + (info.available ? (info.auto ? ' It will try again tonight.' : ' You can try again now.') : '')
})
const jobLine = computed(() => {
  const j = job.value
  if (!j || j.state === 'done') return ''
  if (j.state === 'failed') return j.error || 'Something went wrong.'
  const log = (j.log || []).join('\n')
  if (/starting Cloud Companion v/.test(log)) return 'Starting the new version. This page will reconnect automatically…'
  if (/board files/.test(log)) return 'Downloading…'
  if (/backing up/.test(log)) return 'Backing up first… Cloud Companion is paused for a minute or two.'
  return 'Updating…'
})

function take(r: Info) { Object.assign(info, { available: null, last: null, error: '', checked: '' }, r) }
async function load() {
  try {
    const h = await apiCall('GET', '/phe/host')
    available.value = !!h.available
    if (h.available) take(await apiCall('GET', '/phe/host/update'))
  } catch (e) {
    error.value = (e as Error).message
    if (available.value === null) available.value = false
  }
}
async function check() {
  checking.value = true
  error.value = ''
  try { take(await apiCall('POST', '/phe/host/update/check')) } catch (e) { error.value = (e as Error).message } finally { checking.value = false }
}
async function setAuto(on: boolean) {
  error.value = ''
  try { take(await apiCall('PUT', '/phe/host/update', { auto: on })) } catch (e) { error.value = (e as Error).message }
}
// an update restarts Cloud Companion and maybe the manager: the job is followed through that gap, then the page reloads its state
async function updateNow() {
  error.value = ''
  try {
    let j: Job = await apiCall('POST', '/phe/host/update')
    job.value = j
    let away = 0
    while (j.state === 'running') {
      await new Promise((r) => setTimeout(r, 1000))
      try {
        j = await apiCall('GET', '/phe/host/jobs/' + j.id)
        away = 0
      } catch (e) {
        // a new manager has no record of the job: the update's result is in its state
        if ((e as Error).message.includes('no such job')) break
        if (++away > 900) throw e
        continue
      }
      job.value = j
    }
    if (j.state !== 'running') job.value = j
    await load()
    if (j.state === 'running') job.value = null
  } catch (e) { error.value = (e as Error).message }
}

// the page can open before the connection says this device is the owner (a reload, a link): load once it does
watch(() => state.admin, (a) => { if (a && available.value === null) load() }, { immediate: true })
</script>

<style scoped>
.ph-up h2 { font-size: 1.1rem; margin: 0 0 var(--ph-s2); }
.ph-up__hero { display: flex; gap: var(--ph-s4); align-items: flex-start; padding: var(--ph-s4); margin: var(--ph-s3) 0; border-radius: 16px; background: var(--ph-surface, rgba(127, 127, 127, 0.08)); }
.ph-up__hero p { margin: 0 0 var(--ph-s3); }
.ph-up__text { flex: 1; min-width: 0; }
.ph-up__mark { flex: none; width: 44px; height: 44px; border-radius: 30%; display: grid; place-items: center; font-size: 22px; font-weight: 700; color: #fff; background: #3E8E1E; }
.ph-up__hero--new .ph-up__mark { background: #1C51AF; }
.ph-up__hero--bad .ph-up__mark { background: #D6222A; }
.ph-up__job { color: var(--ph-text-muted); }
.ph-up__actions { display: flex; gap: var(--ph-s2); flex-wrap: wrap; }
.ph-up__notes { padding: var(--ph-s4); margin-bottom: var(--ph-s3); }
.ph-up__notes p { margin: 0; white-space: pre-line; }
.ph-up__list { padding: var(--ph-s2) 0; }
.ph-up__failed { margin: var(--ph-s2) var(--ph-s4) var(--ph-s3); color: var(--ph-text-muted); }
.ph-up__failed b { color: var(--ph-error, #D6222A); }
.ph-err { color: var(--ph-error, #D6222A); }
</style>
