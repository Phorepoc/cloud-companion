// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package main

// The display follows the viewing device: a phone-shaped display (short edge 720, 320 dpi) for phones, a large display (long edge up to 1600, 213 dpi = about 1200 dp wide, so apps
// pick their tablet / desktop layouts) for tablets, laptops and desktops. It is set with `wm size` / `wm density` on the main display, which the scrcpy server follows (a new
// session packet and a new video configuration, which the page already handles for rotation). Android's own overrides persist, so the next viewer gets a display for itself.

import (
	"fmt"
	"log"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

var maxDisplay = func() int {
	n, err := strconv.Atoi(env("PHE_ANDROID_MAX_DISPLAY", "1600"))
	if err != nil || n < 720 || n > 3840 {
		return 1600
	}
	return n
}()

func clampf(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
func even(v float64) int               { return int(math.Round(v/2)) * 2 }
func round16(v float64) int            { return int(math.Round(v/16)) * 16 }

// displayFor chooses the display size and density for a viewer whose usable area is vw x vh css pixels. kind is "phone" or "large".
func displayFor(vw, vh float64, kind string) (w, h, dpi int) {
	if vw < 50 || vh < 50 {
		vw, vh = 360, 640
	}
	landscape := vw > vh
	ratio := math.Max(vw, vh) / math.Min(vw, vh)
	var short, long float64
	if kind == "large" {
		long = float64(maxDisplay)
		short = long / clampf(ratio, 1.2, 1.9)
		dpi = 213
	} else {
		short = 720
		long = short * clampf(ratio, 1.4, 2.2)
		dpi = 320
	}
	l, s := round16(long), even(short)
	if kind == "large" {
		s = round16(short)
	}
	if landscape {
		return l, s, dpi
	}
	return s, l, dpi
}

var disp struct {
	sync.Mutex
	w, h, dpi int
}

// applyViewport sets the display for this viewer unless it already has (nearly) that size; it reports the display size now in force.
func applyViewport(vw, vh float64, kind string) (w, h, dpi int) {
	if kind != "large" {
		kind = "phone"
	}
	w, h, dpi = displayFor(vw, vh, kind)
	disp.Lock()
	defer disp.Unlock()
	if disp.w == w && disp.h == h && disp.dpi == dpi {
		return
	}
	cmd := fmt.Sprintf("wm size %dx%d; wm density %d", w, h, dpi)
	if out, err := adb("shell", cmd).CombinedOutput(); err != nil {
		log.Printf("wm size: %v %s", err, strings.TrimSpace(string(out)))
		return disp.w, disp.h, disp.dpi
	}
	disp.w, disp.h, disp.dpi = w, h, dpi
	return
}

// imeState reads whether a text field is focused and whether it has a selection, from `dumpsys input_method` (about 0.2 s): the served input connection
// is an EditableInputConnection while an editor has focus, and mCursorSelStart / mCursorSelEnd are its selection.
func imeState() (editor, selection bool) {
	out, err := exec.Command("adb", "-s", serial, "shell", "dumpsys", "input_method").Output()
	if err != nil {
		return false, false
	}
	return parseIME(string(out))
}

func parseIME(s string) (editor, selection bool) {
	editor = strings.Contains(s, "mServedInputConnection=RemoteInputConnectionImpl{connection=com.android.internal.inputmethod.EditableInputConnection")
	if !editor {
		return false, false
	}
	var a, b int
	i := strings.Index(s, "mCursorSelStart=")
	if i < 0 {
		return true, false
	}
	if _, err := fmt.Sscanf(s[i:], "mCursorSelStart=%d mCursorSelEnd=%d", &a, &b); err != nil {
		return true, false
	}
	return true, a != b && a >= 0 && b >= 0
}
