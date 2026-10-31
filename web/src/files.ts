// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Files and Music on the server (internal/webui/files.go): the owner's own storage, shared by every paired device.
import { apiCall, apiUpload } from './cloud'

export interface FileEntry { name: string; path: string; dir: boolean; size: number; mod: string; type?: string }
export interface Share { token: string; path: string; created: string; expires?: string }
export interface Track { path: string; title: string; artist: string; album: string; n?: number; cover?: string; size: number }

const q = (p: string) => encodeURIComponent(p)
export const listFiles = async (path: string): Promise<{ path: string; entries: FileEntry[] }> => apiCall('GET', '/phe/files?path=' + q(path))
export const makeFolder = (path: string) => apiCall('POST', '/phe/files/mkdir', { path })
export const moveFile = (from: string, to: string) => apiCall('POST', '/phe/files/move', { from, to })
export const deleteFile = (path: string) => apiCall('POST', '/phe/files/delete', { path })
export async function uploadFiles(dir: string, list: FileList | File[]) {
  const form = new FormData()
  for (const f of Array.from(list)) form.append('file', f, f.name)
  return apiUpload('/phe/files/upload?path=' + q(dir), form)
}

// the browser's own players and links cannot sign requests: a ticket opens one file for 12 hours (kept here, per path)
const tickets = new Map<string, { url: string; at: number }>()
export async function fileURL(path: string, download = false): Promise<string> {
  let t = tickets.get(path)
  if (!t || Date.now() - t.at > 11 * 3600e3) {
    const j = await apiCall('POST', '/phe/files/ticket', { path })
    t = { url: j.url, at: Date.now() }
    tickets.set(path, t)
  }
  return t.url + (download ? '&dl=1' : '')
}

export const listShares = async (): Promise<Share[]> => (await apiCall('GET', '/phe/files/shares')).shares || []
export async function shareFile(path: string, days: number): Promise<string> {
  const j = await apiCall('POST', '/phe/files/share', { path, days })
  return location.origin + j.url
}
export const unshare = (token: string) => apiCall('POST', '/phe/files/unshare', { token })
export const shareLink = (s: Share) => location.origin + '/s/' + s.token

export const loadTracks = async (): Promise<Track[]> => (await apiCall('GET', '/phe/music')).tracks || []
export const castTo = (device: string, paths: string[], index: number) => apiCall('POST', '/phe/music/cast', { device, paths, index })

/** "12 KB", "3.4 MB" */
export function size(n: number) {
  if (n < 1024) return n + ' B'
  const u = ['KB', 'MB', 'GB', 'TB']
  let i = -1
  do { n /= 1024; i++ } while (n >= 1024 && i < u.length - 1)
  return (n < 10 ? n.toFixed(1) : Math.round(n)) + ' ' + u[i]
}

/** an icon for a file, from its type */
export function iconOf(e: FileEntry) {
  if (e.dir) return 'sym_o_folder'
  const t = e.type || ''
  if (t.startsWith('image/')) return 'sym_o_image'
  if (t.startsWith('audio/')) return 'sym_o_music_note'
  if (t.startsWith('video/')) return 'sym_o_movie'
  if (t === 'application/pdf') return 'sym_o_picture_as_pdf'
  if (t.startsWith('text/')) return 'sym_o_description'
  if (/zip|tar|compress|7z|rar/.test(t)) return 'sym_o_folder_zip'
  return 'sym_o_draft'
}
