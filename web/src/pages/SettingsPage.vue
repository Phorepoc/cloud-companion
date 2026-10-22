<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="q-pa-md" style="max-width:520px">
    <div class="text-h6 q-mb-md">Settings</div>
    <div class="text-subtitle2">Byte budget (this device)</div>
    <div class="text-caption text-grey-8 q-mb-sm">Lower JPEG quality and grayscale reduce the data per photo tile; text tiles stay lossless.</div>
    <q-slider v-model="q" :min="10" :max="95" :step="5" label label-always color="primary" data-test="quality" />
    <q-toggle v-model="gray" label="Grayscale tiles" data-test="gray" />
    <div class="q-mt-md"><q-btn color="primary" label="Apply" data-test="apply" @click="apply" /> <span v-if="saved" class="text-positive q-ml-sm" data-test="saved">applied</span></div>
    <q-separator class="q-my-lg" />
    <div class="text-subtitle2">This browser</div>
    <div class="text-caption">Device id {{ state.dev || '(not connected)' }}</div>
    <q-btn flat color="negative" label="Forget this device" class="q-mt-sm" @click="forgetIt" />
  </q-page>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { state, api, forget, connect } from '../cloud.js'

const router = useRouter()
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
