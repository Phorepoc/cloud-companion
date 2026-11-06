// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The feeds (the Feeds page, the Feeds widget and the Feeds part of Events, after the Etesian phone's and Plasma's Feeds), from the
// server's /phe/feeds (internal/webui/feeds.go): the owner's RSS and Atom feeds, newest posts first. Only the owner changes the list.
import { reactive, ref } from 'vue'
import { apiCall } from '../cloud'
import { getPref, loadPrefs, setPref } from '../prefs'

export interface Feed { url: string; title: string }
export interface Post { id: string; feed: string; feedURL: string; title: string; link: string; published: string; summary?: string; html?: string }

export const feedList = ref<Feed[]>([])
export const posts = ref<Post[]>([])
export const feedErrors = ref<Record<string, string>>({})
export const feedsError = ref('')

export async function loadFeeds() {
  try {
    await loadPrefs().then(loadRead, () => { /* read marks start empty */ })
    const j = await apiCall('GET', '/phe/feeds')
    feedList.value = j.feeds || []; posts.value = j.posts || []; feedErrors.value = j.errors || {}; feedsError.value = ''
  } catch (e) { feedsError.value = (e as Error).message }
}

/** Replaces the owner's list (the server reads each new address first) and reloads the posts. */
export async function saveFeeds(list: Feed[]) {
  const j = await apiCall('POST', '/phe/feeds', { feeds: list })
  feedList.value = j.feeds || []
  await loadFeeds()
}

/** Adds an address the owner pasted. */
export const addFeed = (url: string) => saveFeeds([...feedList.value, { url: url.trim(), title: '' }])
export const removeFeed = (url: string) => saveFeeds(feedList.value.filter((f) => f.url !== url))

/** Opens a web page in a new tab (never in this page: the desktop stays). */
export function openLink(u: string) { if (/^https?:\/\//.test(u)) window.open(u, '_blank', 'noopener') }

// read and unread, as on the phone: kept on the server with the rest of the person's state (prefs.ts), so every browser agrees
const READ_KEY = 'feeds.read', READ_MAX = 3000
const read = ref(new Set<string>())
function loadRead() { try { read.value = new Set(JSON.parse(getPref(READ_KEY) || '[]')) } catch { read.value = new Set() } }
function saveRead() { setPref(READ_KEY, JSON.stringify([...read.value].slice(-READ_MAX))) }
export const isUnread = (p: Post) => !read.value.has(p.id)
export function markRead(ids: string[], on = true) {
  const s = new Set(read.value)
  for (const id of ids) { if (on) s.add(id); else s.delete(id) }
  read.value = s; saveRead()
}
export const unreadOf = (feedURL?: string) => posts.value.filter((p) => (!feedURL || p.feedURL === feedURL) && isUnread(p)).length

/** Where the Feeds page is: the list of feeds, one feed's posts (or all of them), or one post. */
export const feedsAt = reactive<{ view: 'feeds' | 'posts' | 'post'; feed: string; post: string }>({ view: 'feeds', feed: 'all', post: '' })
/** Shows a post in the Feeds reader and marks it read; the caller opens the Feeds section. The page itself opens only from there. */
export function showPost(p: Post) {
  Object.assign(feedsAt, { view: 'post', feed: feedsAt.view === 'feeds' ? 'all' : feedsAt.feed, post: p.id })
  markRead([p.id])
}

/** A post's time as a Unix time in seconds (0 when the feed gave none), for `ago`. */
export const postTs = (p: Post) => Math.max(0, Math.floor(new Date(p.published).getTime() / 1000) || 0)
