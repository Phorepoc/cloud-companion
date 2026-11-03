// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package pkginfo

// Round trips: icon.svg and evenodd.svg (testdata/) were encoded with pdk/tools/svg2nvg.py and svg2svgb.py (the encoders of the release's two vector
// formats); the decoders turn them back into SVG and the rasteriser draws it. The geometry is checked by what lands on which pixel, the colours by value.

import (
	"bytes"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
)

func td(t *testing.T, name string) []byte {
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func at(t *testing.T, pngB []byte, x, y int) color.NRGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(pngB))
	if err != nil {
		t.Fatal(err)
	}
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

func near(a, b uint8) bool { return int(a)-int(b) < 10 && int(b)-int(a) < 10 }

func wantColor(t *testing.T, what string, got color.NRGBA, r, g, b, a uint8) {
	t.Helper()
	if !near(got.R, r) || !near(got.G, g) || !near(got.B, b) || !near(got.A, a) {
		t.Errorf("%s: got %v, want %d %d %d %d", what, got, r, g, b, a)
	}
}

// the icon is 48x48 and drawn at 96 px: user (x, y) is pixel (2x, 2y)
func checkIcon(t *testing.T, svg string) {
	t.Helper()
	p, err := SVGToPNG(svg, 96)
	if err != nil {
		t.Fatal(err)
	}
	wantColor(t, "green square", at(t, p, 24, 24), 0, 255, 0, 255)
	wantColor(t, "orange circle", at(t, p, 72, 24), 255, 128, 0, 255)
	wantColor(t, "outside the circle", at(t, p, 72+25, 24), 0, 0, 0, 0)
	wantColor(t, "gradient, red end", at(t, p, 20, 64), 204, 0, 51, 255)
	wantColor(t, "gradient, blue end", at(t, p, 80, 64), 43, 0, 212, 255)
	wantColor(t, "black triangle", at(t, p, 60, 12), 0, 0, 0, 255)
	wantColor(t, "below the curve", at(t, p, 48, 90), 0, 0, 0, 0)
	wantColor(t, "empty corner", at(t, p, 90, 5), 0, 0, 0, 0)
}

func TestNVGRoundTrip(t *testing.T) {
	svg, err := NVGToSVG(td(t, "icon.nvg"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`viewBox="0 0 48 48"`, `fill="#00ff00"`, `fill="#ff8000"`, `<linearGradient`, `stop-color="#ff0000"`, `stop-color="#0000ff"`, `url(#g1)`} {
		if !strings.Contains(svg, want) {
			t.Errorf("the SVG lacks %s:\n%s", want, svg)
		}
	}
	checkIcon(t, svg)
}

func TestSVGBRoundTrip(t *testing.T) {
	svg, err := SVGBToSVG(td(t, "icon.svgb"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`viewBox="0 0 48 48"`, `<linearGradient`, `<circle`, `<rect`, `stop-color="#ff0000"`} {
		if !strings.Contains(svg, want) {
			t.Errorf("the SVG lacks %s:\n%s", want, svg)
		}
	}
	checkIcon(t, svg)
}

func TestSVGBEvenOddOpacityStroke(t *testing.T) {
	svg, err := SVGBToSVG(td(t, "evenodd.svgb"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := SVGToPNG(svg, 80)
	if err != nil {
		t.Fatal(err)
	}
	wantColor(t, "ring at half opacity", at(t, p, 8, 8), 0x33, 0x66, 0x99, 128)
	wantColor(t, "hole (even-odd)", at(t, p, 40, 24), 0, 0, 0, 0)
	wantColor(t, "line", at(t, p, 10, 40), 255, 0, 0, 255)
}

func TestVectorDamage(t *testing.T) {
	for _, name := range []string{"icon.nvg", "icon.svgb"} {
		b := td(t, name)
		for n := 0; n < len(b); n += 7 {
			NVGToSVG(b[:n])
			SVGBToSVG(b[:n])
		}
		c := append([]byte(nil), b...)
		for i := 8; i < len(c); i += 3 {
			c[i] ^= 0x5a
		}
		NVGToSVG(c)
		SVGBToSVG(c)
	}
	if _, err := NVGToSVG([]byte("nope")); err == nil {
		t.Error("garbage accepted")
	}
	if _, err := SVGToPNG("<svg", 10); err == nil {
		t.Error("bad svg accepted")
	}
}

func TestMIF(t *testing.T) {
	mif := buildMIF(mifTestIcon{typ: 2, data: td(t, "icon.nvg")}, mifTestIcon{typ: 1, data: td(t, "icon.svgb")}, mifTestIcon{typ: -1, mbm: 0})
	ics, err := ParseMIF(mif)
	if err != nil || len(ics) != 3 || ics[0].Type != 2 || ics[1].Type != 1 || ics[2].MBMID != 0 {
		t.Fatalf("%+v %v", ics, err)
	}
	for i := 0; i < 2; i++ {
		p, svg, err := MIFIcon(mif, nil, i)
		if err != nil || svg == "" {
			t.Fatal(i, err)
		}
		wantColor(t, "mif vector icon", at(t, p, 24, 24), 0, 255, 0, 255)
	}
	if _, _, err := MIFIcon(mif, nil, 2); err == nil {
		t.Error("a bitmap icon without its .mbm should fail")
	}
	p, svg, err := MIFIcon(mif, buildMBM(), 2)
	if err != nil || svg != "" {
		t.Fatal(err)
	}
	wantColor(t, "mif bitmap icon", at(t, p, 0, 0), 255, 0, 0, 255)
	for n := 0; n < len(mif); n += 5 {
		MIFIcon(mif[:n], nil, 0)
	}
}

// A package whose registration file names the localisable file, which names the .mif; also the plain resource reader.
func TestSISWithMIFIcon(t *testing.T) {
	loc := buildRSC(append([]byte{1, 0, 0, 0}, ltext("Demo")...), append(append(append(ltext("Demo"), 0, 0, 0, 0, 1, 0, 0, 0), ltext(`\resource\apps\demo_icon.mif`)...), 0))
	reg := buildRSC([]byte{0, 0, 0, 0, 0, 0, 0, 0}, append(append(ltext(`\sys\bin\demo.exe`), 0, 0, 0, 0), ltext(`\resource\apps\demo_loc`)...))
	decoy := buildMBM() // another picture in the package that must not be taken
	sis := buildSIS(0xE3333333, []string{"Vec"}, []string{"Acme"}, []uint16{1}, [3]int32{1, 0, 0}, 0, []sisTestFile{
		{`!:\private\10003a3f\import\apps\demo_reg.rsc`, reg},
		{`!:\resource\apps\demo_loc.rsc`, loc},
		{`!:\resource\apps\demo_icon.mif`, buildMIF(mifTestIcon{typ: 2, data: td(t, "icon.nvg")})},
		{`!:\resource\apps\a_app_decoy.mbm`, decoy},
	})
	in, err := ParseSIS(sis)
	if err != nil {
		t.Fatal(err)
	}
	if in.Icon == nil || in.IconSVG == "" || in.IconNote != "" {
		t.Fatalf("no vector icon: %q", in.IconNote)
	}
	wantColor(t, "icon", at(t, in.Icon, 24, 24), 0, 255, 0, 255)
	ri := parseReg(reg)
	if ri.locFile != "demo_loc" {
		t.Errorf("%+v", ri)
	}
	if ic := locIcons(loc); len(ic) != 1 || !strings.HasSuffix(ic[0], "demo_icon.mif") {
		t.Errorf("%v", ic)
	}
}
