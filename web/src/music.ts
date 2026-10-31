// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The music player: one audio element for the whole app, so music keeps playing while you move between sections and windows.
// Songs stream from the owner's files (Range requests: seeking works at once). The system's media keys and lock-screen controls
// work through the Media Session API. "Play on" hands the queue to another device, which checks every few seconds.
import { reactive } from 'vue'
import { apiCall } from './cloud'
import { fileURL, type Track } from './files'

export const player = reactive({
  queue: [] as Track[],
  i: -1,
  playing: false,
  pos: 0,
  dur: 0,
  error: '',
  remote: '', // the device that last sent music here
})

let audio: HTMLAudioElement | null = null
function el() {
  if (audio) return audio
  audio = new Audio()
  audio.preload = 'auto'
  audio.addEventListener('play', () => { player.playing = true })
  audio.addEventListener('pause', () => { player.playing = false })
  audio.addEventListener('timeupdate', () => { player.pos = audio!.currentTime })
  audio.addEventListener('durationchange', () => { player.dur = isFinite(audio!.duration) ? audio!.duration : 0 })
  audio.addEventListener('ended', () => { if (player.i < player.queue.length - 1) go(player.i + 1); else player.playing = false })
  audio.addEventListener('error', () => { player.error = 'This song cannot be played here.'; player.playing = false })
  const ms = navigator.mediaSession
  if (ms) {
    ms.setActionHandler('play', () => toggle(true))
    ms.setActionHandler('pause', () => toggle(false))
    ms.setActionHandler('previoustrack', prev)
    ms.setActionHandler('nexttrack', next)
  }
  return audio
}

export const current = () => (player.i >= 0 ? player.queue[player.i] : undefined)

async function go(i: number) {
  const t = player.queue[i]
  if (!t) return
  player.i = i
  player.error = ''
  player.pos = 0
  player.dur = 0
  const a = el()
  try {
    a.src = await fileURL(t.path)
    await a.play()
  } catch (e) {
    if ((e as Error).name !== 'AbortError') { player.error = (e as Error).name === 'NotAllowedError' ? 'Press play to start.' : (e as Error).message; player.playing = false }
  }
  if (navigator.mediaSession && 'MediaMetadata' in window) {
    const art = t.cover ? [{ src: await fileURL(t.cover).catch(() => ''), sizes: '512x512' }].filter((x) => x.src) : []
    navigator.mediaSession.metadata = new MediaMetadata({ title: t.title, artist: t.artist, album: t.album, artwork: art })
  }
}

/** play `queue` from song `i` */
export function play(queue: Track[], i = 0) { player.queue = queue.slice(); return go(i) }
export function toggle(on?: boolean) {
  if (!current()) return
  const a = el()
  if (on ?? a.paused) a.play().catch(() => { /* needs a gesture */ })
  else a.pause()
}
export function next() { if (player.i < player.queue.length - 1) go(player.i + 1) }
export function prev() {
  if (audio && audio.currentTime > 3) audio.currentTime = 0
  else if (player.i > 0) go(player.i - 1)
}
export function seek(s: number) { if (audio) audio.currentTime = s }
export function stop() { audio?.pause(); player.queue = []; player.i = -1 }

/** hand what is playing to another device; it stops here */
export async function playOn(device: string) {
  if (!player.queue.length) return
  await apiCall('POST', '/phe/music/cast', { device, paths: player.queue.map((t) => t.path), index: Math.max(0, player.i) })
  audio?.pause()
}

/** listen for music sent from another device (every 5 s while the app is open) */
let polling = 0
export function listenForRemote(tracks: () => Promise<Track[]>) {
  if (polling) return
  const tick = async () => {
    try {
      const j = await apiCall('GET', '/phe/music/remote')
      if (j.cast) {
        const all = await tracks()
        const by = new Map(all.map((t) => [t.path, t]))
        const q = (j.cast.paths as string[]).map((p) => by.get(p) || { path: p, title: p.split('/').pop() || p, artist: '', album: '', size: 0 })
        player.remote = j.cast.from
        play(q, j.cast.index)
      }
    } catch { /* offline or signed out: try again later */ }
  }
  polling = window.setInterval(tick, 5000)
  tick()
}

export const mmss = (s: number) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, '0')}`

// for the tests (web/test/files.ts): what this browser is playing
Object.defineProperty(window, '__pheMusic', { configurable: true, get: () => ({ title: current()?.title, playing: player.playing }) })
