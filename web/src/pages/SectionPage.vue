<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- A section that does not exist yet: the empty state from STYLE.md, with the section's purpose in one sentence. The section comes
     from the route, or from `sectionId` when the page sits in a desktop window. -->
<template>
  <q-page class="ph-page">
    <h1>{{ info.title }}</h1>
    <HdStatusState :title="'Nothing here yet'" :icon="info.icon" :text="info.text" :data-test="'section-' + id">
      
        <q-btn outline color="primary" label="Back to the browser" to="/browse" />
      
    </HdStatusState>
  </q-page>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { placeholders } from '../nav'
import { HdStatusState } from '@etesian-design/vue'

const props = defineProps({ sectionId: { type: String, default: '' } })
const route = useRoute()
const id = computed(() => props.sectionId || route.meta.id || '')
const info = computed(() => placeholders[id.value] || { title: 'Section', icon: 'mail', text: '' })
</script>
