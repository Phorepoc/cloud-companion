// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package catalogue

import (
	"strings"
	"testing"
)

func TestSafeNameAndPackages(t *testing.T) {
	for in, want := range map[string]string{
		"app.sisx": "app.sisx", "../../etc/passwd": "passwd", `C:\dir\My App (1).apk`: "My App (1).apk", "a/b/c.deb": "c.deb",
		"..": "", "": "", "we?ird*na:me.zip": "we_ird_na_me.zip",
	} {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
	long := SafeName(strings.Repeat("x", 300) + ".sisx")
	if len(long) != 100 || !strings.HasSuffix(long, ".sisx") {
		t.Errorf("long name: %d %q", len(long), long)
	}
	for in, want := range map[string]string{"A.SISX": "sisx", "x.tar.gz": "tar.gz", "Z.AppImage": "appimage", "noext": "file"} {
		if got := PackageOf(in); got != want {
			t.Errorf("PackageOf(%q) = %q, want %q", in, got, want)
		}
	}
	for pkg, want := range map[string]string{"sis": "symbian", "apk": "android", "msi": "windows", "deb": "linux", "dmg": "macos", "zip": "other", "weird": "other"} {
		if got := PlatformOf(pkg); got != want {
			t.Errorf("PlatformOf(%q) = %q, want %q", pkg, got, want)
		}
	}
}

func TestNormalizeDefaults(t *testing.T) {
	r := Release{AppID: "My-App", Version: "1.0", Name: "My App", Description: "First line.\nSecond."}
	r.File.Name = "my.sisx"
	if err := r.Normalize(); err != nil {
		t.Fatal(err)
	}
	if r.AppID != "my-app" || r.Platform != "symbian" || r.Package != "sisx" || r.Summary != "First line." || r.Arch[0] != "arm" || r.Category != "app" {
		t.Fatalf("%+v", r)
	}
	bad := []Release{{AppID: "x", Version: "1", Name: "n"}, {AppID: "okay", Version: "bad version", Name: "n"}, {AppID: "okay", Version: "1", Name: ""},
		{AppID: "okay", Version: "1", Name: "n", Category: "nonsense"}, {AppID: "okay", Version: "1", Name: "n", Homepage: "ftp://x"}}
	for i, b := range bad {
		b.File.Name = "f.sis"
		if b.Normalize() == nil {
			t.Errorf("bad release %d accepted", i)
		}
	}
	r2 := Release{AppID: "droid", Version: "1", Name: "D"}
	r2.File.Name = "d.apk"
	r2.Normalize()
	if r2.Platform != "android" || r2.Arch[0] != "any" {
		t.Fatalf("%+v", r2)
	}
}
