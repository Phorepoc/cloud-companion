// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package server

import "testing"

func TestClampViewport(t *testing.T) {
	for _, c := range [][4]int{{360, 640, 360, 640}, {10, 10, 240, 240}, {5000, 5000, 2560, 1600}, {1280, 720, 1280, 720}} {
		if w, h := clampViewport(c[0], c[1]); w != c[2] || h != c[3] {
			t.Errorf("clampViewport(%d,%d) = %d,%d want %d,%d", c[0], c[1], w, h, c[2], c[3])
		}
	}
}
