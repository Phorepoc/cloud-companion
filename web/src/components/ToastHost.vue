<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- Shows the toasts from toast.ts: bottom centre, above the phone's bottom bar, 4 seconds (paused while hovered or focused). -->
<template>
  <div class="ph-toasts" role="status" aria-live="polite" data-test="toasts">
    <div v-for="t in toasts" :key="t.id" class="ph-toast" :data-test="'toast'" @mouseenter="pause(t)" @mouseleave="resume(t)" @focusin="pause(t)" @focusout="resume(t)">
      <span class="ph-toast-text">{{ t.message }}</span>
      <button v-if="t.undo" type="button" class="ph-toast-btn" data-test="toast-undo" @click="closeToast(t.id, true)">Undo</button>
      <button type="button" class="ph-toast-btn ph-toast-x" aria-label="Dismiss" @click="closeToast(t.id)"><q-icon name="sym_o_close" size="20px" aria-hidden="true" /></button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { watch } from 'vue'
import { toasts, closeToast, type Toast } from '../toast'

const timers = new Map()
function start(t: Toast) { timers.set(t.id, setTimeout(() => closeToast(t.id), t.ms)) }
function pause(t: Toast) { clearTimeout(timers.get(t.id)) }
function resume(t: Toast) { if (toasts.some((x) => x.id === t.id)) start(t) }
watch(() => toasts.length, () => {
  for (const t of toasts) if (!timers.has(t.id)) start(t)
}, { flush: 'post' })
</script>

<style scoped>
.ph-toasts { position: fixed; left: 0; right: 0; bottom: calc(var(--ph-s4) + env(safe-area-inset-bottom)); z-index: 6000; display: flex; flex-direction: column; align-items: center; gap: var(--ph-s2); pointer-events: none; }
@media (max-width: 599px) { .ph-toasts { bottom: calc(72px + env(safe-area-inset-bottom)); } }
.ph-toast { pointer-events: auto; display: flex; align-items: center; gap: var(--ph-s2); min-height: var(--ph-tap); max-width: min(92vw, 480px); padding: 0 var(--ph-s2) 0 var(--ph-s4);
  background: var(--ph-text); color: var(--ph-bg); border-radius: var(--ph-r-control); box-shadow: var(--ph-elev-3); }
.ph-toast-text { flex: 1; }
.ph-toast-btn { min-width: var(--ph-tap); min-height: var(--ph-tap); background: none; border: 0; color: var(--ph-bg); font: inherit; font-weight: 600; cursor: pointer; text-decoration: underline; border-radius: var(--ph-r-control); }
.ph-toast-x { text-decoration: none; display: grid; place-items: center; }
</style>
