<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- Music: the songs in your files (any folder; Artist/Album/NN Title or the files' own tags), by album, artist or song. The
     player keeps playing across sections and windows; the bar at the bottom has the song, seeking, previous / play / next as lit
     squircles, and "Play on" to hand the music to another of your devices. Album art is shown as it is; an album without art gets
     the generated cover. -->
<template>
  <q-page class="ph-page ph-music" data-test="music-page">
    <div class="ph-head">
      <h1>Music</h1>
      <q-btn-toggle v-model="tab" no-caps unelevated toggle-color="primary" data-test="music-tabs"
                    :options="[{ label: 'Albums', value: 'albums' }, { label: 'Artists', value: 'artists' }, { label: 'Songs', value: 'songs' }]" @update:model-value="album = null; artist = ''" />
    </div>
    <q-banner v-if="error" class="ph-note is-error q-mb-md" rounded role="alert">
      <template #avatar><q-icon name="sym_o_error" aria-hidden="true" /></template>
      {{ error }}
      <template #action><q-btn flat label="Try again" @click="load" /></template>
    </q-banner>
    <EmptyState v-if="loading && !tracks.length" state="loading" title="Loading your music" text="This takes a moment." />
    <EmptyState v-else-if="!loading && !tracks.length && !error" icon="sym_o_music_note" title="No music yet" data-test="music-empty"
                text="Upload songs in Files. A folder per artist with a folder per album inside works best; a cover.jpg in an album's folder becomes its art.">
      <template #action><q-btn outline color="primary" label="Open Files" to="/files" /></template>
    </EmptyState>

    <!-- one album or one artist -->
    <template v-if="album">
      <q-btn flat no-caps icon="sym_o_arrow_back" :label="tab === 'artists' ? artist : 'Albums'" class="q-mb-sm ph-back" @click="album = null" />
      <div class="ph-albumhead">
        <AlbumCover :album="album.name" :cover="album.cover" :size="160" />
        <div><h2>{{ album.name }}</h2><p class="ph-muted">{{ album.artist }} · {{ album.tracks.length }} songs</p>
          <q-btn unelevated no-caps color="primary" icon="sym_o_play_arrow" label="Play" data-test="album-play" @click="play(album.tracks, 0)" /></div>
      </div>
      <SongList :list="album.tracks" />
    </template>
    <template v-else-if="tab === 'albums' && tracks.length">
      <div class="ph-albums" data-test="music-albums">
        <button v-for="a in albums" :key="a.key" type="button" class="ph-album" :data-test="'album-' + a.name" @click="album = a">
          <AlbumCover :album="a.name" :cover="a.cover" :size="150" />
          <b>{{ a.name }}</b><span class="ph-muted ph-small">{{ a.artist }}</span>
        </button>
      </div>
    </template>
    <template v-else-if="tab === 'artists' && tracks.length">
      <ul v-if="!artist" class="ph-list ph-card">
        <li v-for="a in artists" :key="a.name"><button type="button" class="ph-row" @click="artist = a.name">
          <span class="ph-ftile hd-squircle" aria-hidden="true"><q-icon name="sym_o_person" size="22px" /></span>
          <span class="ph-row-main"><span class="ph-row-title">{{ a.name }}</span><span class="ph-small ph-muted">{{ a.albums }} albums · {{ a.songs }} songs</span></span></button></li>
      </ul>
      <template v-else>
        <q-btn flat no-caps icon="sym_o_arrow_back" label="Artists" class="q-mb-sm ph-back" @click="artist = ''" />
        <h2>{{ artist }}</h2>
        <div class="ph-albums"><button v-for="a in albums.filter((x) => x.artist === artist)" :key="a.key" type="button" class="ph-album" @click="album = a">
          <AlbumCover :album="a.name" :cover="a.cover" :size="150" /><b>{{ a.name }}</b></button></div>
      </template>
    </template>
    <SongList v-else-if="tab === 'songs' && tracks.length" :list="tracks" />

    <!-- now playing -->
    <div v-if="now" class="ph-nowbar" data-test="music-now">
      <AlbumCover :album="now.album" :cover="now.cover" :size="56" />
      <div class="ph-nowbar__main">
        <span class="ph-nowbar__t"><b data-test="music-now-title">{{ now.title }}</b> <span class="ph-muted">{{ now.artist }}</span></span>
        <div class="ph-nowbar__seek"><span class="ph-small">{{ mmss(player.pos) }}</span>
          <q-slider :model-value="player.pos" :min="0" :max="player.dur || 1" :step="1" aria-label="Position" dense @update:model-value="(v) => seek(v ?? 0)" />
          <span class="ph-small">{{ mmss(player.dur) }}</span></div>
        <span v-if="player.error" class="text-negative ph-small" role="alert">{{ player.error }}</span>
      </div>
      <div class="ph-nowbar__ctl">
        <button type="button" class="ph-lit hd-squircle" aria-label="Previous" @click="prev"><svg viewBox="0 0 24 24" width="16" height="16"><path d="M6 6h2v12H6zm3.5 6 8.5 6V6z" fill="currentColor" /></svg></button>
        <button type="button" class="ph-lit ph-lit--big hd-squircle" :aria-label="player.playing ? 'Pause' : 'Play'" data-test="music-toggle" @click="toggle()">
          <svg viewBox="0 0 24 24" width="22" height="22"><path :d="player.playing ? 'M6 5h4v14H6zm8 0h4v14h-4z' : 'M8 5v14l11-7z'" fill="currentColor" /></svg></button>
        <button type="button" class="ph-lit hd-squircle" aria-label="Next" @click="next"><svg viewBox="0 0 24 24" width="16" height="16"><path d="M16 6h2v12h-2zM6 18l8.5-6L6 6z" fill="currentColor" /></svg></button>
      </div>
      <q-btn v-if="targets.length" flat no-caps icon="sym_o_cast" label="Play on" data-test="music-playon">
        <q-menu auto-close><q-list dense style="min-width: 200px">
          <q-item v-for="d in targets" :key="d.id" clickable :data-test="'playon-' + d.name" @click="castTo(d)">
            <q-item-section avatar><q-icon name="sym_o_devices" /></q-item-section>
            <q-item-section>{{ d.name }}<q-item-label caption>{{ d.online ? 'Connected' : 'Picks it up when it next opens' }}</q-item-label></q-item-section>
          </q-item>
        </q-list></q-menu>
      </q-btn>
    </div>
  </q-page>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, ref, watch } from 'vue'
import EmptyState from '../components/EmptyState.vue'
import AlbumCover from '../components/AlbumCover.vue'
import { api, connect, creds, state } from '../cloud'
import { loadTracks, type Track } from '../files'
import { current, mmss, next, play, playOn, player, prev, seek, toggle } from '../music'
import { toast } from '../toast'

interface Album { key: string; name: string; artist: string; cover: string; tracks: Track[] }
const tracks = ref<Track[]>([])
const loading = ref(false), error = ref('')
const tab = ref<'albums' | 'artists' | 'songs'>('albums')
const album = ref<Album | null>(null)
const artist = ref('')
const now = computed(() => current())

const albums = computed(() => {
  const m = new Map<string, Album>()
  for (const t of tracks.value) {
    const k = t.artist + '\u0000' + t.album
    let a = m.get(k)
    if (!a) m.set(k, (a = { key: k, name: t.album, artist: t.artist, cover: t.cover || '', tracks: [] }))
    a.tracks.push(t)
    if (!a.cover && t.cover) a.cover = t.cover
  }
  return [...m.values()].sort((a, b) => a.name.localeCompare(b.name))
})
const artists = computed(() => {
  const m = new Map<string, { name: string; albums: Set<string>; songs: number }>()
  for (const t of tracks.value) {
    const a = m.get(t.artist) || { name: t.artist, albums: new Set<string>(), songs: 0 }
    a.albums.add(t.album); a.songs++
    m.set(t.artist, a)
  }
  return [...m.values()].map((a) => ({ name: a.name, albums: a.albums.size, songs: a.songs })).sort((a, b) => a.name.localeCompare(b.name))
})
const targets = computed(() => (state.deviceList || []).filter((d) => !d.revoked && d.id !== creds.value?.dev)
  .map((d) => ({ ...d, online: !!d.seen && Date.now() / 1000 - d.seen < 120 })))

async function castTo(d: { id: string; name: string }) {
  try { await playOn(d.id); toast('Playing on ' + d.name) } catch (e) { player.error = (e as Error).message }
}

async function load() {
  loading.value = true
  error.value = ''
  try { tracks.value = await loadTracks() } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}

const SongList = defineComponent({
  props: { list: { type: Array as () => Track[], required: true } },
  setup(p) {
    return () => h('ul', { class: 'ph-list ph-card', 'data-test': 'music-songs' }, p.list.map((t, i) =>
      h('li', { key: t.path }, h('button', { type: 'button', class: ['ph-row', { 'is-now': current()?.path === t.path }], 'data-test': 'song-' + t.title, onClick: () => play(p.list, i) }, [
        h('span', { class: 'ph-n ph-muted' }, t.n ? String(t.n) : '·'),
        h('span', { class: 'ph-row-main' }, [h('span', { class: 'ph-row-title' }, t.title), h('span', { class: 'ph-small ph-muted' }, t.artist + ' · ' + t.album)]),
      ]))))
  },
})

onMounted(() => { load(); connect().catch(() => { /* the status in the top bar shows it */ }) })
// the devices for "Play on" (owner only), asked again whenever the connection comes back
watch(() => state.status === 'online' && state.admin, (on) => { if (on) api.devices() }, { immediate: true })
</script>

<style scoped>
.ph-music { display: flex; flex-direction: column; }
.ph-head { display: flex; align-items: center; justify-content: space-between; gap: var(--ph-s3); margin-bottom: var(--ph-s4); flex-wrap: wrap; }
.ph-back { align-self: flex-start; }
.ph-albums { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: var(--ph-s4); }
.ph-album { display: flex; flex-direction: column; align-items: flex-start; gap: 4px; padding: 0; border: 0; background: none; color: var(--ph-text); font: inherit; text-align: left; cursor: pointer; min-width: 0; }
.ph-album b { max-width: 150px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; margin-top: 4px; }
.ph-albumhead { display: flex; gap: var(--ph-s4); align-items: flex-end; margin-bottom: var(--ph-s4); }
:deep(.ph-list) { list-style: none; padding: 0; margin: 0; }
:deep(.ph-list li) { border-bottom: 1px solid var(--ph-divider); }
:deep(.ph-list li:last-child) { border-bottom: 0; }
:deep(.ph-row) { width: 100%; min-height: 52px; display: flex; align-items: center; gap: var(--ph-s3); padding: var(--ph-s2) var(--ph-s4); background: none; border: 0; text-align: left; font: inherit; color: var(--ph-text); cursor: pointer; }
:deep(.ph-row:hover) { background: var(--ph-surface-2); }
:deep(.ph-row.is-now .ph-row-title) { color: #E6007E; }
:deep(.ph-row-main) { display: flex; flex-direction: column; min-width: 0; }
:deep(.ph-row-title) { font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
:deep(.ph-n) { width: 22px; text-align: right; }
.ph-ftile { flex: none; width: 40px; height: 40px; display: grid; place-items: center; background: var(--ph-accent-soft); color: var(--ph-accent); }
.ph-nowbar { position: sticky; bottom: 0; z-index: 50; margin: auto calc(-1 * var(--ph-s4)) calc(-1 * var(--ph-s4)); display: flex; align-items: center; gap: var(--ph-s3); padding: 10px var(--ph-s4);
  background: linear-gradient(#2A2A2E, #0E0E10); color: #fff; box-shadow: 0 -2px 10px rgba(0, 0, 0, 0.35); }
.ph-nowbar .ph-muted { color: #B8B8BC; }
.ph-nowbar__main { flex: 1; min-width: 0; }
.ph-nowbar__t { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ph-nowbar__seek { display: flex; align-items: center; gap: var(--ph-s2); }
.ph-nowbar__ctl { display: flex; align-items: center; gap: 10px; }
/* Belle lit squircle buttons (dark glossy, top-lit) */
.ph-lit { display: grid; place-items: center; width: 36px; height: 36px; padding: 0; border: 0; color: #fff; cursor: pointer;
  background: linear-gradient(#4A4A4F, #1E1E21 50%, #0C0C0D); }
.ph-lit--big { width: 46px; height: 46px; }
.ph-lit:hover { background: linear-gradient(#5A5A60, #2A2A2E 50%, #141416); }
</style>
