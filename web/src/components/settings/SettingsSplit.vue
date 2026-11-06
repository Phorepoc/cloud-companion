<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- Settings, split, from the Etesian components: in room 760 px and wider (the window or the page: a container query) a side panel
     of grouped pages with squircle icons and search, the page beside it, as iPad and macOS settings; narrower, the list drills down
     (the list, tap, the page with Back). Pages marked admin show to the owner only. The same design as the shared Settings split
     (@etesian-design/shell XSettingsSplit); the page comes through the slot. -->
<template>
  <div class="ph-sset" :class="{ 'is-page': showPage }">
    <nav class="ph-sset__side" aria-label="Settings">
      <div class="ph-sset__search"><HdSearchField v-model="q" placeholder="Search settings" /></div>
      <template v-for="g in shown" :key="g.title">
        <HdSectionHeader :title="g.title" />
        <HdListItem v-for="p in g.pages" :key="p.id" :title="p.title" :subtitle="p.subtitle" small drilldown :selected="p.id === page"
                    :data-test="'settings-' + p.id" @click="open(p.id)">
          <template #leading><HdThumbnail :icon="p.icon" :size="32" :color="p.color" /></template>
        </HdListItem>
      </template>
    </nav>
    <section class="ph-sset__page" :aria-label="cur?.title">
      <button type="button" class="ph-sset__back hd-focusable" data-test="settings-back" @click="showPage = false"><HdIcon name="back" :size="18" />Settings</button>
      <slot :page="page" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { HdIcon, HdListItem, HdSearchField, HdSectionHeader, HdThumbnail } from '@etesian-design/vue'
import type { SettingsGroup } from '../../settingsNav'

const props = defineProps<{ groups: SettingsGroup[]; page: string; owner: boolean; narrowPage?: boolean }>()
const emit = defineEmits<{ 'update:page': [id: string] }>()
const q = ref('')
const shown = computed(() => props.groups
  .map((g) => ({ ...g, pages: g.pages.filter((p) => (props.owner || !p.admin) && (p.title + ' ' + (p.subtitle ?? '')).toLowerCase().includes(q.value.toLowerCase())) }))
  .filter((g) => g.pages.length))
const cur = computed(() => props.groups.flatMap((g) => g.pages).find((p) => p.id === props.page))
const showPage = ref(!!props.narrowPage)
watch(() => props.page, () => { showPage.value = true })
function open(id: string) { showPage.value = true; emit('update:page', id) }
</script>

<style scoped>
.ph-sset { flex: 1; min-height: 0; display: grid; grid-template-columns: 300px 1fr; background: var(--hd-c-bg); color: var(--hd-c-fg); }
.ph-sset__side { min-height: 0; overflow: auto; border-right: 1px solid var(--hd-c-divider); background: color-mix(in srgb, var(--hd-c-fg) 4%, var(--hd-c-bg)); }
.ph-sset__search { padding: 10px 12px 2px; }
.ph-sset__page { min-width: 0; min-height: 0; overflow: auto; padding: 4px 24px 24px; }
.ph-sset__back { display: none; align-items: center; gap: 4px; margin: 10px 0 0; padding: 6px 10px 6px 4px; border: 0; border-radius: 16px; background: none; color: var(--hd-c-accent-text); font: 400 var(--hd-font-size-default)/1 var(--hd-font-family-base); cursor: pointer; }
.ph-sset__back:hover { background: var(--hd-c-pressed); }
@container (max-width: 759px) {
  .ph-sset { grid-template-columns: 1fr; }
  .ph-sset__side { border: 0; background: none; }
  .ph-sset.is-page .ph-sset__side, .ph-sset:not(.is-page) .ph-sset__page { display: none; }
  .ph-sset__page { padding: 0 12px 16px; }
  .ph-sset__back { display: inline-flex; }
}
</style>
