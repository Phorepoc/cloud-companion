// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package pkginfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRealPackages reads every .sis / .sisx in PHECLOUD_REAL_SIS_DIR (real third-party packages, never committed; skipped when the variable is unset).
// Each must give a name; each must give an icon or say why not (icon_note); packages with "sparkle" or "soundwave" in the name are apps known to have
// an icon. The icons are written next to the inputs as <file>.icon.png (and .icon.svg for vector icons).
func TestRealPackages(t *testing.T) {
	dir := os.Getenv("PHECLOUD_REAL_SIS_DIR")
	if dir == "" {
		t.Skip("PHECLOUD_REAL_SIS_DIR is not set")
	}
	var files []string
	for _, pat := range []string{"*.sis", "*.sisx"} {
		m, _ := filepath.Glob(filepath.Join(dir, pat))
		files = append(files, m...)
	}
	if len(files) == 0 {
		t.Skip("no packages in " + dir)
	}
	for _, f := range files {
		in, err := Inspect(f)
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		t.Logf("%s: %q %s by %q, icon %d bytes, svg %d, note %q", filepath.Base(f), in.Name, in.Version, in.Vendor, len(in.Icon), len(in.IconSVG), in.IconNote)
		if in.Name == "" || in.Version == "" {
			t.Errorf("%s: name or version missing", f)
		}
		if len(in.Icon) == 0 && in.IconNote == "" {
			t.Errorf("%s: no icon and no reason", f)
		}
		low := strings.ToLower(filepath.Base(f))
		if (strings.Contains(low, "sparkle") || strings.Contains(low, "soundwave")) && len(in.Icon) == 0 {
			t.Errorf("%s: this app has an icon on a phone, none was read (%s)", f, in.IconNote)
		}
		if len(in.Icon) > 0 {
			os.WriteFile(f+".icon.png", in.Icon, 0o644)
		}
		if in.IconSVG != "" {
			os.WriteFile(f+".icon.svg", []byte(in.IconSVG), 0o644)
		}
	}
}
