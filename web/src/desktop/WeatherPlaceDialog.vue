<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- Belle Weather's place picker (as on the phone, WeatherPlaceActivity): type a town, pick it from the matches (each with its
     region and country, as two towns can share a name), or go back to the server's default place. The pick is this device's. -->
<template>
  <q-dialog :model-value="modelValue" @update:model-value="emit('update:modelValue', $event)" @before-show="start">
    <q-card class="ph-dialog ph-wplace" data-test="weather-place">
      <q-card-section>
        <h2 class="ph-h3">Place</h2>
        <q-input ref="field" v-model="query" outlined dense label="Town or city" autofocus data-test="weather-place-q" @update:model-value="typed" @keydown.enter="first" />
      </q-card-section>
      <q-list class="ph-wplace__list" aria-label="Places">
        <q-item clickable data-test="weather-place-default" @click="pick(null)">
          <q-item-section>
            <q-item-label :class="{ 'text-primary': !mine }">Default place</q-item-label>
            <q-item-label caption>The one set in Settings → Weather</q-item-label>
          </q-item-section>
        </q-item>
        <q-item v-if="note"><q-item-section class="ph-muted" role="status" data-test="weather-place-note">{{ note }}</q-item-section></q-item>
        <q-item v-for="p in places" :key="p.name + p.detail + p.lat" clickable data-test="weather-place-row" @click="pick(p)">
          <q-item-section>
            <q-item-label>{{ p.name }}</q-item-label>
            <q-item-label caption>{{ p.detail }}</q-item-label>
          </q-item-section>
        </q-item>
      </q-list>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { myPlace, setMyPlace, searchPlaces, loadWeather, type Place } from './weather'

defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [boolean] }>()
const query = ref(''), places = ref<Place[]>([]), note = ref(''), mine = ref(false)
let timer = 0, asked = 0

function start() { const p = myPlace(); mine.value = !!p; query.value = p?.name || ''; places.value = []; note.value = '' }
function typed() { clearTimeout(timer); timer = window.setTimeout(find, 400) }
async function find() {
  const q = query.value.trim(), n = ++asked
  if (q.length < 2) { places.value = []; note.value = ''; return }
  note.value = 'Searching…'
  try {
    const found = await searchPlaces(q)
    if (n !== asked) return
    places.value = found; note.value = found.length ? '' : `No places called “${q}”`
  } catch (e) { if (n === asked) { places.value = []; note.value = (e as Error).message } }
}
function first() { if (places.value.length) pick(places.value[0]) }
function pick(p: Place | null) { setMyPlace(p); emit('update:modelValue', false); loadWeather() }
</script>

<style scoped>
.ph-wplace { width: min(420px, 92vw); }
.ph-wplace__list { max-height: 50vh; overflow-y: auto; padding-bottom: 8px; }
</style>
