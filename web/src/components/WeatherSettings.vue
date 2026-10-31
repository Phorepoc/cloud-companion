<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- Settings → Weather (owner only): where the weather comes from (internal/weather: Open-Meteo, MET Norway, OpenWeatherMap), the
     unit, the default place for devices that have not picked one, and each source's own options. The server tries the source
     before keeping anything. An API key is write-only: it is sealed on the server and never sent back. -->
<template>
  <section class="ph-card ph-section" aria-labelledby="set-weather" data-test="weather-settings">
    <h2 id="set-weather">Weather</h2>
    <p class="ph-muted">The weather on the desktop's Weather widget. Each device can pick its own place on the widget; the default place is for the rest.</p>
    <template v-if="cfg">
      <q-option-group v-model="cfg.provider" :options="providers.map((p) => ({ label: p.name, value: p.id }))" color="primary" data-test="weather-provider" />
      <p class="ph-muted ph-small">{{ about }}</p>
      <q-btn-toggle v-model="cfg.units" no-caps unelevated toggle-color="primary" toggle-text-color="on-accent" color="surface-2" text-color="ph"
                    :options="[{ label: 'Celsius', value: 'c' }, { label: 'Fahrenheit', value: 'f' }]" data-test="weather-units" />
      <q-input v-if="needsKey" v-model="key" outlined dense type="password" autocomplete="off" class="q-mt-md" data-test="weather-key"
               :label="hasKey ? 'API key (saved; type a new one to replace it)' : 'API key'" :hint="canKeepKeys ? '' : 'This server has no vault, so it cannot keep keys.'" />
      <q-input v-if="cfg.provider === 'met-no'" v-model="cfg.contact" outlined dense class="q-mt-md" label="Contact (an e-mail or a site)" data-test="weather-contact"
               hint="MET Norway asks who to contact about the requests." />
      <q-expansion-item dense label="Own server" class="q-mt-sm" header-class="ph-small">
        <q-input v-model="cfg.baseURL" outlined dense class="q-mt-sm" label="Weather address" placeholder="https://api.open-meteo.com" data-test="weather-base"
                 hint="Leave empty for the source's own. For a self-hosted Open-Meteo, its address." />
        <q-input v-if="cfg.provider !== 'openweathermap'" v-model="cfg.geoURL" outlined dense class="q-mt-sm" label="Place search address" placeholder="https://geocoding-api.open-meteo.com" />
      </q-expansion-item>
      <h3 class="ph-h3">Default place</h3>
      <div class="ph-actions">
        <span data-test="weather-default">{{ cfg.place ? cfg.place.name + (cfg.place.detail ? ', ' + cfg.place.detail : '') : 'None yet' }}</span>
        <q-btn flat no-caps color="primary" label="Change" data-test="weather-default-change" @click="picking = true" />
      </div>
      <div v-if="picking">
        <q-input v-model="query" outlined dense autofocus label="Town or city" data-test="weather-default-q" @update:model-value="typed" />
        <q-list dense>
          <q-item v-for="p in found" :key="p.name + p.detail + p.lat" clickable data-test="weather-default-row" @click="cfg.place = p; picking = false">
            <q-item-section><q-item-label>{{ p.name }}</q-item-label><q-item-label caption>{{ p.detail }}</q-item-label></q-item-section>
          </q-item>
        </q-list>
      </div>
      <div class="ph-actions">
        <q-btn unelevated color="primary" label="Save" :loading="busy" data-test="weather-save" @click="save" />
        <span v-if="saved" class="ph-ok" role="status" data-test="weather-saved"><q-icon name="sym_o_check_circle" size="20px" aria-hidden="true" /> Saved</span>
      </div>
    </template>
    <p v-if="error" class="ph-error" role="alert" data-test="weather-error"><q-icon name="sym_o_error" size="20px" aria-hidden="true" /> {{ error }}</p>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { apiCall } from '../cloud'
import { searchPlaces, loadWeather, type Place } from '../desktop/weather'

interface Cfg { provider: string; units: string; place?: Place | null; baseURL?: string; geoURL?: string; contact?: string }
interface Provider { id: string; name: string; key: boolean; about: string }
const cfg = ref<Cfg | null>(null), providers = ref<Provider[]>([]), hasKey = ref(false), canKeepKeys = ref(true)
const key = ref(''), busy = ref(false), saved = ref(false), error = ref('')
const picking = ref(false), query = ref(''), found = ref<Place[]>([])
const needsKey = computed(() => providers.value.find((p) => p.id === cfg.value?.provider)?.key)
const about = computed(() => providers.value.find((p) => p.id === cfg.value?.provider)?.about || '')

onMounted(async () => {
  try {
    const j = await apiCall('GET', '/phe/weather/config')
    cfg.value = j.config; providers.value = j.providers; hasKey.value = j.hasKey; canKeepKeys.value = j.canKeepKeys
  } catch (e) { error.value = (e as Error).message }
})
let timer = 0
function typed() {
  clearTimeout(timer)
  timer = window.setTimeout(async () => { try { found.value = await searchPlaces(query.value) } catch (e) { error.value = (e as Error).message } }, 400)
}
async function save() {
  if (!cfg.value) return
  busy.value = true; saved.value = false; error.value = ''
  try {
    const j = await apiCall('POST', '/phe/weather/config', { ...cfg.value, key: key.value })
    hasKey.value = j.hasKey; key.value = ''; saved.value = true
    loadWeather()
  } catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
</script>
