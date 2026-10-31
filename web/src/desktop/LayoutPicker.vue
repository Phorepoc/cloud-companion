<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- The snap-layout picker (etesian-design explorations/desktop2/XLayoutPicker.vue, Plasma's etesian_snaplayouts): the six layouts,
     three per row, numbered. It drops down when a window is dragged to the top edge (release over a zone to snap there), and
     Super+Z / Ctrl+Alt+Z opens it from the keyboard: 1–6 picks the layout, then the zone's number. -->
<template>
  <section class="ph-lpick" role="dialog" aria-label="Snap layouts" data-test="layout-picker">
    <header class="ph-lpick__head"><h2>Snap layouts</h2><kbd>Ctrl+Alt+Z</kbd></header>
    <div class="ph-lpick__grid">
      <div v-for="(l, i) in LAYOUTS" :key="l.label" class="ph-lpick__item" :class="{ 'is-on': layout === i }">
        <div class="ph-lpick__mini" role="group" :aria-label="l.label">
          <button v-for="(z, j) in l.zones" :key="j" type="button" class="ph-lpick__z" :class="{ 'is-hot': hot && hot[0] === i && hot[1] === j }"
                  :data-lz="i + ':' + j" :data-test="`layout-${i + 1}-${j + 1}`" :aria-label="`${l.label}, zone ${j + 1}`"
                  :style="{ left: z.x * 100 + '%', top: z.y * 100 + '%', width: z.w * 100 + '%', height: z.h * 100 + '%' }" @click="emit('pick', i, j)">
            <span v-if="layout === i">{{ j + 1 }}</span></button>
        </div>
        <span class="ph-lpick__lab"><kbd>{{ i + 1 }}</kbd>{{ l.label }}</span>
      </div>
    </div>
    <footer class="ph-lpick__foot">{{ drag ? 'Release over a zone to snap there · drag down to keep the window floating' : '1–6 layout, then the zone number · Esc cancels' }}</footer>
  </section>
</template>

<script setup lang="ts">
import { LAYOUTS } from './snaplayouts'
defineProps<{ layout: number; hot: [number, number] | null; drag: boolean }>()
const emit = defineEmits<{ pick: [layout: number, zone: number] }>()
</script>

<style scoped>
.ph-lpick { position: absolute; z-index: 9300; top: 8px; left: 50%; transform: translateX(-50%); width: 640px; padding: 16px 18px 12px;
  border-radius: var(--ph-r-card); background: rgba(20, 20, 20, 0.96); color: #fff; box-shadow: var(--hd-shadow-overlay), 0 0 0 1px rgba(255, 255, 255, 0.16);
  -webkit-backdrop-filter: blur(16px); backdrop-filter: blur(16px); font-family: var(--ph-font); }
.ph-lpick__head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.ph-lpick__head h2 { margin: 0; font: 300 21px/1 var(--ph-font-light); }
kbd { min-width: 16px; padding: 2px 4px; border: 1px solid rgba(255, 255, 255, 0.16); border-radius: 3px; font: 700 10px/1 var(--ph-font); color: #C8C8C8; }
.ph-lpick__grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 14px 16px; }
.ph-lpick__item { display: flex; flex-direction: column; gap: 6px; padding: 8px; border-radius: 8px; }
.ph-lpick__item.is-on { background: rgba(255, 255, 255, 0.08); }
.ph-lpick__mini { position: relative; height: 90px; }
.ph-lpick__z { position: absolute; padding: 0; border: 0; background: transparent; cursor: pointer; color: #fff; font-weight: 700; }
.ph-lpick__z::before { content: ''; position: absolute; inset: 3px; border-radius: 5px; background: rgba(255, 255, 255, 0.22); }
.ph-lpick__z span { position: relative; }
.ph-lpick__z:hover::before, .ph-lpick__z.is-hot::before { background: #4591FF; }
.ph-lpick__lab { display: flex; align-items: center; gap: 6px; font-size: 12px; color: #C8C8C8; }
.ph-lpick__foot { margin-top: 12px; padding-top: 10px; border-top: 1px solid rgba(255, 255, 255, 0.16); font-size: 12px; color: #C8C8C8; }
</style>
