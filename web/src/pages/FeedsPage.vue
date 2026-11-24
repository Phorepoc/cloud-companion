<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: AGPL-3.0-only -->
<!-- Feeds, after the Etesian phone's Feeds app: the feeds (All posts first), then the posts of all of them or of one, then one
     post in the reader. A post opens here, never straight to its web page: Open in browser (or a link in the post) does that.
     Read and unread follow the person between browsers (desktop/feeds.ts). Only the owner adds and removes feeds, and shares them
     with feed readers (Share: off, public or private with a token, as catalogues are; internal/webui/feedshare.go). -->
<template>
  <q-page class="ph-feeds" data-test="feeds">
    <p v-if="feedsError" class="ph-feeds__note" role="alert">Could not load: {{ feedsError }}</p>
    <p v-if="removeFail" class="ph-feeds__note" role="alert" data-test="feeds-remove-error">{{ removeFail }}</p>
    <template v-if="feedsAt.view === 'feeds'">
      <XFeedList :feeds="sources" @open="openFeed" />
      <p v-if="!feedList.length" class="ph-feeds__note">{{ state.admin ? 'No feeds yet. Add one with +.' : 'No feeds yet. The owner adds them.' }}</p>
    </template>
    <XFeedPosts v-else-if="feedsAt.view === 'posts'" :title="feedTitle" :show-feed="feedsAt.feed === 'all'" :posts="shown" @open="openPost" />
    <XFeedReader v-else-if="post" :post="asPost(post)" @link="openLink" />
    <p v-else class="ph-feeds__note">This post is no longer in its feed.</p>

    <q-dialog v-model="adding">
      <q-card class="ph-feeds__add" data-test="feeds-add">
        <q-card-section><b>Add feed</b></q-card-section>
        <q-card-section>
          <q-input v-model="address" dense outlined autofocus label="Feed or website address" placeholder="https://example.org/feed" data-test="feeds-add-url" @keyup.enter="add" />
          <p v-if="addFail" class="ph-feeds__fail" role="alert" data-test="feeds-add-error">{{ addFail }}</p>
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat no-caps label="Cancel" @click="adding = false" />
          <q-btn unelevated no-caps color="primary" label="Add" :loading="busy" data-test="feeds-add-save" @click="add" />
        </q-card-actions>
      </q-card>
    </q-dialog>
    <q-dialog v-model="sharing">
      <q-card class="ph-feeds__add" data-test="feeds-share">
        <q-card-section><b>Share with feed readers</b></q-card-section>
        <q-card-section>
          <p class="ph-feeds__hint">Feed readers, like Feeds on an Etesian phone, can follow these feeds and their posts. Turned off, nothing is shared.</p>
          <q-toggle :model-value="share.mode !== 'off'" label="Share the feeds" data-test="feeds-share-on" @update:model-value="(v: boolean) => setShare({ mode: v ? 'private' : 'off' })" />
          <template v-if="share.mode !== 'off'">
            <q-toggle :model-value="share.mode === 'public'" label="Public (anyone with the address can read them)" data-test="feeds-share-public" @update:model-value="(v: boolean) => setShare({ mode: v ? 'public' : 'private' })" />
            <p v-if="share.mode === 'private'" class="ph-feeds__hint">
              Private. The addresses contain a secret token, and anyone with an address can read the feeds.
              <q-btn flat no-caps color="primary" label="New token" data-test="feeds-share-rotate" @click="setShare({ rotate_token: true })" />
            </p>
            <div v-for="a in shareAddresses" :key="a.key" class="ph-feeds__addr">
              <span class="ph-feeds__addr-label">{{ a.label }}</span>
              <code :data-test="'feeds-share-' + a.key">{{ a.url }}</code>
              <q-btn flat dense no-caps icon="sym_o_content_copy" :aria-label="'Copy the ' + a.label + ' address'" @click="copy(a.url)" />
            </div>
          </template>
          <p v-if="shareFail" class="ph-feeds__fail" role="alert">{{ shareFail }}</p>
        </q-card-section>
        <q-card-actions align="right"><q-btn flat no-caps label="Close" @click="sharing = false" /></q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useChrome, XFeedList, XFeedPosts, XFeedReader, type FeedPost, type FeedSource } from '@etesian-design/shell'
import { apiCall, state } from '../cloud'
import { onResume, toast } from '../toast'
import { feedList, posts, feedErrors, feedsError, loadFeeds, addFeed, removeFeed, openLink, isUnread, markRead, unreadOf, feedsAt, showPost, type Post } from '../desktop/feeds'

const when = (p: Post) => { const d = new Date(p.published); return isNaN(d.getTime()) ? '' : d.toLocaleString(undefined, { day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' }) }
const asPost = (p: Post): FeedPost => ({ id: p.id, title: p.title || '(no title)', feed: p.feed, when: when(p), unread: isUnread(p), html: p.html, link: p.link })

const hidden = ref<string[]>([]) // removed, Undo still possible
// each feed its own colour, as in the feeds-app story (the same feed keeps it), and the time of its newest post on the right
const TINTS = ['#FF6A00', '#2FA8CC', '#7A3FD0', '#3D9A36', '#D6222A', '#0067BC', '#C2185B', '#8A6D1F']
const tintOf = (u: string) => TINTS[[...u].reduce((h, c) => (h * 31 + c.charCodeAt(0)) >>> 0, 7) % TINTS.length]
const newest = (u: string) => posts.value.filter((p) => p.feedURL === u).reduce((m, p) => (p.published > m ? p.published : m), '')
const updated = (u: string) => {
  const t = newest(u); if (!t) return undefined
  const d = new Date(t); return d.toDateString() === new Date().toDateString() ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : d.toLocaleDateString([], { day: '2-digit', month: '2-digit' })
}
const sources = computed<FeedSource[]>(() => feedList.value.filter((f) => !hidden.value.includes(f.url)).map((f) => ({ id: f.url, title: f.title || f.url, color: tintOf(f.url), updated: updated(f.url), unread: unreadOf(f.url), error: feedErrors.value[f.url] ? "Can't update" : undefined })))
const feedTitle = computed(() => (feedsAt.feed === 'all' ? 'All posts' : feedList.value.find((f) => f.url === feedsAt.feed)?.title || feedsAt.feed))
const inFeed = computed(() => posts.value.filter((p) => feedsAt.feed === 'all' || p.feedURL === feedsAt.feed))
const shown = computed(() => inFeed.value.map(asPost))
const post = computed(() => posts.value.find((p) => p.id === feedsAt.post))

function openFeed(id: string) { Object.assign(feedsAt, { view: 'posts', feed: id }) }
function openPost(id: string) { const p = posts.value.find((x) => x.id === id); if (p) showPost(p) }
function back() { feedsAt.view = feedsAt.view === 'post' ? 'posts' : 'feeds' }

const adding = ref(false), address = ref(''), busy = ref(false), addFail = ref('')
async function add() {
  if (!address.value.trim()) return
  busy.value = true; addFail.value = ''
  try { await addFeed(address.value); adding.value = false; address.value = '' } catch (e) { addFail.value = (e as Error).message } finally { busy.value = false }
}

// sharing with feed readers (owner only): off, public, or private with a token in the address
type Share = { mode: 'off' | 'public' | 'private'; addresses?: { index: string; opml: string; atom: string } }
const sharing = ref(false), share = ref<Share>({ mode: 'off' }), shareFail = ref('')
const shareAddresses = computed(() => {
  const a = share.value.addresses
  return a ? [{ key: 'index', label: 'Etesian Feeds', url: a.index }, { key: 'opml', label: 'OPML list', url: a.opml }, { key: 'atom', label: 'Atom feed', url: a.atom }] : []
})
async function openShare() {
  shareFail.value = ''; sharing.value = true
  try { share.value = await apiCall('GET', '/phe/feeds/share') } catch (e) { shareFail.value = (e as Error).message }
}
async function setShare(body: Record<string, unknown>) {
  shareFail.value = ''
  try { share.value = await apiCall('POST', '/phe/feeds/share', body) } catch (e) { shareFail.value = (e as Error).message }
}
function copy(t: string) { navigator.clipboard?.writeText(t).then(() => toast('Address copied'), () => {}) }

// the header, as on the phone: + and Refresh on the feeds; Refresh, Mark all as read (and the owner's Remove feed) on the posts;
// Open in browser and Mark as unread on a post
const chrome = useChrome(() => ({
  title: feedsAt.view === 'feeds' ? 'Feeds' : feedsAt.view === 'posts' ? feedTitle.value : post.value?.feed || 'Feeds',
  back: feedsAt.view !== 'feeds',
  tools: feedsAt.view === 'feeds' ? [...(state.admin ? [{ icon: 'add', label: 'Add feed' }, { icon: 'share', label: 'Share' }] : []), { icon: 'refresh', label: 'Refresh' }]
    : feedsAt.view === 'posts' ? [{ icon: 'refresh', label: 'Refresh' }, { icon: 'check', label: 'Mark all as read' }, ...(state.admin && feedsAt.feed !== 'all' ? [{ icon: 'delete', label: 'Remove feed' }] : [])]
      : [{ icon: 'globe', label: 'Open in browser' }, { icon: 'undo', label: 'Mark as unread' }],
}))
chrome.on('back', back)
chrome.on('tool', async (l) => {
  if (l === 'Add feed') { addFail.value = ''; adding.value = true }
  else if (l === 'Share') openShare()
  else if (l === 'Refresh') loadFeeds()
  else if (l === 'Mark all as read') markRead(inFeed.value.map((p) => p.id))
  else if (l === 'Remove feed') remove(feedsAt.feed)
  else if (l === 'Open in browser' && post.value) openLink(post.value.link)
  else if (l === 'Mark as unread' && post.value) { markRead([post.value.id], false); feedsAt.view = 'posts' }
})

// removing waits for the toast, as every other delete does: Undo keeps the feed; an error is a banner, not a toast
const removeFail = ref('')
function remove(url: string) {
  const f = feedList.value.find((x) => x.url === url)
  if (!f) return
  removeFail.value = ''
  hidden.value = [...hidden.value, url]
  feedsAt.view = 'feeds'
  toast(`${f.title || f.url} removed`, {
    resume: ['feeds.remove', url],
    undo: () => { hidden.value = hidden.value.filter((u) => u !== url) },
    onExpire: async () => {
      try { await removeFeed(url) } catch (e) { removeFail.value = "Couldn't remove the feed: " + (e as Error).message }
      hidden.value = hidden.value.filter((u) => u !== url)
    },
  })
}

onResume<string>('feeds.remove', async (url) => { await loadFeeds(); await removeFeed(url) })
onMounted(loadFeeds)
watch(() => state.status === 'online', (on) => { if (on) loadFeeds() })
</script>

<style scoped>
.ph-feeds { background: var(--hd-c-bg); }
.ph-feeds :deep(.x-fr) { max-width: 760px; }
.ph-feeds__note { margin: 16px; color: var(--hd-c-fg-secondary); }
.ph-feeds__add { width: min(420px, 92vw); }
.ph-feeds__hint { margin: 0 0 8px; color: var(--hd-c-fg-secondary); }
.ph-feeds__addr { display: flex; align-items: center; gap: 8px; margin-top: 8px; }
.ph-feeds__addr-label { flex: 0 0 96px; color: var(--hd-c-fg-secondary); }
.ph-feeds__addr code { flex: 1; min-width: 0; overflow-wrap: anywhere; font-size: 13px; }
.ph-feeds__fail { margin: 8px 0 0; color: var(--ph-error, #D6222A); }
</style>
