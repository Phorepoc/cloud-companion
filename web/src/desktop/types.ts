// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

/** Where a window is: floating at its own rectangle, snapped to a half, or filling the desktop. */
export type WinState = 'float' | 'left' | 'right' | 'max'

/** A desktop window (stored on the device): its section, its floating rectangle in desktop-area coordinates, and its state. */
export interface Win { id: string; x: number; y: number; w: number; h: number; state: WinState; z: number; min: boolean }

export interface Rect { x: number; y: number; w: number; h: number }
