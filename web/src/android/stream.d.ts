// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Types of stream.js, which stays JavaScript: the Android bridge serves the same file to browsers for its standalone page
// (cmd/pheandroid, kept identical by a Go test). Keep this in step with the methods the web app calls.

export type StreamState = 'idle' | 'connecting' | 'starting' | 'live' | 'closed' | 'error'

/** The bridge's statistics frame, with the measured round trip and the video size added. */
export interface StreamStats { fps: number; kbps: number; rtt: number; w: number; h: number; i2f_n?: number; i2f_ms: number; [k: string]: unknown }

export interface StreamOptions {
  base?: string // where the bridge is: '/android/' behind phecloud, './' on its own page
  onState?: (state: StreamState, message: string) => void
  onStats?: (stats: StreamStats) => void
  onNote?: (text: string) => void
  onSize?: (w: number, h: number) => void
}

/** Query options of a connection (all optional; the bridge has defaults). */
export interface ConnectOptions { codec?: string; size?: string | number; fps?: string | number; kbps?: string | number; kind?: string; audio?: boolean }

export class AndroidStream {
  constructor(canvas: HTMLCanvasElement, opts?: StreamOptions)
  state: StreamState
  connect(ticket: string, opts?: ConnectOptions): void
  close(quiet?: boolean): void
  dispose(): void
  key(code: number, meta?: number): void
  setSound(on: boolean): void
  copyFromPhone(): Promise<void>
  pasteToPhone(): Promise<void>
  installApk(file: File): Promise<void>
  installFdroid(id: string): Promise<void>
}
