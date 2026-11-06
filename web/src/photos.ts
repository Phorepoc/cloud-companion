// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Photos on the server (internal/webui/photos.go): Immich accounts and the owner's photo folders, in one timeline and albums.
import { apiCall } from './cloud'

/** src: "im:<account>" (Immich) or "f" (a photo folder; id is the file's path, as in Files). */
export interface Photo { src: string; id: string; name: string; taken: string; video?: boolean; w?: number; h?: number; type?: string }
export interface PhotoAlbum { src: string; id: string; name: string; count: number; cover?: string; cover_src?: string }
interface Page { items: Photo[]; more: boolean; ticket: string }

let ticket = ''
const q = (p: string) => encodeURIComponent(p)
const keep = <T extends { ticket: string }>(j: T) => { ticket = j.ticket; return j }

export const loadTimeline = async (before = ''): Promise<Page> => keep(await apiCall('GET', '/phe/photos?n=150' + (before ? '&before=' + q(before) : '')))
export const loadAlbum = async (a: { src: string; id: string }, before = ''): Promise<Page> =>
  keep(await apiCall('GET', `/phe/photos/album?n=150&src=${q(a.src)}&id=${q(a.id)}` + (before ? '&before=' + q(before) : '')))
export const loadAlbums = async (): Promise<PhotoAlbum[]> => keep<{ albums: PhotoAlbum[]; ticket: string }>(await apiCall('GET', '/phe/photos/albums')).albums
export const photoFolders = async (): Promise<string[]> => (await apiCall('GET', '/phe/photos/folders')).folders
export const setPhotoFolders = async (folders: string[]): Promise<string[]> => (await apiCall('POST', '/phe/photos/folders', { folders })).folders

/** The picture's address for <img>/<video>: size "thumb", "preview" or "" for the original. A photo folder's files have one size. */
export function photoURL(p: { src: string; id: string; video?: boolean }, size: 'thumb' | 'preview' | '' = 'thumb') {
  return `/phe/photos/img?t=${q(ticket)}&src=${q(p.src)}&id=${q(p.id)}` + (p.src === 'f' ? '' : `&size=${size}`) + (p.video && !size ? '&video=1' : '')
}
/** A folder's video has no picture of its own to show in a grid. */
export const hasThumb = (p: Photo) => !(p.src === 'f' && p.video)
