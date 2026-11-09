<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- Browser (owner only): how many tabs Firefox keeps loaded. Tabs are never refused; above this number the background tabs used
     longest ago are unloaded and load again when picked. By default the number follows the browser container's memory. -->
<template>
  <q-page class="ph-page ph-bset" data-test="browser-settings">
    <h1>Browser</h1>
    <HdStatusState v-if="!state.admin" icon="lock" title="Only admin devices can see this page" text="Open this page on an admin device." />
    <HdStatusState v-else-if="info === null" state="loading" title="Connecting to the server" text="One moment…" />
    <HdStatusState v-else-if="!info.configured" state="empty" title="The browser is not set up" text="Start Cloud Companion with docker-compose.browser.yml to browse from here." />
    <template v-else>
      <section class="ph-card ph-bset__list">
        <HdListItem title="Open tabs" :subtitle="mem ? `${mem.open} open, ${mem.loaded} loaded` : 'The browser is starting'" data-test="bset-open" />
        <HdListItem title="Browser memory" :subtitle="mem ? gb(mem.memoryMB) : '…'" data-test="bset-memory" />
        <HdListItem title="Choose automatically" :subtitle="mem ? `${mem.auto} tabs loaded at most, from the memory` : 'From the memory'" data-test="bset-auto">
          <template #trailing><HdSwitch :model-value="auto" aria-label="Choose automatically" data-test="bset-auto-switch" @update:model-value="setAuto" /></template>
        </HdListItem>
        <form v-if="!auto" class="ph-bset__form" @submit.prevent="save">
          <HdTextField v-model="typed" type="number" inputmode="numeric" label="Tabs kept loaded" helper="Tabs beyond this stay open but unload until you pick them." data-test="bset-number" />
          <HdButton type="submit" variant="accent" :disabled="busy" data-test="bset-save">Save</HdButton>
        </form>
      </section>
      <p v-if="error" class="ph-err" role="alert" data-test="bset-error">{{ error }}</p>
    </template>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { HdButton, HdListItem, HdStatusState, HdSwitch, HdTextField } from '@etesian-design/vue'
import { state, apiCall } from '../cloud'
import { toast } from '../toast'

interface Mem { memoryMB: number; auto: number; budget: number; override: number; open: number; loaded: number }
interface Info { configured: boolean; override: number; memory: Mem | null }
const info = ref<Info | null>(null)
const typed = ref('')
const busy = ref(false)
const error = ref('')
const forced = ref<boolean | null>(null) // the switch turned off, before a number is saved
const mem = computed(() => info.value?.memory ?? null)
const auto = computed(() => forced.value ?? !info.value?.override)
const gb = (mb: number) => (mb >= 1024 ? (mb / 1024).toFixed(1) + ' GB' : mb + ' MB')

async function load() {
  try {
    info.value = await apiCall('GET', '/phe/browser/settings')
    if (!typed.value) typed.value = String(info.value?.override || mem.value?.auto || '')
  } catch (e) { error.value = (e as Error).message }
}
async function put(n: number) {
  busy.value = true; error.value = ''
  try { await apiCall('PUT', '/phe/browser/settings', { override: n }); forced.value = null; await load(); return true }
  catch (e) { error.value = (e as Error).message; return false }
  finally { busy.value = false }
}
async function setAuto(on: boolean) {
  if (on) { if (await put(0)) toast('Tabs kept loaded: automatic') }
  else forced.value = false
}
async function save() {
  const n = Math.round(Number(typed.value))
  if (!(n >= 1 && n <= 500)) { error.value = 'Enter a number from 1 to 500.'; return }
  if (await put(n)) toast(`Up to ${n} tabs kept loaded`)
}
// the counts change as tabs open and close in the browser
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => { void load(); timer = setInterval(() => { if (!busy.value) void load() }, 5000) })
onBeforeUnmount(() => clearInterval(timer))
</script>

<style scoped>
.ph-bset__form { display: flex; gap: var(--ph-s2); align-items: flex-start; padding: var(--ph-s2) var(--ph-s3); }
.ph-bset__form > :first-child { flex: 1; }
</style>
