<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- A section that does not exist yet: the empty state from STYLE.md, with the section's purpose in one sentence. -->
<template>
  <q-page class="ph-page">
    <h1>{{ info.title }}</h1>
    <EmptyState :title="'Nothing here yet'" :icon="info.icon" :text="info.text" :data-test="'section-' + route.meta.id">
      <template #action>
        <q-btn outline color="primary" label="Back to the browser" to="/browse" />
      </template>
    </EmptyState>
  </q-page>
</template>

<script setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { placeholders } from '../nav.js'
import EmptyState from '../components/EmptyState.vue'

const route = useRoute()
const info = computed(() => placeholders[route.meta.id] || { title: 'Section', icon: 'sym_o_inbox', text: '' })
</script>
