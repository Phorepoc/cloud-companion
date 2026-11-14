// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package pkginfo

// Cases found on real Qt applications for Symbian, rebuilt synthetically: the registration names the localisable resource file without "_loc"
// (\resource\apps\Name), and the .mif holds an SVG-T icon that is only an <image> with an embedded PNG whose base64 text has line breaks.

import (
	"encoding/base64"
	"image/color"
	"strings"
	"testing"
)

func embeddedImageSVGB(pngB []byte) []byte {
	b64 := base64.StdEncoding.EncodeToString(pngB)
	b64 = b64[:20] + "\n " + b64[20:] // the white space real files have inside the data
	uri := "data:image/png;base64," + b64
	var out []byte
	out = append(out, le(uint32(svgbMagic))...)
	out = append(out, 0) // svg
	out = append(out, le(uint16(26), uint8(0), int32(32<<16))...)
	out = append(out, le(uint16(27), uint8(0), int32(32<<16))...)
	out = append(out, le(uint16(63+25), int32(0), int32(0), int32(32<<16), int32(32<<16))...) // viewBox 0 0 32 32
	out = append(out, le(uint16(svgbStart))...)
	out = append(out, 22) // image
	out = append(out, le(uint16(1+25), int32(32<<16), uint16(2+25), int32(32<<16))...)
	out = append(out, le(uint16(111+25), int32(len(uri)))...)
	out = append(out, uri...)
	out = append(out, le(uint16(svgbStart))...)
	out = append(out, svgbEnd, svgbEnd, svgbFile)
	return out
}

func TestEmbeddedImageIcon(t *testing.T) {
	svg, err := SVGBToSVG(embeddedImageSVGB(testPNG(color.NRGBA{200, 30, 60, 255}, 8)))
	if err != nil {
		t.Fatal(err)
	}
	p, err := SVGToPNG(svg, 64)
	if err != nil {
		t.Fatal(err)
	}
	wantColor(t, "embedded picture", at(t, p, 32, 32), 200, 30, 60, 255)
}

func vectorApp(regRefs string, withLoc bool, mif []byte) []byte {
	loc := buildRSC(append(append(append(ltext("App"), 0, 0, 0, 0, 1, 0, 0, 0), ltext(`\resource\apps\app.mif`)...), 0))
	reg := buildRSC(append(append(ltext(`\sys\bin\app.exe`), 0, 0, 0, 0), ltext(regRefs)...))
	files := []sisTestFile{{`!:\private\10003a3f\import\apps\app_reg.rsc`, reg}}
	if withLoc {
		files = append(files, sisTestFile{`!:\resource\apps\app.rsc`, loc}) // no "_loc" in the name
	}
	if mif != nil {
		files = append(files, sisTestFile{`!:\resource\apps\app.mif`, mif})
	}
	return buildSIS(0xE5555555, []string{"App"}, []string{"V"}, []uint16{1}, [3]int32{1, 0, 0}, 0, files)
}

func TestLocFileWithoutLocInItsName(t *testing.T) {
	mif := buildMIF(mifTestIcon{typ: 1, data: embeddedImageSVGB(testPNG(color.NRGBA{10, 200, 10, 255}, 8))})
	in, err := ParseSIS(vectorApp(`\resource\apps\app`, true, mif))
	if err != nil || in.Icon == nil || in.IconNote != "" {
		t.Fatalf("%v %+v", err, in)
	}
	wantColor(t, "icon", at(t, in.Icon, 48, 48), 10, 200, 10, 255)
}

func TestIconNotesSayWhy(t *testing.T) {
	cases := []struct {
		name string
		sis  []byte
		want string
	}{
		{"no application", buildSIS(0xE6666666, []string{"Lib"}, []string{"V"}, []uint16{1}, [3]int32{1, 0, 0}, 0, []sisTestFile{{`!:\sys\bin\l.dll`, []byte("x")}}), "registers no application"},
		{"icon file absent", vectorApp(`\resource\apps\app`, true, nil), "app.mif, which is not in the package"},
		{"registration without icon", vectorApp(`\resource\apps\other`, false, nil), "does not name an icon file"},
		{"unreadable icon file", vectorApp(`\resource\apps\app`, true, []byte("definitely not an icon file here")), "could not be shown"},
	}
	for _, c := range cases {
		in, err := ParseSIS(c.sis)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if in.Icon != nil || !strings.Contains(in.IconNote, c.want) {
			t.Errorf("%s: icon %d bytes, note %q (want %q)", c.name, len(in.Icon), in.IconNote, c.want)
		}
	}
}
