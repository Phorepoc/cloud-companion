<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- Features (owner only): on a board set up by the host manager (docs/host-manager.md), the parts of Cloud Companion that run as their
     own containers, each with a switch. Switching asks for the feature's settings if it has any, then shows the manager's progress
     until it is running or put back. A server set up by hand has no manager: the page says how its features are chosen there. -->
<template>
  <q-page class="ph-page ph-feat" data-test="features">
    <h1>Features</h1>
    <HdStatusState v-if="!state.admin" icon="lock" title="Only an admin device can change features" text="Open this page on the first device you connected." />
    <HdStatusState v-else-if="available === null" state="loading" title="Looking for the board" text="This takes a moment." />
    <template v-else-if="!available">
      <p class="ph-muted" data-test="features-by-hand">This server was set up by hand, so its features are chosen in its compose files (see the README, "Optional
        services"). On a board set up from a Cloud Companion image they are switched on and off here.</p>
    </template>
    <template v-else>
      <p class="ph-muted" data-test="features-board">{{ boardText }}</p>
      <p v-if="loadError" class="ph-err" role="alert">{{ loadError }}</p>
      <ul class="ph-feat__list ph-card" data-test="features-list">
        <li v-for="f in features" :key="f.id" class="ph-feat__row" :data-test="'feature-' + f.id">
          <div class="ph-feat__main">
            <div class="ph-feat__title">{{ f.title }}</div>
            <div class="ph-small ph-muted">{{ f.about }}</div>
            <div v-if="note(f)" class="ph-small ph-feat__note" :data-test="'feature-note-' + f.id">{{ note(f) }}</div>
            <div v-if="jobs[f.id]" class="ph-small" :class="jobs[f.id].state === 'failed' ? 'ph-err' : 'ph-muted'" role="status" :data-test="'feature-job-' + f.id">
              {{ jobText(jobs[f.id]) }}
            </div>
          </div>
          <q-toggle :model-value="f.on" :disable="!canSwitch(f)" :aria-label="(f.on ? 'Switch off ' : 'Switch on ') + f.title"
                    :data-test="'feature-switch-' + f.id" @update:model-value="(v: boolean) => flip(f, v)" />
        </li>
      </ul>
    </template>

    <q-dialog v-model="asking">
      <q-card class="ph-dialog" data-test="feature-settings">
        <form @submit.prevent="confirmSettings">
          <q-card-section class="ph-formcol">
            <h2>{{ askFor?.title }}</h2>
            <q-input v-for="s in askSettings" :key="s.key" v-model="values[s.key]" :label="s.label" outlined :type="s.secret ? 'password' : 'text'"
                     :data-test="'feature-setting-' + s.key" />
          </q-card-section>
          <q-card-actions align="right">
            <q-btn flat no-caps label="Cancel" @click="asking = false" />
            <q-btn unelevated no-caps color="primary" label="Switch on" type="submit" data-test="feature-settings-ok" />
          </q-card-actions>
        </form>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { HdStatusState } from '@etesian-design/vue'
import { state, apiCall } from '../cloud'

interface Setting { key: string; label: string; generate?: boolean; secret?: boolean; required?: boolean }
interface Feature { id: string; title: string; about: string; core?: boolean; on: boolean; fits: boolean; why?: string; requiredBy?: string[]; settings?: Setting[]; values?: Record<string, string>; needs?: { memory?: string } }
interface Job { id: string; state: 'running' | 'done' | 'failed'; error?: string; log: string[] }
interface Board { model?: string; arch?: string; memory?: number }

const available = ref<boolean | null>(null)
const board = ref<Board>({})
const features = ref<Feature[]>([])
const loadError = ref('')
const jobs = reactive<Record<string, Job>>({})
const busy = computed(() => Object.values(jobs).some((j) => j.state === 'running'))

const boardText = computed(() => {
  const b = board.value
  const mem = b.memory ? ` with ${Math.round(b.memory / 2 ** 30)} GB of memory` : ''
  return `${b.model || 'This server'}${mem}. Switching a feature on downloads and starts it; switching it off stops it and keeps its data.`
})

const titleOf = (id: string) => features.value.find((f) => f.id === id)?.title || id
function note(f: Feature) {
  if (f.core) return 'Always on'
  if (!f.fits) return f.why || 'Not available on this board'
  if (f.on && f.requiredBy?.length) return 'Needed by ' + f.requiredBy.map(titleOf).join(', ')
  return ''
}
function canSwitch(f: Feature) {
  return !busy.value && !f.core && (f.on ? !f.requiredBy?.length : f.fits)
}
function jobText(j: Job) {
  if (j.state === 'failed') return 'Could not switch: ' + (j.error || 'the board did not say why') + '. Everything is as it was.'
  if (j.state === 'done') return ''
  const last = [...(j.log || [])].reverse().find((l) => l.trim() && !l.startsWith('docker '))
  return 'Working on it' + (last ? ': ' + last.trim() : '…')
}

async function load() {
  try {
    const h = await apiCall('GET', '/phe/host')
    available.value = !!h.available
    board.value = h.system?.board || {}
    if (!h.available) return
    const fs: Feature[] = (await apiCall('GET', '/phe/host/features')).features || []
    features.value = fs.sort((a, b) => Number(!!b.core) - Number(!!a.core) || a.title.localeCompare(b.title)) // Cloud Companion first
    loadError.value = ''
  } catch (e) {
    loadError.value = (e as Error).message
    if (available.value === null) available.value = false
  }
}

const asking = ref(false)
const askFor = ref<Feature | null>(null)
const values = reactive<Record<string, string>>({})
const askSettings = computed(() => (askFor.value?.settings || []).filter((s) => !s.generate))

function flip(f: Feature, on: boolean) {
  if (!on) return run(f, 'DELETE')
  if ((f.settings || []).some((s) => !s.generate)) {
    askFor.value = f
    for (const k of Object.keys(values)) delete values[k]
    Object.assign(values, f.values || {})
    asking.value = true
    return
  }
  run(f, 'POST', {})
}
function confirmSettings() {
  asking.value = false
  if (askFor.value) run(askFor.value, 'POST', { ...values })
}

async function run(f: Feature, method: 'POST' | 'DELETE', settings?: Record<string, string>) {
  try {
    let j: Job = await apiCall(method, '/phe/host/features/' + f.id, method === 'POST' ? { settings } : undefined)
    jobs[f.id] = j
    while (j.state === 'running') {
      await new Promise((r) => setTimeout(r, 1000))
      j = await apiCall('GET', '/phe/host/jobs/' + j.id)
      jobs[f.id] = j
    }
  } catch (e) {
    jobs[f.id] = { id: '', state: 'failed', error: (e as Error).message, log: [] }
  }
  await load()
}

onMounted(() => { if (state.admin) load() })
</script>

<style scoped>
.ph-feat__list { list-style: none; margin: var(--ph-s3) 0 0; padding: 0; }
.ph-feat__row { display: flex; align-items: center; gap: var(--ph-s3); padding: var(--ph-s3) var(--ph-s4); }
.ph-feat__row + .ph-feat__row { border-top: 1px solid var(--ph-divider, rgba(127, 127, 127, 0.2)); }
.ph-feat__main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.ph-feat__title { font-weight: 600; }
.ph-feat__note { color: var(--ph-text-muted); font-style: italic; }
.ph-err { color: var(--ph-error, #D6222A); }
</style>
