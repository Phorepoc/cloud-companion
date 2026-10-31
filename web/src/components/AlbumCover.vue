<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- An album's art, shown as it is: square, edge to edge, never rounded or cropped into a shape. An album without art gets the
     Etesian generated cover (as the phone's Music): its name in heavy capitals in a bright colour, then the name again without
     spaces in a dark shade of it, filling the square, on black. The colour stays the same for the album; an unknown album is purple. -->
<template>
  <span class="ph-cover" :style="{ width: size + 'px', height: size + 'px' }" role="img" :aria-label="album + ' cover'">
    <img v-if="src" :src="src" alt="" @error="src = ''">
    <span v-else class="ph-cover__gen" :style="{ '--c': colour, '--d': dark, fontSize: size / 7 + 'px' }" aria-hidden="true">
      <b>{{ album.toUpperCase() }}</b><i>{{ filler }}</i>
    </span>
  </span>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { fileURL } from '../files'

const props = withDefaults(defineProps<{ album: string; cover?: string; size?: number }>(), { cover: '', size: 120 })
const src = ref('')
watch(() => props.cover, async (c) => { src.value = c ? await fileURL(c).catch(() => '') : '' }, { immediate: true })

const COLOURS = ['#FF6A00', '#E6007E', '#00A3E0', '#8DC63F', '#FFC20E', '#00B2A9', '#ED1C24', '#2E8BFF']
const unknown = computed(() => !props.album || /^unknown album$/i.test(props.album))
const colour = computed(() => {
  if (unknown.value) return '#9B30D9'
  let h = 0
  for (const ch of props.album) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  return COLOURS[h % COLOURS.length]
})
const dark = computed(() => `color-mix(in srgb, ${colour.value} 35%, #000)`)
const filler = computed(() => (props.album.replace(/\s+/g, '').toUpperCase() || 'MUSIC').repeat(12))
</script>

<style scoped>
.ph-cover { position: relative; flex: none; display: block; overflow: hidden; background: #000; }
.ph-cover img { display: block; width: 100%; height: 100%; object-fit: contain; }
.ph-cover__gen { position: absolute; inset: 0; padding: 6%; overflow: hidden; font-family: var(--ph-font); line-height: 0.95; text-align: justify; word-break: break-all; }
.ph-cover__gen b { color: var(--c); font-weight: 900; }
.ph-cover__gen i { margin-left: 0.25em; font-style: normal; font-weight: 900; color: var(--d); }
</style>
