<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="ph-page">
    <h1>Settings</h1>

    <section class="ph-card ph-section" aria-labelledby="set-appearance">
      <h2 id="set-appearance">Appearance</h2>
      <p class="ph-muted">Choose how the app looks. "Match system" follows your device's light or dark setting.</p>
      <q-btn-toggle v-model="choice" no-caps unelevated spread toggle-color="primary" toggle-text-color="on-accent" color="surface-2" text-color="ph"
                    :options="[{ label: 'Match system', value: 'system', icon: 'sym_o_contrast' }, { label: 'Light', value: 'light', icon: 'sym_o_light_mode' }, { label: 'Dark', value: 'dark', icon: 'sym_o_dark_mode' }]"
                    data-test="theme" @update:model-value="applyTheme" />
    </section>

    <section class="ph-card ph-section" aria-labelledby="set-data">
      <h2 id="set-data">Data use</h2>
      <p class="ph-muted">Lower picture quality and grayscale send fewer bytes. Text always stays sharp. This applies to this device only.</p>
      <div id="q-label" class="ph-small">Picture quality: {{ q }}</div>
      <q-slider v-model="q" :min="10" :max="95" :step="5" label color="primary" aria-labelledby="q-label" data-test="quality" />
      <q-toggle v-model="gray" label="Grayscale pictures" data-test="gray" />
      <div class="ph-actions">
        <q-btn unelevated color="primary" label="Save" data-test="apply" @click="apply" />
        <span v-if="saved" class="ph-ok" role="status" data-test="saved"><q-icon name="sym_o_check_circle" size="20px" aria-hidden="true" /> Saved</span>
      </div>
    </section>

    <section class="ph-card ph-section" aria-labelledby="set-device">
      <h2 id="set-device">This browser</h2>
      <p class="ph-muted">Device ID: <span class="ph-mono">{{ state.dev || 'not connected' }}</span></p>
      <q-btn outline color="negative" label="Disconnect this browser" @click="forgetIt" />
    </section>
  </q-page>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { state, api, forget, connect } from '../cloud.js'
import { theme, applyTheme } from '../theme.js'

const router = useRouter()
const choice = ref(theme.value)
const q = ref(state.settings.q)
const gray = ref(state.settings.gray)
const saved = ref(false)
async function apply() {
  try { await connect() } catch (e) { /* ignore */ }
  api.settings(q.value, gray.value)
  saved.value = true
}
async function forgetIt() {
  await forget()
  router.replace('/pair')
}
</script>

<style scoped>
.ph-section { margin-top: var(--ph-s4); display: flex; flex-direction: column; gap: var(--ph-s3); }
.ph-section p { margin: 0; }
.ph-actions { display: flex; align-items: center; gap: var(--ph-s4); }
.ph-ok { display: inline-flex; align-items: center; gap: var(--ph-s1); color: var(--ph-positive); }
.ph-mono { font-family: var(--ph-font-mono); font-size: var(--ph-text-small); overflow-wrap: anywhere; }

</style>
