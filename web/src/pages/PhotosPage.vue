<!--
  Copyright The Phorepoc Authors
  SPDX-License-Identifier: Apache-2.0
-->
<template>
  <q-page class="ph-page ph-photos" data-test="photos-page">
    <!-- in a host with a header (the classic bar, a desktop window) the title, Timeline | Albums, Back from an album and Photo folders
         are there (useChrome); without one the page draws them itself -->
    <div v-if="!chrome.hosted" class="ph-head">
      <h1>{{ album ? album.name : 'Photos' }}</h1>
      <span class="ph-head__acts">
        <q-btn v-if="state.admin && !album" flat no-caps icon="sym_o_folder_special" label="Photo folders" data-test="photos-folders" @click="openFolders" />
      </span>
    </div>
    <template v-if="!chrome.hosted">
    <nav v-if="!album" class="ph-tabs" aria-label="Photos">
      <button type="button" class="hd-sqh" :aria-current="tab === 'timeline' ? 'page' : undefined" data-test="photos-tab-timeline" @click="tab = 'timeline'">Timeline</button>
      <button type="button" class="hd-sqh" :aria-current="tab === 'albums' ? 'page' : undefined" data-test="photos-tab-albums" @click="tab = 'albums'">Albums</button>
    </nav>
    <q-btn v-else flat no-caps icon="sym_o_arrow_back" label="Albums" class="ph-back" data-test="photos-back" @click="closeAlbum" />
    </template>

    <q-banner v-if="error" class="ph-note is-error q-mb-md" rounded role="alert">
      <template #avatar><q-icon name="sym_o_error" aria-hidden="true" /></template>
      {{ error }}
      <template #action><q-btn flat label="Try again" @click="reload" /></template>
    </q-banner>
    <HdStatusState v-if="loading && !items.length && !albums.length" state="loading" title="Loading photos" text="This takes a moment." />

    <template v-if="tab === 'albums' && !album">
      <HdStatusState v-if="!loading && !albums.length && !error" icon="photo" title="No albums yet" data-test="photos-no-albums"
                  text="Albums from Immich and your photo folders appear here." />
      <div v-else class="ph-palbums" data-test="photos-albums">
        <button v-for="a in albums" :key="a.src + a.id" type="button" class="ph-palbum" :data-test="'photos-album-' + a.name" @click="openAlbum(a)">
          <img v-if="a.cover" class="hd-squircle" :src="photoURL({ src: a.cover_src || a.src, id: a.cover })" alt="" loading="lazy">
          <span v-else class="ph-palbum__none hd-squircle" aria-hidden="true"><q-icon name="sym_o_photo_library" size="40px" /></span>
          <b>{{ a.name }}</b><span class="ph-small ph-muted">{{ a.count === 1 ? '1 item' : a.count + ' items' }} · {{ a.src === 'f' ? 'Folder' : 'Immich' }}</span>
        </button>
      </div>
    </template>
    <template v-else>
      <HdStatusState v-if="!loading && !items.length && !error" icon="photo" :title="album ? 'This album is empty' : 'No photos yet'" data-test="photos-empty"
                  :text="album ? '' : state.admin ? 'Connect Immich in Connections, or mark folders in Files as photo folders.' : 'The owner can connect Immich, or mark folders as photo folders.'" />
      <section v-for="g in groups" :key="g.title" class="ph-month">
        <h2 class="ph-small">{{ g.title }}</h2>
        <div class="ph-grid" data-test="photos-grid">
          <button v-for="p in g.items" :key="p.src + p.id" type="button" class="ph-thumb hd-squircle" :aria-label="p.name" :data-test="'photo-' + p.name" @click="view(items.indexOf(p))">
            <img v-if="hasThumb(p)" :src="photoURL(p)" alt="" loading="lazy">
            <q-icon v-else name="sym_o_movie" size="32px" aria-hidden="true" />
            <q-icon v-if="p.video" class="ph-thumb__play" name="sym_o_play_circle" size="24px" aria-hidden="true" />
          </button>
        </div>
      </section>
      <div v-if="more" ref="sentinel" class="ph-more"><q-btn flat no-caps label="Show older" :loading="loading" data-test="photos-more" @click="loadMore" /></div>
    </template>

    <q-dialog v-model="viewer.open" maximized transition-show="fade" transition-hide="fade">
      <div class="ph-viewer" data-test="photos-viewer">
        <div class="ph-viewer__bar">
          <span class="ph-viewer__name">{{ current?.name }}<span class="ph-viewer__when"> · {{ current ? new Date(current.taken).toLocaleString() : '' }}</span></span>
          <q-btn flat round icon="sym_o_download" aria-label="Download" :href="current ? photoURL(current, '') : undefined" :download="current?.name" data-test="photos-download" />
          <q-btn flat round icon="sym_o_close" aria-label="Close" v-close-popup data-test="photos-close" />
        </div>
        <div class="ph-viewer__stage">
          <q-btn v-if="viewer.i > 0" flat round class="ph-viewer__nav is-prev" icon="sym_o_chevron_left" size="lg" aria-label="Newer" data-test="photos-prev" @click="step(-1)" />
          <video v-if="current?.video" :key="current.id" :src="photoURL(current, '')" controls autoplay playsinline data-test="photos-video" />
          <img v-else-if="current" :key="current.id" :src="photoURL(current, 'preview')" :alt="current.name" data-test="photos-image">
          <q-btn v-if="viewer.i < items.length - 1" flat round class="ph-viewer__nav is-next" icon="sym_o_chevron_right" size="lg" aria-label="Older" data-test="photos-next" @click="step(1)" />
        </div>
      </div>
    </q-dialog>

    <q-dialog v-model="folders.open">
      <q-card class="ph-dialog" data-test="photos-folders-dialog">
        <q-card-section><h2 class="text-h6 q-my-none">Photo folders</h2>
          <p class="ph-small ph-muted">Photos and videos in these folders, and the folders in them, show in Photos. Mark more from a folder's menu in Files.</p>
          <ul v-if="folders.list.length" class="ph-list">
            <li v-for="p in folders.list" :key="p" class="ph-frow"><span class="ph-row-title">{{ p }}</span>
              <q-btn flat dense no-caps color="negative" label="Remove" :data-test="'photos-folder-remove-' + p" @click="removeFolder(p)" /></li>
          </ul>
          <p v-else class="ph-muted" data-test="photos-folders-none">None yet.</p>
          <p v-if="folders.error" class="text-negative" role="alert">{{ folders.error }}</p></q-card-section>
        <q-card-actions align="right"><q-btn flat label="Done" v-close-popup /></q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { HdStatusState } from '@etesian-design/vue'
import { useChrome } from '@etesian-design/shell'
import { state } from '../cloud'
import { hasThumb, loadAlbum, loadAlbums, loadTimeline, photoFolders, photoURL, setPhotoFolders, type Photo, type PhotoAlbum } from '../photos'

const tab = ref<'timeline' | 'albums'>('timeline')
const album = ref<PhotoAlbum | null>(null)
const items = ref<Photo[]>([]), albums = ref<PhotoAlbum[]>([])
const more = ref(false), loading = ref(false), error = ref('')
const sentinel = ref<HTMLElement | null>(null)

const groups = computed(() => {
  const out: { title: string; items: Photo[] }[] = []
  for (const p of items.value) {
    const title = new Date(p.taken).toLocaleDateString([], { month: 'long', year: 'numeric' })
    if (out.at(-1)?.title !== title) out.push({ title, items: [] })
    out.at(-1)!.items.push(p)
  }
  return out
})

async function fetchPage(before = '') { return album.value ? loadAlbum(album.value, before) : loadTimeline(before) }
async function reload() {
  loading.value = true; error.value = ''
  try {
    if (tab.value === 'albums' && !album.value) albums.value = await loadAlbums()
    else { const j = await fetchPage(); items.value = j.items; more.value = j.more }
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
async function loadMore() {
  if (loading.value || !more.value) return
  loading.value = true
  try { const j = await fetchPage(items.value.at(-1)?.taken); items.value.push(...j.items); more.value = j.more }
  catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
function openAlbum(a: PhotoAlbum) { album.value = a; items.value = []; reload() }
function closeAlbum() { album.value = null; items.value = []; reload() }

// the header: the album's name with Back, or Photos with its two views
const VIEWS: Record<string, 'timeline' | 'albums'> = { Timeline: 'timeline', Albums: 'albums' }
const chrome = useChrome(() => ({
  title: album.value ? album.value.name : 'Photos',
  back: !!album.value,
  views: album.value ? undefined : Object.keys(VIEWS),
  view: tab.value === 'albums' ? 'Albums' : 'Timeline',
  tools: state.admin && !album.value ? [{ icon: 'folder', label: 'Photo folders' }] : [],
}))
chrome.on('back', closeAlbum)
chrome.on('view', (v) => { const t = v ? VIEWS[v] : undefined; if (t) tab.value = t })
chrome.on('tool', (l) => { if (l === 'Photo folders') openFolders() })
watch(tab, () => { items.value = []; reload() })
watch(() => state.status === 'online', (on) => { if (on) reload() }, { immediate: true })

// older photos load as the end of the grid scrolls into view
let io: IntersectionObserver | undefined
watch(sentinel, (el) => { io?.disconnect(); if (el) { io = new IntersectionObserver((e) => { if (e[0]?.isIntersecting) loadMore() }); io.observe(el) } })
onBeforeUnmount(() => io?.disconnect())

const viewer = reactive({ open: false, i: 0 })
const current = computed(() => (viewer.open ? items.value[viewer.i] : undefined))
function view(i: number) { viewer.i = i; viewer.open = true }
// the arrow keys go through the photos while the viewer is open
const onKey = (e: KeyboardEvent) => { if (e.key === 'ArrowLeft') step(-1); else if (e.key === 'ArrowRight') step(1) }
watch(() => viewer.open, (open) => (open ? window.addEventListener('keydown', onKey) : window.removeEventListener('keydown', onKey)))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
function step(d: number) {
  const i = viewer.i + d
  if (i >= 0 && i < items.value.length) viewer.i = i
  if (i >= items.value.length - 3) loadMore()
}

const folders = reactive({ open: false, list: [] as string[], error: '' })
async function openFolders() { folders.error = ''; folders.open = true; try { folders.list = await photoFolders() } catch (e) { folders.error = (e as Error).message } }
async function removeFolder(p: string) {
  try { folders.list = await setPhotoFolders(folders.list.filter((x) => x !== p)); reload() } catch (e) { folders.error = (e as Error).message }
}
</script>

<style scoped>
.ph-head { display: flex; align-items: center; justify-content: space-between; gap: var(--ph-s3); margin-bottom: var(--ph-s3); flex-wrap: wrap; }
.ph-tabs { display: flex; gap: var(--ph-s2); margin-bottom: var(--ph-s4); }
.ph-tabs button { padding: 6px 18px; border: 0; outline-offset: 2px; background: var(--ph-surface-2); color: var(--ph-text); font: inherit; cursor: pointer; }
.ph-tabs button[aria-current] { background: #7A3FD0; color: #fff; font-weight: 600; }
.ph-back { align-self: flex-start; margin-bottom: var(--ph-s3); }
.ph-month h2 { margin: var(--ph-s4) 0 var(--ph-s2); font-weight: 600; color: var(--ph-muted, inherit); }
.ph-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(104px, 1fr)); gap: 6px; }
.ph-thumb { position: relative; aspect-ratio: 1; padding: 0; border: 0; overflow: hidden; cursor: pointer; display: grid; place-items: center;
  background: var(--ph-surface-2); color: var(--ph-text); }
.ph-thumb img { width: 100%; height: 100%; object-fit: cover; display: block; }
.ph-thumb__play { position: absolute; right: 6px; bottom: 6px; color: #fff; filter: drop-shadow(0 0 2px rgba(0, 0, 0, 0.7)); }
.ph-more { display: flex; justify-content: center; padding: var(--ph-s4); }
.ph-palbums { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: var(--ph-s4); }
.ph-palbum { display: flex; flex-direction: column; align-items: flex-start; gap: 2px; padding: 0; border: 0; background: none; color: var(--ph-text); font: inherit; text-align: left; cursor: pointer; min-width: 0; }
.ph-palbum img, .ph-palbum__none { width: 150px; max-width: 100%; aspect-ratio: 1; object-fit: cover; display: grid; place-items: center; background: var(--ph-surface-2); margin-bottom: 4px; }
.ph-palbum b { max-width: 150px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ph-viewer { display: flex; flex-direction: column; width: 100%; height: 100%; background: #000; color: #fff; }
.ph-viewer__bar { display: flex; align-items: center; gap: var(--ph-s2); padding: var(--ph-s2) var(--ph-s3); }
.ph-viewer__name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 600; }
.ph-viewer__when { font-weight: 400; color: #B8B8BC; }
.ph-viewer__stage { position: relative; flex: 1; min-height: 0; display: grid; place-items: center; }
.ph-viewer__stage img, .ph-viewer__stage video { max-width: 100%; max-height: 100%; object-fit: contain; }
.ph-viewer__nav { position: absolute; top: 50%; transform: translateY(-50%); background: rgba(0, 0, 0, 0.45); color: #fff; }
.ph-viewer__nav.is-prev { left: 8px; }
.ph-viewer__nav.is-next { right: 8px; }
.ph-list { list-style: none; padding: 0; margin: 0; }
.ph-frow { display: flex; align-items: center; justify-content: space-between; gap: var(--ph-s2); padding: 6px 0; border-bottom: 1px solid var(--ph-divider); }
</style>
