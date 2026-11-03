// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package pkginfo

// Small real-format packages for tests: a SISX like emitter/sis.py writes (deflated controller and data), an MBM, an APK with a binary
// manifest and a resource table. The builders are the inverse of the readers, written from the format descriptions, not from the readers' code.

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	_ "embed"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"unicode/utf16"
)

func le(v ...any) []byte {
	var b bytes.Buffer
	for _, x := range v {
		binary.Write(&b, binary.LittleEndian, x)
	}
	return b.Bytes()
}

func sfld(typ uint32, payload []byte, typed bool) []byte {
	var b []byte
	if typed {
		b = le(typ)
	}
	b = append(b, le(uint32(len(payload)))...)
	b = append(b, payload...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func sarr(et uint32, els ...[]byte) []byte {
	return sfld(fArray, append(le(et), bytes.Join(els, nil)...), true)
}

func u16(s string) []byte { return le(utf16.Encode([]rune(s))) }

func deflate(b []byte) []byte {
	var o bytes.Buffer
	w := zlib.NewWriter(&o)
	w.Write(b)
	w.Close()
	return o.Bytes()
}

type sisTestFile struct {
	target string
	data   []byte
}

func buildSIS(uid uint32, names, vendors []string, langs []uint16, ver [3]int32, targetUID uint32, files []sisTestFile) []byte {
	var ns, vs, ls [][]byte
	for _, n := range names {
		ns = append(ns, sfld(fString, u16(n), false))
	}
	for _, n := range vendors {
		vs = append(vs, sfld(fString, u16(n), false))
	}
	for _, l := range langs {
		ls = append(ls, sfld(11, le(uint32(l)), false))
	}
	info := sfld(fInfo, append(append(append(append(append(sfld(fUID, le(uid), true), sfld(fString, u16(vendors[0]), true)...),
		sarr(fString, ns...)...), sarr(fString, vs...)...), sfld(fVersion, le(ver[0], ver[1], ver[2]), true)...),
		sfld(8, append(sfld(6, le(uint16(2024), uint8(0), uint8(0)), true), sfld(7, le(uint8(1), uint8(2), uint8(3)), true)...), true)...), true)
	info = append(info[:len(info):len(info)], le(uint8(0), uint8(0))...) // install type and flags are inside the payload in real files; harmless here
	_ = info
	// rebuild info with the two trailing bytes inside the payload
	ip := append(append(append(append(append(sfld(fUID, le(uid), true), sfld(fString, u16(vendors[0]), true)...),
		sarr(fString, ns...)...), sarr(fString, vs...)...), sfld(fVersion, le(ver[0], ver[1], ver[2]), true)...),
		sfld(8, append(sfld(6, le(uint16(2024), uint8(0), uint8(0)), true), sfld(7, le(uint8(1), uint8(2), uint8(3)), true)...), true)...)
	ip = append(ip, 0, 0, 0, 0)
	infoF := sfld(fInfo, ip, true)
	supLangs := sfld(fSupLangs, sarr(11, ls...), true)
	var devs [][]byte
	if targetUID != 0 {
		devs = append(devs, sfld(18, append(append(sfld(fUID, le(targetUID), true), sfld(5, sfld(fVersion, le(int32(0), int32(0), int32(0)), true), true)...),
			sarr(fString, sfld(fString, u16("Test platform"), false))...), false))
	}
	prereq := sfld(fPrereq, append(sarr(18, devs...), sarr(18)...), true)
	var descs [][]byte
	var datas [][]byte
	for i, f := range files {
		z := deflate(f.data)
		body := append(sfld(fString, u16(f.target), true), sfld(fString, nil, true)...)
		body = append(body, sfld(fHash, append(le(uint32(1)), sfld(37, bytes.Repeat([]byte{7}, 20), true)...), true)...)
		body = append(body, le(uint32(1), uint32(0), uint64(len(z)), uint64(len(f.data)), uint32(i))...)
		descs = append(descs, sfld(fFileDes, body, false))
		datas = append(datas, sfld(32, sfld(fCompressed, append(le(uint32(1), uint64(len(f.data))), z...), true), false))
	}
	block := sfld(fInstallBlk, append(append(sarr(fFileDes, descs...), sarr(fController)...), sarr(fIf)...), true)
	ctrl := sfld(fController, bytes.Join([][]byte{infoF, sfld(16, sarr(33), true), supLangs, prereq, sfld(19, sarr(20), true), block, sfld(fDataIndex, le(uint32(0)), true)}, nil), true)
	cc := sfld(fCompressed, append(le(uint32(1), uint64(len(ctrl))), deflate(ctrl)...), true)
	unit := sfld(31, sarr(32, datas...), false)
	data := sfld(fData, sarr(31, unit), true)
	contents := sfld(fContents, bytes.Join([][]byte{sfld(34, le(uint16(0)), true), sfld(35, le(uint16(0)), true), cc, data}, nil), true)
	return append(le(uint32(sisMagic), uint32(0), uid, uint32(0)), contents...)
}

// buildMBM: a 4x4 picture (24 bpp, red) and its 8-bit mask (left half opaque), plus a smaller 2x2 pair; the big pair's rows are byte-run coded.
func buildMBM() []byte {
	bm := func(w, h, bpp, colour, comp int, data []byte) []byte {
		return append(le(uint32(40+len(data)), uint32(40), uint32(w), uint32(h), uint32(0), uint32(0), uint32(bpp), uint32(colour), uint32(0), uint32(comp)), data...)
	}
	rows24 := func(w, h int) []byte {
		stride := (w*24 + 31) / 32 * 4
		b := make([]byte, stride*h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				b[y*stride+3*x], b[y*stride+3*x+1], b[y*stride+3*x+2] = 0, 0, 255 // BGR: red
			}
		}
		return b
	}
	mask := func(w, h int) []byte {
		b := make([]byte, ((w+3)/4*4)*h)
		for y := 0; y < h; y++ {
			for x := 0; x < w/2; x++ {
				b[y*((w+3)/4*4)+x] = 255
			}
		}
		return b
	}
	var bms [][]byte
	bms = append(bms, bm(2, 2, 24, 1, 0, rows24(2, 2)), bm(2, 2, 8, 0, 0, mask(2, 2)))
	// 4x4 mask run-coded: each 4-byte row = 2x255, 2x0 -> runs (repeat 2 of 255)(repeat 2 of 0) per row, as byte RLE units
	rle := []byte{}
	for y := 0; y < 4; y++ {
		rle = append(rle, 1, 255, 1, 0)
	}
	// 4x4 picture run-coded in 24-bit units: stride 12 bytes per row = 4 pixels: repeat 4 of (0,0,255)
	rle24 := []byte{}
	for y := 0; y < 4; y++ {
		rle24 = append(rle24, 3, 0, 0, 255)
	}
	bms = append(bms, bm(4, 4, 24, 1, 4, rle24), bm(4, 4, 8, 0, 1, rle))
	out := le(uint32(0x10000037), uint32(0x10000042), uint32(0), uint32(0), uint32(0))
	offs := []uint32{}
	for _, b := range bms {
		offs = append(offs, uint32(len(out)))
		out = append(out, b...)
	}
	trailer := uint32(len(out))
	out = append(out, le(uint32(len(bms)))...)
	for _, o := range offs {
		out = append(out, le(o)...)
	}
	binary.LittleEndian.PutUint32(out[16:], trailer)
	return out
}

// ---- APK

func axPool(strs []string) []byte {
	var offs, data []byte
	for _, s := range strs {
		offs = append(offs, le(uint32(len(data)))...)
		u := utf16.Encode([]rune(s))
		data = append(data, le(uint16(len(u)))...)
		data = append(data, le(u)...)
		data = append(data, 0, 0)
	}
	for len(data)%4 != 0 {
		data = append(data, 0)
	}
	hs := 28
	start := hs + len(offs)
	return append(le(uint16(1), uint16(hs), uint32(start+len(data)), uint32(len(strs)), uint32(0), uint32(0), uint32(start), uint32(0)), append(offs, data...)...)
}

type axAttr struct {
	name  int
	raw   int
	dtype uint8
	data  uint32
}

func axElem(name int, attrs ...axAttr) []byte {
	body := le(uint32(1), uint32(0xffffffff), uint32(0xffffffff)) // line, comment, ns (header part: line no, comment) -- see below
	_ = body
	var a []byte
	for _, x := range attrs {
		a = append(a, le(int32(-1), int32(x.name), int32(x.raw), uint16(8), uint8(0), x.dtype, x.data)...)
	}
	hdr := le(uint16(0x102), uint16(16), uint32(36+len(a)), uint32(1), uint32(0xffffffff))
	hdr = append(hdr, le(int32(-1), int32(name), uint16(20), uint16(20), uint16(len(attrs)), uint16(0), uint16(0), uint16(0))...)
	return append(hdr, a...)
}

func axDoc(strs []string, elems ...[]byte) []byte {
	body := append(axPool(strs), bytes.Join(elems, nil)...)
	return append(le(uint16(3), uint16(8), uint32(8+len(body))), body...)
}

func testPNG(c color.NRGBA, size int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// arscTable: package 0x7f with string resources (type 1: app_name -> "Test App") and drawables (type 2: icon with a low and a high density PNG).
func arscTable(global []string) []byte {
	typeChunk := func(tid uint8, density uint16, locale string, vals []uint32) []byte {
		cfg := make([]byte, 32)
		binary.LittleEndian.PutUint32(cfg, 32)
		copy(cfg[8:], locale)
		binary.LittleEndian.PutUint16(cfg[14:], density)
		hs := 20 + 32
		var offs, ents []byte
		for _, v := range vals {
			offs = append(offs, le(uint32(len(ents)))...)
			ents = append(ents, le(uint16(8), uint16(0), uint32(0), uint16(8), uint8(0), uint8(3), v)...)
		}
		start := hs + len(offs)
		h := le(uint16(0x201), uint16(hs), uint32(start+len(ents)), tid, uint8(0), uint16(0), uint32(len(vals)), uint32(start))
		return append(append(append(h, cfg...), offs...), ents...)
	}
	pkgPool := axPool([]string{"string", "drawable"})
	keyPool := axPool([]string{"app_name", "icon"})
	hdr := make([]byte, 284)
	binary.LittleEndian.PutUint16(hdr, 0x200)
	binary.LittleEndian.PutUint16(hdr[2:], 284)
	binary.LittleEndian.PutUint32(hdr[8:], 0x7f)
	body := bytes.Join([][]byte{pkgPool, keyPool,
		typeChunk(1, 0, "", []uint32{0}),
		typeChunk(2, 160, "", []uint32{1}), typeChunk(2, 480, "", []uint32{2})}, nil)
	binary.LittleEndian.PutUint32(hdr[4:], uint32(284+len(body)))
	pkg := append(hdr, body...)
	gp := axPool(global)
	out := append(le(uint16(2), uint16(12), uint32(12+len(gp)+len(pkg)), uint32(1)), gp...)
	return append(out, pkg...)
}

func buildAPK() []byte {
	// manifest strings: 0 manifest 1 package 2 versionName 3 application 4 label 5 icon 6 com.Example.App 7 1.2.3 8 uses-sdk 9 minSdkVersion
	strs := []string{"manifest", "package", "versionName", "application", "label", "icon", "com.Example.App", "1.2.3", "uses-sdk", "minSdkVersion"}
	man := axDoc(strs,
		axElem(0, axAttr{1, 6, 3, 6}, axAttr{2, 7, 3, 7}),
		axElem(8, axAttr{9, 0xffffffff, 0x10, 21}),
		axElem(3, axAttr{4, 0xffffffff, 1, 0x7f010000}, axAttr{5, 0xffffffff, 1, 0x7f020000}))
	table := arscTable([]string{"Test App", "res/drawable-mdpi/icon.png", "res/drawable-xxhdpi/icon.png"})
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct {
		n string
		b []byte
	}{{"AndroidManifest.xml", man}, {"resources.arsc", table}, {"res/drawable-mdpi/icon.png", testPNG(color.NRGBA{255, 0, 0, 255}, 8)},
		{"res/drawable-xxhdpi/icon.png", testPNG(color.NRGBA{0, 255, 0, 255}, 16)}, {"lib/arm64-v8a/libx.so", []byte("x")}, {"lib/x86_64/libx.so", []byte("x")}} {
		w, _ := zw.Create(f.n)
		w.Write(f.b)
	}
	zw.Close()
	return buf.Bytes()
}

// Exported for the tests of other packages (the catalogue scanner): small real-format packages.

// ExampleSIS is a SISX with one file; the same uid with another version is "the same app, changed".
func ExampleSIS(uid uint32, name, vendor string, version [3]int32, payload string) []byte {
	return buildSIS(uid, []string{name}, []string{vendor}, []uint16{1}, version, 0x20022E6D, []sisTestFile{{`!:\sys\bin\x.exe`, []byte(payload)}})
}

// ExampleAPK is an APK for com.Example.App 1.2.3 labelled "Test App", with a 16x16 icon.
func ExampleAPK() []byte { return buildAPK() }

// buildMIF: an S60 icon file (version 2) from entries: {type, data} for vector icons, {-1, nil} for "bitmap number n of the .mbm" (n given in data[0]).
type mifTestIcon struct {
	typ  int
	data []byte
	mbm  int
}

func buildMIF(icons ...mifTestIcon) []byte {
	out := le(int32(mifFileUID), int32(2), int32(0), int32(len(icons)))
	// layout: header (16) | icons | index
	body := []byte{}
	var entries [][2]int32
	for _, ic := range icons {
		if ic.typ < 0 {
			entries = append(entries, [2]int32{int32(-ic.mbm), 0})
			continue
		}
		start := 16 + len(body)
		h := le(int32(mifIconUID), int32(1), int32(32), int32(len(ic.data)), int32(ic.typ), int32(8), int32(0), int32(8))
		body = append(body, h...)
		body = append(body, ic.data...)
		for len(body)%4 != 0 {
			body = append(body, 0)
		}
		entries = append(entries, [2]int32{int32(start), int32(32 + len(ic.data))})
	}
	var idx []byte
	for _, e := range entries {
		idx = append(idx, le(e[0], e[1])...)
	}
	out = le(int32(mifFileUID), int32(2), int32(16+len(body)), int32(len(icons)))
	return append(append(out, body...), idx...)
}

// buildRSC: a plain resource file (no compression) from resources.
func buildRSC(res ...[]byte) []byte {
	var out []byte
	var offs []uint16
	for _, r := range res {
		offs = append(offs, uint16(len(out)))
		out = append(out, r...)
	}
	start := len(out)
	for _, o := range offs {
		out = append(out, le(o)...)
	}
	return append(out, le(uint16(start))...)
}

// ltext: a counted UTF-16 string, as the resource compiler writes LTEXT in a Unicode build.
func ltext(s string) []byte { return append([]byte{byte(len([]rune(s)))}, u16(s)...) }

//go:embed testdata/icon.nvg
var exampleNVG []byte

// ExampleVectorSIS is a SISX whose application icon is a vector (NVG) icon in a .mif named by its registration and localisable resources.
func ExampleVectorSIS(uid uint32, name string) []byte {
	loc := buildRSC(append(append(append(ltext(name), 0, 0, 0, 0, 1, 0, 0, 0), ltext(`\resource\apps\v_icon.mif`)...), 0))
	reg := buildRSC(append(append(ltext(`\sys\bin\v.exe`), 0, 0, 0, 0), ltext(`\resource\apps\v_loc`)...))
	return buildSIS(uid, []string{name}, []string{"Acme"}, []uint16{1}, [3]int32{1, 0, 0}, 0, []sisTestFile{
		{`!:\private\10003a3f\import\apps\v_reg.rsc`, reg}, {`!:\resource\apps\v_loc.rsc`, loc},
		{`!:\resource\apps\v_icon.mif`, buildMIF(mifTestIcon{typ: 2, data: exampleNVG})}})
}
