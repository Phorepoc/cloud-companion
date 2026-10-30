// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package server

// clampViewport keeps a requested viewport in a sane range: at least a small phone's width, at most a large desktop window (the page is cut into tiles and sent, so
// the size is also what the network and the browser have to carry).
func clampViewport(w, h int) (int, int) {
	return min(max(w, 240), 2560), min(max(h, 240), 1600)
}
