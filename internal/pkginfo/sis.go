// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package pkginfo

// Symbian 9 SIS/SISX (the tree of typed fields):
//   field   = [type u32][length u32 (bit 31 set: a second u32 holds the upper half)][payload], padded to 4 bytes; array elements have no type
//   file    = 16-byte header (0x10201A7A magic, package UID, checksum) + SISContents{ checksums, SISCompressed<SISController>, SISData }
//   SISController: SISInfo (uid, vendor, names[], vendor names[], version ...), supported languages, prerequisites (target devices), install block (files)
//   SISData: array of data units, each an array of file data, each a SISCompressed (algorithm 0 none, 1 zlib).

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"unicode/utf16"
)

const (
	fString       = 1
	fArray        = 2
	fCompressed   = 3
	fVersion      = 4
	fUID          = 9
	fContents     = 12
	fController   = 13
	fInfo         = 14
	fSupLangs     = 15
	fPrereq       = 17
	fFileDes      = 24
	fHash         = 25
	fIf           = 26
	fElseIf       = 27
	fInstallBlk   = 28
	fData         = 30
	fCaps         = 41
	fDataIndex    = 40
	sisMagic      = 0x10201A7A
	maxController = 64 << 20
)

type sfield struct {
	typ     uint32
	payload []byte
}

var errSIS = errors.New("not a readable SIS file")

// readField reads one field at b[off:]; typed=false for array elements. It returns the field and the offset of the next one.
func readField(b []byte, off int, typed bool) (sfield, int, error) {
	var f sfield
	p := off
	if typed {
		if p+8 > len(b) {
			return f, 0, errSIS
		}
		f.typ = binary.LittleEndian.Uint32(b[p:])
		p += 4
	} else if p+4 > len(b) {
		return f, 0, errSIS
	}
	l := uint64(binary.LittleEndian.Uint32(b[p:]))
	p += 4
	if l&0x80000000 != 0 {
		if p+4 > len(b) {
			return f, 0, errSIS
		}
		l = (l & 0x7fffffff) | uint64(binary.LittleEndian.Uint32(b[p:]))<<31
		p += 4
	}
	if l > uint64(len(b)-p) {
		return f, 0, errSIS
	}
	f.payload = b[p : p+int(l)]
	return f, p + int((l+3)&^3), nil
}

func children(b []byte) []sfield {
	var out []sfield
	for off := 0; off < len(b); {
		f, next, err := readField(b, off, true)
		if err != nil {
			break
		}
		out = append(out, f)
		off = next
	}
	return out
}

// elements splits an array field's payload (element type u32, then untyped elements).
func elements(b []byte) (et uint32, els [][]byte) {
	if len(b) < 4 {
		return
	}
	et = binary.LittleEndian.Uint32(b)
	for off := 4; off < len(b); {
		f, next, err := readField(b, off, false)
		if err != nil {
			break
		}
		els = append(els, f.payload)
		off = next
	}
	return
}

func u16s(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}

func strings16(arrPayload []byte) []string {
	_, els := elements(arrPayload)
	out := make([]string, len(els))
	for i, e := range els {
		out[i] = u16s(e)
	}
	return out
}

// inflate opens a SISCompressed field's payload.
func inflate(p []byte, limit int64) ([]byte, error) {
	if len(p) < 12 {
		return nil, errSIS
	}
	alg, usz := binary.LittleEndian.Uint32(p), binary.LittleEndian.Uint64(p[4:])
	if usz > uint64(limit) {
		return nil, errors.New("a part of the package is too large to read")
	}
	switch alg {
	case 0:
		return p[12:], nil
	case 1:
		zr, err := zlib.NewReader(bytes.NewReader(p[12:]))
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		return io.ReadAll(io.LimitReader(zr, limit))
	}
	return nil, fmt.Errorf("unknown compression %d", alg)
}

// langNames: the TLanguage numbers of the common languages.
var langNames = map[int]string{1: "English", 2: "French", 3: "German", 4: "Spanish", 5: "Italian", 6: "Swedish", 7: "Danish", 8: "Norwegian", 9: "Finnish",
	10: "American English", 13: "Portuguese", 14: "Turkish", 16: "Russian", 17: "Hungarian", 18: "Dutch", 25: "Czech", 26: "Slovak", 27: "Polish", 28: "Slovenian",
	29: "Traditional Chinese", 30: "Hong Kong Chinese", 31: "Chinese", 32: "Japanese", 33: "Thai", 37: "Indonesian", 42: "Ukrainian", 44: "Korean", 45: "Arabic", 57: "Greek", 67: "Romanian"}

// platformNames: target-device UIDs that software names in its prerequisites.
var platformNames = map[uint32]string{0x101F7961: "S60 3rd Edition", 0x101F9115: "S60 3rd Edition FP1", 0x102032BE: "S60 3rd Edition FP2", 0x102752AE: "S60 3rd Edition FP2",
	0x1028315F: "S60 5th Edition", 0x20014DD0: "Symbian^3", 0x20022E6D: "Symbian Anna / Belle", 0x20024102: "Symbian Belle", 0x101F6300: "UIQ 3", 0x10200F9A: "UIQ 3"}

type sisFile struct {
	target string
	op     uint32
	index  uint32
}

// ParseSIS reads the metadata and the icon of a SIS/SISX file held in memory.
func ParseSIS(b []byte) (*Info, error) {
	if len(b) < 24 {
		return nil, errSIS
	}
	if binary.LittleEndian.Uint32(b) != sisMagic && binary.LittleEndian.Uint32(b[4:]) != sisMagic {
		return nil, errors.New("not a Symbian 9 SIS file (the 0x10201A7A signature is missing)")
	}
	top, _, err := readField(b, 16, true)
	if err != nil || top.typ != fContents {
		return nil, errSIS
	}
	var ctrl, data []byte
	for _, f := range children(top.payload) {
		switch f.typ {
		case fCompressed:
			if ctrl == nil {
				if ctrl, err = inflate(f.payload, maxController); err != nil {
					return nil, fmt.Errorf("the controller: %w", err)
				}
			}
		case fData:
			data = f.payload
		}
	}
	if ctrl == nil {
		return nil, errors.New("the package has no controller")
	}
	cf, _, err := readField(ctrl, 0, true)
	if err != nil || cf.typ != fController {
		return nil, errSIS
	}
	info := &Info{Kind: "sis", Platform: "symbian"}
	var langIDs []int
	var infoNames, infoVendors []string
	var files []sisFile
	var targets [][]string
	var dataIndex uint32
	for _, f := range children(cf.payload) {
		switch f.typ {
		case fInfo:
			for _, c := range children(f.payload) {
				switch c.typ {
				case fUID:
					if len(c.payload) >= 4 && info.UID == "" {
						info.UID = fmt.Sprintf("%08x", binary.LittleEndian.Uint32(c.payload))
					}
				case fString:
					if info.Vendor == "" {
						info.Vendor = u16s(c.payload)
					}
				case fArray:
					if infoNames == nil {
						infoNames = strings16(c.payload)
					} else if infoVendors == nil {
						infoVendors = strings16(c.payload)
					}
				case fVersion:
					if len(c.payload) >= 12 {
						info.Version = fmt.Sprintf("%d.%d", int32(binary.LittleEndian.Uint32(c.payload)), int32(binary.LittleEndian.Uint32(c.payload[4:])))
						if bld := int32(binary.LittleEndian.Uint32(c.payload[8:])); bld != 0 {
							info.Version += fmt.Sprintf(".%d", bld)
						}
					}
				}
			}
		case fSupLangs:
			// the payload is one array field (typed)
			if af, _, err := readField(f.payload, 0, true); err == nil && af.typ == fArray {
				_, els := elements(af.payload)
				for _, e := range els {
					if len(e) >= 2 {
						langIDs = append(langIDs, int(binary.LittleEndian.Uint16(e)))
					}
				}
			}
		case fPrereq:
			// the first array: target devices; each a dependency (uid, version range, names)
			if af, _, err := readField(f.payload, 0, true); err == nil && af.typ == fArray {
				_, deps := elements(af.payload)
				for _, d := range deps {
					var uid uint32
					var names []string
					for _, c := range children(d) {
						switch c.typ {
						case fUID:
							if len(c.payload) >= 4 {
								uid = binary.LittleEndian.Uint32(c.payload)
							}
						case fArray:
							names = strings16(c.payload)
						}
					}
					if n := platformNames[uid]; n != "" {
						names = []string{n}
					}
					targets = append(targets, names)
				}
			}
		case fInstallBlk:
			files = collectFiles(f.payload, files)
		case fDataIndex:
			if len(f.payload) >= 4 {
				dataIndex = binary.LittleEndian.Uint32(f.payload)
			}
		}
	}
	pick := func(list []string) string { // the English name, else the first one
		for i, id := range langIDs {
			if (id == 1 || id == 10) && i < len(list) && list[i] != "" {
				return list[i]
			}
		}
		for _, s := range list {
			if s != "" {
				return s
			}
		}
		return ""
	}
	info.Name = strings.TrimSpace(pick(infoNames))
	if v := strings.TrimSpace(pick(infoVendors)); v != "" {
		info.Vendor = v
	}
	if info.UID != "" {
		info.AppID = "uid-" + info.UID
	}
	for _, id := range langIDs {
		if n := langNames[id]; n != "" {
			info.Languages = append(info.Languages, n)
		} else {
			info.Languages = append(info.Languages, fmt.Sprintf("language %d", id))
		}
	}
	seen := map[string]bool{}
	for _, t := range targets {
		for _, n := range t {
			if n = strings.TrimSpace(n); n != "" && !seen[n] {
				seen[n] = true
				info.Targets = append(info.Targets, n)
			}
		}
	}
	for _, f := range files {
		if f.op == 1 {
			info.Files++
		}
	}
	g := &sisData{units: data, dataIndex: dataIndex}
	if info.Arch = g.arch(files); info.Arch == nil {
		info.Arch = []string{"arm"}
	}
	g.icon(files, info)
	return info, nil
}

// collectFiles gathers the file descriptions of an install block, in the conditional branches too.
func collectFiles(block []byte, out []sisFile) []sisFile {
	for _, f := range children(block) {
		switch f.typ {
		case fArray:
			et, els := elements(f.payload)
			for _, e := range els {
				switch et {
				case fFileDes:
					if sf, ok := parseFileDes(e); ok {
						out = append(out, sf)
					}
				case fIf, fElseIf:
					out = collectFiles(e, out)
				}
			}
		case fInstallBlk, fIf, fElseIf:
			out = collectFiles(f.payload, out)
		}
	}
	return out
}

func parseFileDes(b []byte) (sisFile, bool) {
	var sf sisFile
	f, next, err := readField(b, 0, true)
	if err != nil || f.typ != fString {
		return sf, false
	}
	sf.target = u16s(f.payload)
	if f, next, err = readField(b, next, true); err != nil || f.typ != fString { // mime type
		return sf, false
	}
	for { // optional capabilities, then the hash
		f, n2, err := readField(b, next, true)
		if err != nil {
			return sf, false
		}
		next = n2
		if f.typ == fHash {
			break
		}
		if f.typ != fCaps {
			return sf, false
		}
	}
	if next+28 > len(b) {
		return sf, false
	}
	sf.op = binary.LittleEndian.Uint32(b[next:])
	sf.index = binary.LittleEndian.Uint32(b[next+24:])
	return sf, true
}

type sisData struct {
	units     []byte // the payload of SISData
	dataIndex uint32
}

// file returns the content of an installed file (at most limit bytes).
func (g *sisData) file(sf sisFile, limit int64) ([]byte, error) {
	if g.units == nil || sf.op != 1 {
		return nil, errors.New("no data")
	}
	af, _, err := readField(g.units, 0, true)
	if err != nil || af.typ != fArray {
		return nil, errSIS
	}
	_, us := elements(af.payload)
	if int(g.dataIndex) >= len(us) {
		return nil, errSIS
	}
	inner, _, err := readField(us[g.dataIndex], 0, true)
	if err != nil || inner.typ != fArray {
		return nil, errSIS
	}
	_, fds := elements(inner.payload)
	if int(sf.index) >= len(fds) {
		return nil, errSIS
	}
	cf, _, err := readField(fds[sf.index], 0, true)
	if err != nil || cf.typ != fCompressed {
		return nil, errSIS
	}
	return inflate(cf.payload, limit)
}

// arch tells the processor from the first executable the package installs (E32Image header: "EPOC" at 16, cpu at 122).
func (g *sisData) arch(files []sisFile) []string {
	for _, f := range files {
		t := strings.ToLower(f.target)
		if f.op != 1 || !(strings.HasSuffix(t, ".exe") || strings.HasSuffix(t, ".dll")) {
			continue
		}
		b, err := g.file(f, 16<<20)
		if err != nil || len(b) < 124 || string(b[16:20]) != "EPOC" {
			continue
		}
		switch cpu := binary.LittleEndian.Uint16(b[122:]); {
		case cpu == 0x1000:
			return []string{"x32"}
		case cpu >= 0x2000 && cpu < 0x3000:
			return []string{"arm"}
		}
	}
	return nil
}

var iconRef = regexp.MustCompile(`(?i)[A-Za-z0-9_$ .\\/-]{1,200}\.(mbm|mif|svg|aif)`)

func baseName(p string) string { return strings.ToLower(path.Base(strings.ReplaceAll(p, "\\", "/"))) }

// icon looks for the application's icon: the registration resource (xxx_reg.rsc) and the localisable resource name the icon file (searched as text,
// 8-bit and UTF-16); failing that, a picture file of the package whose name says "icon", "aif" or "app".
func (g *sisData) icon(files []sisFile, info *Info) {
	byBase := map[string]sisFile{}
	var rscs []sisFile
	for _, f := range files {
		if f.op != 1 {
			continue
		}
		bn := baseName(f.target)
		byBase[bn] = f
		lt := strings.ToLower(f.target)
		if strings.HasSuffix(bn, ".rsc") && (strings.Contains(bn, "_reg") || strings.Contains(bn, "_loc") || strings.Contains(lt, "resource\\apps") || strings.Contains(lt, "import\\apps")) {
			rscs = append(rscs, f)
		}
	}
	var cands []string
	for _, r := range rscs {
		b, err := g.file(r, 4<<20)
		if err != nil {
			continue
		}
		for _, m := range iconRef.FindAll(b, -1) {
			cands = append(cands, baseName(string(m)))
		}
		w := make([]byte, 0, len(b)/2) // UTF-16 text: keep the low bytes of printable pairs
		for i := 0; i+1 < len(b); i += 2 {
			if b[i+1] == 0 && b[i] >= 32 && b[i] < 127 {
				w = append(w, b[i])
			} else {
				w = append(w, 0)
			}
		}
		for _, m := range iconRef.FindAll(w, -1) {
			cands = append(cands, baseName(string(m)))
		}
	}
	for bn := range byBase { // fallback: a picture file named like an icon (map order does not matter: a stable sort of names below)
		if (strings.HasSuffix(bn, ".mbm") || strings.HasSuffix(bn, ".mif")) && (strings.Contains(bn, "icon") || strings.Contains(bn, "_aif") || strings.Contains(bn, "app")) {
			cands = append(cands, bn)
		}
	}
	sawScalable := false
	for _, c := range cands {
		f, ok := byBase[c]
		if !ok {
			continue
		}
		b, err := g.file(f, 16<<20)
		if err != nil {
			continue
		}
		switch {
		case strings.HasSuffix(c, ".mbm"):
			if png, err := MBMToPNG(b); err == nil {
				info.Icon, info.IconNote = png, ""
				return
			} else if info.IconNote == "" {
				info.IconNote = "the icon is a bitmap this reader cannot decode (" + err.Error() + ")"
			}
		case strings.HasSuffix(c, ".mif"), strings.HasSuffix(c, ".svg"):
			sawScalable = true
		}
	}
	if sawScalable && info.Icon == nil {
		info.IconNote = "the package has a scalable icon (MIF / SVG-T), which is not shown here"
	}
}
