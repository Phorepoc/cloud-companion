<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- Settings, "Pair a new device": makes a single-use pairing code on the server (POST /phe/pair/code, admin devices only) and shows it large, with a live countdown to its expiry and the
     server certificate fingerprint the new device can check. The same codes `phecloud paircode` prints. -->
<template>
  <section class="ph-card ph-pair" aria-labelledby="set-pair" data-test="pair-device">
    <h2 id="set-pair">Pair a new device</h2>
    <p v-if="!state.admin" class="ph-muted" data-test="pair-denied">
      <q-icon name="sym_o_lock" size="20px" aria-hidden="true" /> Only an admin device can pair new devices. Sign in on an admin device (or use <span class="ph-mono">phecloud paircode</span> on the server).
    </p>
    <template v-else>
      <p class="ph-muted">Make a one-time code, then enter it on the new device: the pairing screen of the app or the phone. It works once and expires in 10 minutes.</p>
      <q-btn-toggle v-model="kind" no-caps unelevated spread toggle-color="primary" toggle-text-color="on-accent" color="surface-2" text-color="ph" aria-label="What kind of device"
                    :options="[{ label: 'Normal device', value: 'normal', icon: 'sym_o_smartphone' }, { label: 'Admin device', value: 'admin', icon: 'sym_o_admin_panel_settings' }]" data-test="pair-kind" />
      <p v-if="kind === 'admin'" class="ph-note" role="note" data-test="pair-admin-note">
        <q-icon name="sym_o_info" size="20px" aria-hidden="true" /> An admin device can also pair and sign out other devices and change these settings. Give it only to a device you own.
      </p>
      <div v-if="!info" class="ph-actions">
        <q-btn unelevated color="primary" icon="sym_o_add_link" label="Generate a pairing code" :loading="busy" data-test="pair-generate" @click="generate" />
      </div>

      <div v-else class="ph-result" data-test="pair-result">
        <div class="ph-codeline">
          <span class="ph-bigcode" :class="{ 'is-expired': expired }" data-test="pair-code" :aria-label="'Pairing code ' + info.code.split('').join(' ')">{{ info.code }}</span>
          <q-btn flat round icon="sym_o_content_copy" :disable="expired" aria-label="Copy the pairing code" data-test="pair-copy" @click="copy(info.code, 'code')" />
        </div>
        <p class="ph-small" :class="expired ? 'ph-error' : 'ph-muted'" role="timer" data-test="pair-countdown">
          <q-icon :name="expired ? 'sym_o_timer_off' : 'sym_o_timer'" size="20px" aria-hidden="true" />
          <template v-if="expired">This code has expired. Generate another.</template>
          <template v-else>Expires in <strong>{{ clock }}</strong> · {{ info.admin ? 'admin device' : 'normal device' }} · works once</template>
        </p>
        <div v-if="info.fingerprint" class="ph-fp">
          <div class="ph-small ph-muted" id="pair-fp-label">Server certificate fingerprint (SHA-256). The new device can check that it is talking to this server.</div>
          <div class="ph-codeline">
            <span class="ph-mono" aria-labelledby="pair-fp-label" data-test="pair-fingerprint">{{ grouped(info.fingerprint) }}</span>
            <q-btn flat round icon="sym_o_content_copy" aria-label="Copy the fingerprint" data-test="pair-copy-fp" @click="copy(info.fingerprint, 'fingerprint')" />
          </div>
        </div>
        <p v-if="copied" class="ph-ok ph-small" role="status" data-test="pair-copied"><q-icon name="sym_o_check_circle" size="20px" aria-hidden="true" /> Copied the {{ copied }}.</p>
        <div class="ph-actions">
          <q-btn :outline="!expired" :unelevated="expired" color="primary" icon="sym_o_autorenew" label="Generate another" :loading="busy" data-test="pair-another" @click="generate" />
        </div>
      </div>
      <p v-if="error" class="ph-error" role="alert" data-test="pair-new-error"><q-icon name="sym_o_error" size="20px" aria-hidden="true" /> {{ error }}</p>
    </template>
  </section>
</template>

<script setup lang="ts">
import { ref, computed, onBeforeUnmount } from 'vue'
import { state, pairCodeNew } from '../cloud'
import type { PairCodeInfo } from '../proto'

const kind = ref<'normal' | 'admin'>('normal')
const info = ref<PairCodeInfo | null>(null)
const busy = ref(false)
const error = ref('')
const copied = ref('')
const now = ref(Date.now())
let timer = 0
let copiedTimer = 0

const left = computed(() => (info.value ? Math.max(0, info.value.expires - Math.floor(now.value / 1000)) : 0))
const expired = computed(() => !!info.value && left.value <= 0)
const clock = computed(() => `${Math.floor(left.value / 60)}:${String(left.value % 60).padStart(2, '0')}`)
/** Groups of 8 hex digits, easier to compare by eye. */
const grouped = (h: string) => h.replace(/(.{8})/g, '$1 ').trim()

async function generate() {
  busy.value = true
  error.value = ''
  copied.value = ''
  try {
    info.value = await pairCodeNew(kind.value === 'admin')
    now.value = Date.now()
    window.clearInterval(timer)
    timer = window.setInterval(() => { now.value = Date.now(); if (left.value <= 0) window.clearInterval(timer) }, 1000)
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    busy.value = false
  }
}

async function copy(text: string, what: string) {
  try {
    await navigator.clipboard.writeText(text)
    copied.value = what
  } catch {
    error.value = 'This browser did not allow copying: select the text and copy it yourself.'
    return
  }
  window.clearTimeout(copiedTimer)
  copiedTimer = window.setTimeout(() => { copied.value = '' }, 3000)
}

onBeforeUnmount(() => { window.clearInterval(timer); window.clearTimeout(copiedTimer) })
</script>

<style scoped>
.ph-pair { margin-top: var(--ph-s4); display: flex; flex-direction: column; gap: var(--ph-s3); }
.ph-pair p { margin: 0; }
.ph-result { display: flex; flex-direction: column; gap: var(--ph-s3); }
.ph-codeline { display: flex; align-items: center; gap: var(--ph-s2); }
.ph-bigcode { font-family: var(--ph-font-mono, ui-monospace, Menlo, Consolas, monospace); font-size: 2rem; font-weight: 600; letter-spacing: 0.08em; padding: var(--ph-s2) var(--ph-s3); border-radius: var(--ph-r-control); background: var(--ph-surface-2, var(--ph-bg)); user-select: all; }
.ph-bigcode.is-expired { opacity: 0.45; text-decoration: line-through; }
.ph-mono { font-family: var(--ph-font-mono); font-size: var(--ph-text-small); overflow-wrap: anywhere; user-select: all; }
.ph-note { display: flex; gap: var(--ph-s2); align-items: flex-start; padding: var(--ph-s3); border-radius: var(--ph-r-control); background: var(--ph-surface-2, var(--ph-bg)); }
.ph-error { display: flex; gap: var(--ph-s2); align-items: flex-start; color: var(--ph-negative); }
.ph-ok { display: inline-flex; align-items: center; gap: var(--ph-s1); color: var(--ph-positive); }
.ph-actions { display: flex; align-items: center; gap: var(--ph-s4); }
@media (max-width: 599px) { .ph-bigcode { font-size: 1.5rem; } }
</style>
