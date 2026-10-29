<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- A section that does not exist yet: the empty state from STYLE.md, with the section's purpose in one sentence. The section comes
     from the route, or from `sectionId` when the page sits in a desktop window. -->
<template>
  <q-page class="ph-page">
    <h1>{{ info.title }}</h1>
    <EmptyState :title="'Nothing here yet'" :icon="info.icon" :text="info.text" :data-test="'section-' + id">
      <template #action>
        <q-btn outline color="primary" label="Back to the browser" to="/browse" />
      </template>
    </EmptyState>
  </q-page>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { placeholders } from '../nav'
import EmptyState from '../components/EmptyState.vue'

const props = defineProps({ sectionId: { type: String, default: '' } })
const route = useRoute()
const id = computed(() => props.sectionId || route.meta.id || '')
const info = computed(() => placeholders[id.value] || { title: 'Section', icon: 'sym_o_inbox', text: '' })
</script>
