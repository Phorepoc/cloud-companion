<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: AGPL-3.0-only -->
<!-- List and detail, as the Etesian Mail and Settings (Web/Classic layout, Desktop/Settings split): side by side when the page or the
     window has 760 px, each pane scrolling on its own; narrower, one at a time (the list, tap, the detail with Back). The page that
     holds it fills its height (fillPage). Styles in css/panes.css. -->
<template>
  <div class="ph-split-box">
    <div class="ph-split" :class="{ 'is-detail': detail }">
      <section class="ph-split__list" :aria-label="listLabel"><slot name="list" /></section>
      <section class="ph-split__detail" :aria-label="detailLabel"><slot name="detail" /></section>
    </div>
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{ detail?: boolean; listLabel?: string; detailLabel?: string }>(), { detail: false })
</script>
