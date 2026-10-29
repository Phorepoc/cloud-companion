<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- A section's icon on its lit N9 squircle tile (Etesian icon lighting, css/etesian/base.css), in the section's colour (nav.ts).
     Decorative: the label next to it, or the control around it, names the section. -->
<template>
  <span class="ph-tile hd-squircle hd-icon-lit" :style="{ '--hd-icon-accent': section.color, '--hd-icon-size': size + 'px' }" aria-hidden="true">
    <q-icon :name="section.icon" :size="Math.round(size * 0.6) + 'px'" />
  </span>
</template>

<script setup lang="ts">
defineProps({ section: { type: Object, required: true }, size: { type: Number, default: 32 } })
</script>
