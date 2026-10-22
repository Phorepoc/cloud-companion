<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- The logo mark from the brand token (src/brand.js), drawn in the accent colour. -->
<template>
  <svg :width="size" :height="size" viewBox="0 0 64 64" role="img" :aria-label="brand.name" class="ph-logo">
    <rect width="64" height="64" rx="14" fill="currentColor" />
    <path :d="brand.logoPath" fill="var(--ph-on-accent)" />
  </svg>
</template>

<script setup>
import { brand } from '../brand.js'
defineProps({ size: { type: Number, default: 32 } })
</script>

<style scoped>
.ph-logo { color: var(--ph-accent); flex: none; }
</style>
