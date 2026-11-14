<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: AGPL-3.0-only -->
<!-- Shows the toasts from toast.ts in the Etesian toast stack: bottom centre, above the phone's bottom bar, paused while hovered or focused. -->
<template>
  <HdToastStack class="ph-toasts" :toasts="shown" data-test="toasts" @close="(id, undo) => closeToast(Number(id), undo)" />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { HdToastStack } from '@etesian-design/vue'
import { toasts, closeToast } from '../toast'

const shown = computed(() => toasts.map((t) => ({ id: t.id, text: t.message, action: t.undo ? 'Undo' : undefined, timeout: t.ms })))
</script>

<style scoped>
.ph-toasts { z-index: 6000; }
</style>
