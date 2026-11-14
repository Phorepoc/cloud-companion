// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package pkginfo

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestMBM(t *testing.T) {
	b, err := MBMToPNG(buildMBM())
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 4 { // the larger pair wins
		t.Fatalf("size %v", img.Bounds())
	}
	r, g, bl, a := img.At(0, 0).RGBA()
	if r>>8 != 255 || g != 0 || bl != 0 || a>>8 != 255 {
		t.Errorf("left pixel %d %d %d %d", r>>8, g, bl, a>>8)
	}
	if _, _, _, a := img.At(3, 3).RGBA(); a != 0 {
		t.Errorf("the right half should be transparent, alpha %d", a)
	}
}

func TestSIS(t *testing.T) {
	// the icon: _reg.rsc names "!:\resource\apps\demo.mbm"
	rsc := append([]byte{1, 2, 3, 0}, []byte("\x1a!:\\resource\\apps\\Demo_icon.mbm")...)
	sis := buildSIS(0xE1234567, []string{"Demo App", "Appli de demo"}, []string{"Acme", "Acme FR"}, []uint16{1, 2}, [3]int32{1, 2, 3}, 0x20022E6D, []sisTestFile{
		{`!:\sys\bin\demo.exe`, append(append(make([]byte, 16), []byte("EPOC")...), append(make([]byte, 102), 0x00, 0x10)...)}, // cpu 0x1000 at 122
		{`!:\private\10003a3f\import\apps\demo_reg.rsc`, rsc},
		{`!:\resource\apps\demo_icon.mbm`, buildMBM()},
	})
	in, err := ParseSIS(sis)
	if err != nil {
		t.Fatal(err)
	}
	if in.Name != "Demo App" || in.Vendor != "Acme" || in.Version != "1.2.3" || in.UID != "e1234567" || in.AppID != "uid-e1234567" {
		t.Errorf("%+v", in)
	}
	if len(in.Languages) != 2 || in.Languages[1] != "French" || len(in.Targets) != 1 || in.Targets[0] != "Symbian Anna / Belle" || in.Files != 3 {
		t.Errorf("%+v", in)
	}
	if len(in.Arch) != 1 || in.Arch[0] != "x32" {
		t.Errorf("arch %v", in.Arch)
	}
	if in.Icon == nil || in.IconNote != "" {
		t.Errorf("no icon: %q", in.IconNote)
	}
	// damaged files give errors, not panics
	for _, n := range []int{0, 10, 20, 100, len(sis) / 2, len(sis) - 3} {
		ParseSIS(sis[:n])
	}
	if _, err := ParseSIS(bytes.Repeat([]byte{9}, 100)); err == nil {
		t.Error("garbage accepted")
	}
}

func TestInspectAndAPK(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.apk")
	os.WriteFile(p, buildAPK(), 0o644)
	in, err := Inspect(p)
	if err != nil {
		t.Fatal(err)
	}
	if in.AppID != "com.example.app" || in.Version != "1.2.3" || in.Name != "Test App" || in.Platform != "android" {
		t.Errorf("%+v", in)
	}
	if len(in.Arch) != 2 || in.Arch[0] != "aarch64" || len(in.Targets) != 1 {
		t.Errorf("%v %v", in.Arch, in.Targets)
	}
	img, err := png.Decode(bytes.NewReader(in.Icon))
	if err != nil || img.Bounds().Dx() != 16 { // the xxhdpi one
		t.Errorf("icon %v %v", err, in.Icon == nil)
	}
	os.WriteFile(filepath.Join(dir, "bad.apk"), []byte("PK not really"), 0o644)
	if _, err := Inspect(filepath.Join(dir, "bad.apk")); err == nil {
		t.Error("bad apk accepted")
	}
	os.WriteFile(filepath.Join(dir, "x.zip"), []byte("z"), 0o644)
	if _, err := Inspect(filepath.Join(dir, "x.zip")); err != ErrUnsupported {
		t.Error(err)
	}
}
