// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package pkginfo

// Android APK: a zip with a binary AndroidManifest.xml (chunked XML with a string pool) and resources.arsc (the resource table). Read here: package,
// versionName / versionCode, the application label (a string or a reference into the table), minSdkVersion, the processors of lib/<abi>/, and the
// icon (the largest PNG the icon resource has; an adaptive icon, whose layers are PNGs, is composed).

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"sort"
	"strings"
	"unicode/utf16"
)

type strPool struct{ s []string }

func parsePool(b []byte, off int) (strPool, error) { // b[off:] is a string pool chunk (type 1)
	var p strPool
	if off+28 > len(b) || binary.LittleEndian.Uint16(b[off:]) != 1 {
		return p, errors.New("no string pool")
	}
	hs := int(binary.LittleEndian.Uint16(b[off+2:]))
	size := int(binary.LittleEndian.Uint32(b[off+4:]))
	n := int(binary.LittleEndian.Uint32(b[off+8:]))
	flags := binary.LittleEndian.Uint32(b[off+16:])
	start := int(binary.LittleEndian.Uint32(b[off+20:]))
	if size < 28 || off+size > len(b) || n < 0 || n > 1<<20 || off+hs+4*n > len(b) {
		return p, errors.New("bad string pool")
	}
	chunk := b[off : off+size]
	utf8 := flags&0x100 != 0
	p.s = make([]string, n)
	for i := 0; i < n; i++ {
		o := start + int(binary.LittleEndian.Uint32(chunk[hs+4*i:]))
		if o < 0 || o >= len(chunk) {
			continue
		}
		if utf8 {
			_, o = poolLen8(chunk, o)
			l, o2 := poolLen8(chunk, o)
			if o2+l <= len(chunk) {
				p.s[i] = string(chunk[o2 : o2+l])
			}
		} else {
			if o+2 > len(chunk) {
				continue
			}
			l := int(binary.LittleEndian.Uint16(chunk[o:]))
			o += 2
			if l&0x8000 != 0 && o+2 <= len(chunk) {
				l = (l&0x7fff)<<16 | int(binary.LittleEndian.Uint16(chunk[o:]))
				o += 2
			}
			if o+2*l <= len(chunk) {
				u := make([]uint16, l)
				for k := range u {
					u[k] = binary.LittleEndian.Uint16(chunk[o+2*k:])
				}
				p.s[i] = string(utf16.Decode(u))
			}
		}
	}
	return p, nil
}

func poolLen8(b []byte, o int) (int, int) {
	if o >= len(b) {
		return 0, o
	}
	l := int(b[o])
	o++
	if l&0x80 != 0 && o < len(b) {
		l = (l&0x7f)<<8 | int(b[o])
		o++
	}
	return l, o
}

func (p strPool) get(i int) string {
	if i < 0 || i >= len(p.s) {
		return ""
	}
	return p.s[i]
}

type axmlAttr struct {
	name  string
	ns    string
	dtype uint8
	data  uint32
	raw   string
}

type axmlElem struct {
	name  string
	attrs []axmlAttr
}

func (e axmlElem) attr(name string) (axmlAttr, bool) {
	for _, a := range e.attrs {
		if a.name == name {
			return a, true
		}
	}
	return axmlAttr{}, false
}

// parseAXML returns the start elements of a binary XML document in order.
func parseAXML(b []byte) ([]axmlElem, error) {
	if len(b) < 8 || binary.LittleEndian.Uint16(b) != 3 {
		return nil, errors.New("not a binary XML document")
	}
	var pool strPool
	var out []axmlElem
	off := int(binary.LittleEndian.Uint16(b[2:]))
	for off+8 <= len(b) {
		typ := binary.LittleEndian.Uint16(b[off:])
		size := int(binary.LittleEndian.Uint32(b[off+4:]))
		if size < 8 || off+size > len(b) {
			break
		}
		switch typ {
		case 1:
			p, err := parsePool(b, off)
			if err != nil {
				return nil, err
			}
			pool = p
		case 0x102: // start element
			c := b[off : off+size]
			if len(c) < 36 {
				break
			}
			e := axmlElem{name: pool.get(int(int32(binary.LittleEndian.Uint32(c[20:]))))}
			attrStart := int(binary.LittleEndian.Uint16(c[24:]))
			attrSize := int(binary.LittleEndian.Uint16(c[26:]))
			n := int(binary.LittleEndian.Uint16(c[28:]))
			if attrSize < 20 {
				attrSize = 20
			}
			for i := 0; i < n; i++ {
				o := 16 + attrStart + i*attrSize
				if o+20 > len(c) {
					break
				}
				a := axmlAttr{ns: pool.get(int(int32(binary.LittleEndian.Uint32(c[o:])))), name: pool.get(int(int32(binary.LittleEndian.Uint32(c[o+4:])))),
					raw: pool.get(int(int32(binary.LittleEndian.Uint32(c[o+8:])))), dtype: c[o+15], data: binary.LittleEndian.Uint32(c[o+16:])}
				e.attrs = append(e.attrs, a)
			}
			out = append(out, e)
		}
		off += size
	}
	return out, nil
}

// value renders an attribute as text; refs gives the resource id when it is a reference.
func (a axmlAttr) text(pool func(uint32) string) (s string, ref uint32) {
	switch a.dtype {
	case 3:
		if a.raw != "" {
			return a.raw, 0
		}
		return pool(a.data), 0
	case 1:
		return "", a.data
	case 0x10:
		return fmt.Sprint(int32(a.data)), 0
	case 0x11:
		return fmt.Sprintf("0x%x", a.data), 0
	}
	return a.raw, 0
}

// ---- resources.arsc

type arscEntry struct {
	cfgDensity int
	cfgLocale  string
	dtype      uint8
	data       uint32
}

type arsc struct {
	global strPool
	// id (0xPPTTEEEE) -> entries, one per configuration
	res map[uint32][]arscEntry
}

func parseARSC(b []byte) (*arsc, error) {
	if len(b) < 12 || binary.LittleEndian.Uint16(b) != 2 {
		return nil, errors.New("not a resource table")
	}
	t := &arsc{res: map[uint32][]arscEntry{}}
	off := int(binary.LittleEndian.Uint16(b[2:]))
	for off+8 <= len(b) {
		typ := binary.LittleEndian.Uint16(b[off:])
		size := int(binary.LittleEndian.Uint32(b[off+4:]))
		if size < 8 || off+size > len(b) {
			break
		}
		switch typ {
		case 1:
			p, err := parsePool(b, off)
			if err != nil {
				return nil, err
			}
			t.global = p
		case 0x200:
			t.parsePackage(b[off : off+size])
		}
		off += size
	}
	return t, nil
}

func (t *arsc) parsePackage(c []byte) {
	if len(c) < 284 {
		return
	}
	pid := binary.LittleEndian.Uint32(c[8:])
	off := int(binary.LittleEndian.Uint16(c[2:]))
	for off+8 <= len(c) {
		typ := binary.LittleEndian.Uint16(c[off:])
		size := int(binary.LittleEndian.Uint32(c[off+4:]))
		if size < 8 || off+size > len(c) {
			break
		}
		if typ == 0x201 {
			t.parseType(pid, c[off:off+size])
		}
		off += size
	}
}

func (t *arsc) parseType(pid uint32, c []byte) {
	if len(c) < 28 {
		return
	}
	hs := int(binary.LittleEndian.Uint16(c[2:]))
	tid := uint32(c[8])
	flags := c[9]
	count := int(binary.LittleEndian.Uint32(c[12:]))
	start := int(binary.LittleEndian.Uint32(c[16:]))
	if hs < 28 || count < 0 || count > 1<<20 || flags&1 != 0 { // sparse tables: not read
		return
	}
	cfg := c[20:hs]
	e := arscEntry{}
	if len(cfg) >= 16 {
		e.cfgDensity = int(binary.LittleEndian.Uint16(cfg[14:]))
		if l := cfg[8:12]; l[0] != 0 {
			e.cfgLocale = strings.TrimRight(string(l[:2]), "\x00")
		}
	}
	for i := 0; i < count; i++ {
		if hs+4*i+4 > len(c) {
			return
		}
		eo := binary.LittleEndian.Uint32(c[hs+4*i:])
		if eo == 0xFFFFFFFF {
			continue
		}
		o := start + int(eo)
		if o+16 > len(c) {
			continue
		}
		if binary.LittleEndian.Uint16(c[o+2:])&1 != 0 { // complex (a bag): skip
			continue
		}
		ent := e
		ent.dtype = c[o+11]
		ent.data = binary.LittleEndian.Uint32(c[o+12:])
		id := pid<<24 | tid<<16 | uint32(i)
		t.res[id] = append(t.res[id], ent)
	}
}

// str resolves a string resource: the entry without a locale, else English, else the first.
func (t *arsc) str(id uint32) string {
	es := t.res[id]
	best := -1
	for i, e := range es {
		if e.dtype != 3 {
			continue
		}
		if e.cfgLocale == "" {
			best = i
			break
		}
		if best < 0 || e.cfgLocale == "en" {
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	return t.global.get(int(es[best].data))
}

// files lists the file paths (string values) of a resource, densest configuration first.
func (t *arsc) files(id uint32) []string {
	es := append([]arscEntry(nil), t.res[id]...)
	sort.SliceStable(es, func(i, j int) bool { return es[i].cfgDensity > es[j].cfgDensity })
	var out []string
	for _, e := range es {
		if e.dtype == 3 {
			if s := t.global.get(int(e.data)); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// ---- the package

func readZip(zr *zip.Reader, name string, limit int64) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name == name {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer r.Close()
			return io.ReadAll(io.LimitReader(r, limit))
		}
	}
	return nil, errors.New("not in the package: " + name)
}

// ParseAPK reads the metadata and icon of an APK.
func ParseAPK(path string) (*Info, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("not a readable APK (%v)", err)
	}
	defer zr.Close()
	man, err := readZip(&zr.Reader, "AndroidManifest.xml", 8<<20)
	if err != nil {
		return nil, err
	}
	elems, err := parseAXML(man)
	if err != nil {
		return nil, fmt.Errorf("AndroidManifest.xml: %w", err)
	}
	var tab *arsc
	if rb, err := readZip(&zr.Reader, "resources.arsc", 64<<20); err == nil {
		tab, _ = parseARSC(rb)
	}
	poolFn := func(uint32) string { return "" }
	resolve := func(a axmlAttr) (string, uint32) {
		s, ref := a.text(poolFn)
		if ref != 0 && tab != nil {
			if v := tab.str(ref); v != "" {
				return v, 0
			}
		}
		return s, ref
	}
	info := &Info{Kind: "apk", Platform: "android"}
	var iconRef uint32
	var iconPath string
	for _, e := range elems {
		switch e.name {
		case "manifest":
			if a, ok := e.attr("package"); ok {
				info.AppID, _ = a.text(poolFn)
			}
			if a, ok := e.attr("versionName"); ok {
				info.Version, _ = a.text(poolFn)
			}
			if info.Version == "" {
				if a, ok := e.attr("versionCode"); ok {
					info.Version, _ = a.text(poolFn)
				}
			}
		case "uses-sdk":
			if a, ok := e.attr("minSdkVersion"); ok {
				v, _ := a.text(poolFn)
				info.Targets = append(info.Targets, "Android API "+v+" and newer")
			}
		case "application":
			if a, ok := e.attr("label"); ok {
				info.Name, _ = resolve(a)
			}
			if a, ok := e.attr("icon"); ok {
				s, ref := a.text(poolFn)
				iconRef, iconPath = ref, s
			}
		}
	}
	info.AppID = strings.ToLower(info.AppID)
	if info.Name == "" {
		info.Name = info.AppID
	}
	abis := map[string]string{"arm64-v8a": "aarch64", "armeabi-v7a": "armv7", "armeabi": "armv7", "x86_64": "x86_64", "x86": "x86"}
	seen := map[string]bool{}
	for _, f := range zr.File {
		if p := strings.Split(f.Name, "/"); len(p) >= 3 && p[0] == "lib" {
			if a, ok := abis[p[1]]; ok && !seen[a] {
				seen[a] = true
				info.Arch = append(info.Arch, a)
			}
		}
	}
	sort.Strings(info.Arch)
	if iconRef != 0 && tab != nil || iconPath != "" {
		apkIcon(&zr.Reader, tab, iconRef, iconPath, info)
	}
	if info.Icon == nil && info.IconNote == "" {
		if iconRef == 0 && iconPath == "" {
			info.IconNote = "the application declares no icon"
		} else {
			info.IconNote = "the icon the application names was not found in the package"
		}
	}
	return info, nil
}

func decodePNG(b []byte) (image.Image, bool) {
	img, err := png.Decode(bytes.NewReader(b))
	return img, err == nil
}

func apkIcon(zr *zip.Reader, tab *arsc, ref uint32, direct string, info *Info) {
	var paths []string
	if direct != "" {
		paths = append(paths, direct)
	}
	if tab != nil && ref != 0 {
		paths = append(paths, tab.files(ref)...)
	}
	sawVector, sawWebP := false, false
	for _, p := range paths {
		switch {
		case strings.HasSuffix(p, ".png"):
			if b, err := readZip(zr, p, 8<<20); err == nil {
				if _, ok := decodePNG(b); ok {
					info.Icon, info.IconNote = b, ""
					return
				}
			}
		case strings.HasSuffix(p, ".webp"):
			sawWebP = true
		case strings.HasSuffix(p, ".xml"):
			if b, err := readZip(zr, p, 1<<20); err == nil {
				if png, ok := adaptive(zr, tab, b); ok {
					info.Icon, info.IconNote = png, ""
					return
				}
			}
			sawVector = true
		}
	}
	switch {
	case sawVector:
		info.IconNote = "the icon is an Android vector or adaptive drawable, which is not shown here"
	case sawWebP:
		info.IconNote = "the icon is a WebP picture, which is not shown here"
	}
}

// adaptive composes an adaptive icon (<adaptive-icon> with background and foreground drawables that are PNGs or colours).
func adaptive(zr *zip.Reader, tab *arsc, xml []byte) ([]byte, bool) {
	els, err := parseAXML(xml)
	if err != nil || tab == nil {
		return nil, false
	}
	layer := func(name string) (image.Image, bool) {
		for _, e := range els {
			if e.name != name {
				continue
			}
			a, ok := e.attr("drawable")
			if !ok {
				return nil, false
			}
			if a.dtype >= 0x1c && a.dtype <= 0x1f { // a colour
				c := a.data
				u := image.NewUniform(rgba(c))
				dst := image.NewNRGBA(image.Rect(0, 0, 108, 108))
				draw.Draw(dst, dst.Bounds(), u, image.Point{}, draw.Src)
				return dst, true
			}
			for _, p := range tab.files(a.data) {
				if strings.HasSuffix(p, ".png") {
					if b, err := readZip(zr, p, 8<<20); err == nil {
						if img, ok := decodePNG(b); ok {
							return img, true
						}
					}
				}
			}
			// a colour resource
			for _, e := range tab.res[a.data] {
				if e.dtype >= 0x1c && e.dtype <= 0x1f {
					dst := image.NewNRGBA(image.Rect(0, 0, 108, 108))
					draw.Draw(dst, dst.Bounds(), image.NewUniform(rgba(e.data)), image.Point{}, draw.Src)
					return dst, true
				}
			}
		}
		return nil, false
	}
	fg, ok := layer("foreground")
	if !ok {
		return nil, false
	}
	b := fg.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	if bg, ok := layer("background"); ok {
		scaleInto(dst, bg)
	}
	draw.Draw(dst, dst.Bounds(), fg, b.Min, draw.Over)
	var buf bytes.Buffer
	if png.Encode(&buf, dst) != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

func scaleInto(dst *image.NRGBA, src image.Image) { // nearest neighbour, enough for a flat background
	sb, db := src.Bounds(), dst.Bounds()
	for y := 0; y < db.Dy(); y++ {
		for x := 0; x < db.Dx(); x++ {
			dst.Set(x, y, src.At(sb.Min.X+x*sb.Dx()/db.Dx(), sb.Min.Y+y*sb.Dy()/db.Dy()))
		}
	}
}

func rgba(c uint32) color.NRGBA {
	return color.NRGBA{R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c), A: uint8(c >> 24)}
}
