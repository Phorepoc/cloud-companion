// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The desktop's feeds (the Feeds widget and the Feeds part of Events, after the Etesian phone's and Plasma's Feeds), from the
// server's /phe/feeds (internal/webui/feeds.go): the owner's RSS and Atom feeds, newest posts first. Only the owner changes the list.
import { ref } from 'vue'
import { apiCall } from '../cloud'

export interface Feed { url: string; title: string }
export interface Post { feed: string; feedURL: string; title: string; link: string; published: string; summary?: string }

export const feedList = ref<Feed[]>([])
export const posts = ref<Post[]>([])
export const feedErrors = ref<Record<string, string>>({})
export const feedsError = ref('')

export async function loadFeeds() {
  try {
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

/** Opens a post in a new tab (never in this page: the desktop stays). */
export function openPost(p: Post) { if (/^https?:\/\//.test(p.link)) window.open(p.link, '_blank', 'noopener') }

/** A post's time as a Unix time in seconds (0 when the feed gave none), for `ago`. */
export const postTs = (p: Post) => Math.max(0, Math.floor(new Date(p.published).getTime() / 1000) || 0)
