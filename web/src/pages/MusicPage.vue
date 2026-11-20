<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: AGPL-3.0-only -->
<!-- Music: the songs in your files (any folder; Artist/Album/NN Title or the files' own tags), by album, artist or song. The
     player keeps playing across sections and windows; the library is the music-app story (cover collage, then Artists / Albums / Songs / Playlists);
     the bar at the bottom is its now playing (song n/m, artist, album, previous / play / next, the pink progress line), and "Play on" to hand the music to another of your devices. Album art is shown as it is; an album without art gets
     the generated cover. -->
<template>
  <q-page class="ph-page ph-music" data-test="music-page">
    <!-- with a header (the classic bar, a desktop window) Albums | Artists | Songs is the title's menu -->
    <div v-if="!chrome.hosted" class="ph-head">
      <h1>Music</h1>
      <q-btn-toggle v-model="tab" no-caps unelevated toggle-color="primary" data-test="music-tabs"
                    :options="[{ label: 'Library', value: 'library' }, { label: 'Albums', value: 'albums' }, { label: 'Artists', value: 'artists' }, { label: 'Songs', value: 'songs' }]" @update:model-value="album = null; artist = ''" />
    </div>
    <q-banner v-if="error" inline-actions class="ph-note is-error q-mb-md" rounded role="alert">
      <template #avatar><q-icon name="sym_o_error" aria-hidden="true" /></template>
      {{ error }}
      <template #action><q-btn flat label="Try again" @click="load" /></template>
    </q-banner>
    <HdStatusState v-if="loading && !tracks.length" state="loading" title="Loading your music" text="This takes a moment." />
    <HdStatusState v-else-if="!loading && !tracks.length && !error" icon="music" title="No music yet" data-test="music-empty"
                text="Upload songs in Files. A folder per artist with a folder per album inside works best; a cover.jpg in an album's folder becomes its art.">
      <q-btn outline color="primary" label="Open Files" to="/files" />
    </HdStatusState>

    <!-- one album or one artist -->
    <template v-if="album">
      <q-btn flat no-caps icon="sym_o_arrow_back" :label="tab === 'artists' ? artist : tab === 'library' ? 'Library' : 'Albums'" class="q-mb-sm ph-back" @click="album = null" />
      <div class="ph-albumhead">
        <AlbumCover :album="album.name" :cover="album.cover" :size="160" />
        <div><h2>{{ album.name }}</h2><p class="ph-muted">{{ album.artist }} · {{ album.tracks.length }} songs</p>
          <q-btn unelevated no-caps color="primary" icon="sym_o_play_arrow" label="Play" data-test="album-play" @click="play(album.tracks, 0)" /></div>
      </div>
      <SongList :list="album.tracks" />
    </template>
    <!-- the library (music-app story): a collage of the album covers, the newest big with its name on a pink strip, then the
         Artists / Albums / Songs / Playlists rows -->
    <template v-else-if="tab === 'library' && tracks.length">
      <div class="ph-collage" data-test="music-collage">
        <button v-for="(a, i) in albums.slice(0, 6)" :key="a.key" type="button" class="ph-collage__tile" :class="{ 'is-big': i === 0 }"
                :data-test="'album-' + a.name" :aria-label="a.name + ', ' + a.artist" @click="album = a">
          <AlbumCover :album="a.name" :cover="a.cover" :size="i === 0 ? 240 : 120" />
          <span v-if="i === 0" class="ph-collage__label"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M7 5v14l12-7z" fill="currentColor" /></svg>{{ a.name }}</span>
        </button>
      </div>
      <ul class="ph-libmenu" data-test="music-library">
        <li><button type="button" @click="tab = 'artists'">Artists<span class="ph-libmenu__n">{{ artists.length }}</span><q-icon name="sym_o_chevron_right" size="24px" /></button></li>
        <li><button type="button" @click="tab = 'albums'">Albums<span class="ph-libmenu__n">{{ albums.length }}</span><q-icon name="sym_o_chevron_right" size="24px" /></button></li>
        <li><button type="button" @click="tab = 'songs'">Songs<span class="ph-libmenu__n">{{ tracks.length }}</span><q-icon name="sym_o_chevron_right" size="24px" /></button></li>
        <li><span class="ph-libmenu__off">Playlists<span class="ph-libmenu__n">none yet</span></span></li>
      </ul>
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

    <!-- now playing (music-app story): title and n/m, artist and album, previous / play / next, the pink progress line with times -->
    <div v-if="now" class="ph-nowbar" data-test="music-now">
      <AlbumCover class="ph-nowbar__cover" :album="now.album" :cover="now.cover" :size="72" />
      <div class="ph-nowbar__main">
        <div class="ph-nowbar__l1"><b data-test="music-now-title">{{ now.title }}</b><span class="ph-nowbar__n">{{ player.i + 1 }}/{{ player.queue.length }}</span></div>
        <div class="ph-nowbar__l2">{{ now.artist }}</div>
        <div class="ph-nowbar__l2">{{ now.album }}</div>
        <span v-if="player.error" class="text-negative ph-small" role="alert">{{ player.error }}</span>
      </div>
      <div class="ph-nowbar__ctl">
        <button type="button" class="ph-np" aria-label="Previous" @click="prev"><svg viewBox="0 0 24 24" width="22" height="22"><path d="M7 5v14M18 5 9 12l9 7z" stroke="currentColor" stroke-width="1.6" fill="none" stroke-linejoin="round" /></svg></button>
        <button type="button" class="ph-np" :aria-label="player.playing ? 'Pause' : 'Play'" data-test="music-toggle" @click="toggle()">
          <svg viewBox="0 0 24 24" width="24" height="24"><path :d="player.playing ? 'M8 5v14M16 5v14' : 'M8 5v14l11-7z'" stroke="currentColor" stroke-width="1.8" fill="none" stroke-linejoin="round" /></svg></button>
        <button type="button" class="ph-np" aria-label="Next" @click="next"><svg viewBox="0 0 24 24" width="22" height="22"><path d="M17 5v14M6 5l9 7-9 7z" stroke="currentColor" stroke-width="1.6" fill="none" stroke-linejoin="round" /></svg></button>
      </div>
      <div class="ph-nowbar__seek">
        <q-slider :model-value="player.pos" :min="0" :max="player.dur || 1" :step="1" aria-label="Position" dense color="pink-8" track-color="grey-9" thumb-size="8px"
                  @update:model-value="(v) => seek(v ?? 0)" />
        <div class="ph-nowbar__times"><span>{{ mmss(player.pos) }}</span><span>{{ mmss(player.dur) }}</span></div>
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
import { HdStatusState } from '@etesian-design/vue'
import { useChrome } from '@etesian-design/shell'
import AlbumCover from '../components/AlbumCover.vue'
import { api, connect, creds, state } from '../cloud'
import { loadTracks, type Track } from '../files'
import { current, mmss, next, play, playOn, player, prev, seek, toggle } from '../music'
import { toast } from '../toast'

interface Album { key: string; name: string; artist: string; cover: string; tracks: Track[] }
const tracks = ref<Track[]>([])
const loading = ref(false), error = ref('')
const tab = ref<'library' | 'albums' | 'artists' | 'songs'>('library')
const album = ref<Album | null>(null)
const artist = ref('')

const VIEWS: Record<string, 'library' | 'albums' | 'artists' | 'songs'> = { Library: 'library', Albums: 'albums', Artists: 'artists', Songs: 'songs' }
const chrome = useChrome(() => ({ title: 'Music', views: Object.keys(VIEWS), view: tab.value[0]!.toUpperCase() + tab.value.slice(1) }))
chrome.on('view', (v) => { const t = v ? VIEWS[v] : undefined; if (t) { tab.value = t; album.value = null; artist.value = '' } })
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

// the song playing, as one value: the list redraws when the song changes, not on every tick of the player's position
const nowPath = computed(() => current()?.path)
const SongList = defineComponent({
  props: { list: { type: Array as () => Track[], required: true } },
  setup(p) {
    return () => h('ul', { class: 'ph-list ph-card', 'data-test': 'music-songs' }, p.list.map((t, i) =>
      h('li', { key: t.path }, h('button', { type: 'button', class: ['ph-row', { 'is-now': nowPath.value === t.path }], 'data-test': 'song-' + t.title, onClick: () => play(p.list, i) }, [
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
.ph-music { max-width: none; }
.ph-albums { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: var(--ph-s4) var(--ph-s3); }
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
/* library: covers edge to edge, the first one 2x2 with its name on the pink strip */
.ph-collage { display: grid; grid-template-columns: repeat(3, 1fr); margin: calc(-1 * var(--ph-s4)) calc(-1 * var(--ph-s4)) 0; max-width: 480px; }
@media (min-width: 600px) { .ph-collage { margin: 0; } }
.ph-collage__tile { position: relative; padding: 0; border: 0; background: #000; cursor: pointer; aspect-ratio: 1; overflow: hidden; }
.ph-collage__tile.is-big { grid-column: span 2; grid-row: span 2; }
.ph-collage__tile :deep(.ph-cover) { width: 100% !important; height: 100% !important; }
.ph-collage__label { position: absolute; left: 4px; right: 4px; bottom: 4px; display: flex; align-items: center; gap: 8px; padding: 2px 10px; border-radius: 4px;
  background: #C8008C; color: #fff; font: var(--hd-font-weight-bold) 13px/1.4 var(--hd-font-family-base); text-align: left; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.ph-libmenu { list-style: none; margin: 8px 0 0; padding: 0; max-width: 480px; }
.ph-libmenu button, .ph-libmenu__off { width: 100%; display: flex; align-items: center; gap: 8px; min-height: 49px; padding: 0 4px; border: 0; background: none; color: var(--hd-c-fg);
  font: var(--hd-font-weight-bold) var(--hd-font-size-large)/1.2 var(--hd-font-family-base); text-align: left; cursor: pointer; }
.ph-libmenu button:hover { background: var(--hd-c-pressed); }
.ph-libmenu__n { margin-left: auto; font: 400 var(--hd-font-size-small) var(--hd-font-family-base); color: var(--hd-c-fg-secondary); }
.ph-libmenu__off { cursor: default; color: var(--hd-c-fg-secondary); }
.ph-nowbar { position: sticky; bottom: 0; z-index: 50; margin: auto calc(-1 * var(--ph-s4)) calc(-1 * var(--ph-s4)); display: grid; align-items: center; column-gap: 16px;
  grid-template-columns: auto 1fr auto auto; grid-template-areas: "cover main ctl playon" "cover seek seek seek"; padding: 12px var(--ph-s4) 8px;
  background: #000; color: #fff; border-top: 1px solid #333; }
.ph-nowbar__cover { grid-area: cover; }
.ph-nowbar__main { grid-area: main; }
.ph-nowbar__ctl { grid-area: ctl; }
.ph-nowbar > .q-btn { grid-area: playon; }
.ph-nowbar__seek { grid-area: seek; }
.ph-nowbar__l1 { display: flex; gap: 8px; align-items: baseline; font: var(--hd-font-weight-bold) 17px/1.3 var(--hd-font-family-base); }
.ph-nowbar__l1 b { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ph-nowbar__n { margin-left: auto; font-size: 13px; font-weight: 400; color: #9A9A9E; }
.ph-nowbar__l2 { color: #9A9A9E; font-size: 15px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ph-nowbar__times { display: flex; justify-content: space-between; font-size: 13px; color: #9A9A9E; margin-top: -4px; }
.ph-nowbar :deep(.q-slider__selection) { background: #C8008C !important; }
.ph-nowbar :deep(.q-slider__thumb) { color: #C8008C !important; }
.ph-nowbar :deep(.q-slider__track) { background: #3A3A3C; }
.ph-nowbar > .q-btn { color: #fff !important; }
.ph-nowbar__l2 { color: #9A9A9E !important; }
.ph-np { width: 44px; height: 44px; display: grid; place-items: center; border: 0; background: none; color: #fff; cursor: pointer; border-radius: 8px; }
.ph-np:hover { background: rgba(255, 255, 255, .12); }
@media (max-width: 599px) {
  .ph-nowbar { grid-template-columns: 1fr auto; grid-template-areas: "main playon" "ctl ctl" "seek seek"; }
  .ph-nowbar__cover { display: none; }
  .ph-nowbar__ctl { justify-content: space-between; padding: 4px 8px; }
}
.ph-nowbar__main { min-width: 0; }
.ph-nowbar__ctl { display: flex; align-items: center; gap: 10px; }
</style>
