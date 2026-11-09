<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- Settings: one place for this device and, for the owner, the server (Connections, Devices, Features, Catalogue, Weather, Security). The Etesian
     Settings split (@etesian-design/shell XSettingsSplit): on a wide window or screen a side panel of grouped pages with search
     and the page beside it, as iPad and macOS settings; narrower, the list drills down and the page has Back. Each page has its address
     (/settings/connections); the old /connections, /devices and /catalogue open their page here. -->
<template>
  <q-page class="ph-settings">
    <div class="ph-settings__box">
      <XSettingsSplit :groups="groups" :owner="state.admin" :page="page" :narrow-page="!!route.params.page" @update:page="pick">
        <template #page="{ page: id }">
          <ConnectionsPage v-if="id === 'connections'" />
          <DevicesPage v-else-if="id === 'devices'" />
          <CataloguePage v-else-if="id === 'catalogue'" />
          <FeaturesPage v-else-if="id === 'features'" />
          <BackupsPage v-else-if="id === 'backups'" />
          <UpdatesPage v-else-if="id === 'updates'" />
          <SystemPage v-else-if="id === 'system'" />
          <BrowserSettingsPage v-else-if="id === 'webbrowser'" />
          <DeviceSettings v-else :part="id" />
        </template>
      </XSettingsSplit>
    </div>
  </q-page>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { XSettingsSplit, useChrome } from '@etesian-design/shell'
import { state } from '../cloud'
import { SETTINGS_GROUPS, settingsPage, settingsPageIds } from '../settingsNav'
import DeviceSettings from '../components/settings/DeviceSettings.vue'

const ConnectionsPage = defineAsyncComponent(() => import('./ConnectionsPage.vue'))
const DevicesPage = defineAsyncComponent(() => import('./DevicesPage.vue'))
const CataloguePage = defineAsyncComponent(() => import('./CataloguePage.vue'))
const FeaturesPage = defineAsyncComponent(() => import('./FeaturesPage.vue'))
const BackupsPage = defineAsyncComponent(() => import('./BackupsPage.vue'))
const UpdatesPage = defineAsyncComponent(() => import('./UpdatesPage.vue'))
const SystemPage = defineAsyncComponent(() => import('./SystemPage.vue'))
const BrowserSettingsPage = defineAsyncComponent(() => import('./BrowserSettingsPage.vue'))

const route = useRoute()
const router = useRouter()
const groups = SETTINGS_GROUPS
// the classic layout follows the address; in a desktop window the route is /desktop and the page comes from settingsPage alone
watch(() => route.params.page, (p) => { if (typeof p === 'string' && settingsPageIds.includes(p)) settingsPage.value = p }, { immediate: true })
const page = computed(() => settingsPage.value)
function pick(id: string) {
  settingsPage.value = id
  if (route.path.startsWith('/settings')) router.replace('/settings/' + id)
}
useChrome(() => ({ title: 'Settings' }))
</script>

<style scoped>
.ph-settings { display: flex; }
/* the split follows the room it has (a window, or the page), not the screen */
.ph-settings__box { flex: 1; min-width: 0; display: flex; container-type: inline-size; }
.ph-settings__box :deep(.x-sset) { min-height: 100%; }
/* the pages inside are pages of their own elsewhere: no second page height or margins */
.ph-settings__box :deep(.q-page) { min-height: 0 !important; padding: 0; }
.ph-settings__box :deep(.x-sset__page) { padding-top: 4px; }
</style>
