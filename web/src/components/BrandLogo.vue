<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: AGPL-3.0-only -->
<!-- The logo mark from the brand token (src/brand.ts), drawn in the accent colour on the N9 squircle (the Etesian icon shape, never a
     rounded square). -->
<template>
  <svg :width="size" :height="size" viewBox="0 0 64 64" role="img" :aria-label="brand.name" class="ph-logo">
    <path :d="SQUIRCLE" fill="currentColor" />
    <path :d="brand.logoPath" fill="var(--ph-on-accent)" />
  </svg>
</template>

<script setup lang="ts">
import { brand, SQUIRCLE_64 as SQUIRCLE } from '../brand'
defineProps({ size: { type: Number, default: 32 } })
</script>

<style scoped>
.ph-logo { color: var(--ph-accent); flex: none; }
</style>
