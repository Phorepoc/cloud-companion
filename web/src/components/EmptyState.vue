<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- Empty, loading and error states share one layout: an icon, one sentence, at most one action. See STYLE.md. -->
<template>
  <div class="ph-empty" :data-state="state">
    <q-spinner v-if="state === 'loading'" size="40px" color="primary" aria-label="Loading" />
    <q-icon v-else :name="icon" size="48px" :class="state === 'error' ? 'text-negative' : 'ph-muted'" aria-hidden="true" />
    <h2 class="ph-empty-title">{{ title }}</h2>
    <p class="ph-muted ph-empty-text"><slot>{{ text }}</slot></p>
    <slot name="action" />
  </div>
</template>

<script setup lang="ts">
defineProps({
  state: { type: String, default: 'empty' }, // empty | loading | error
  icon: { type: String, default: 'sym_o_inbox' },
  title: { type: String, required: true },
  text: { type: String, default: '' },
})
</script>

<style scoped>
.ph-empty { display: flex; flex-direction: column; align-items: center; text-align: center; gap: var(--ph-s3); padding: var(--ph-s12) var(--ph-s4); max-width: 420px; margin: 0 auto; }
.ph-empty-text { margin: 0; }
</style>
