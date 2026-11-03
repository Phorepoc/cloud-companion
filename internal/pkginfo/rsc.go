// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package pkginfo

// Symbian resource files (.rsc, as RResourceFile reads them; secureswitools/swisistools/source/rscparser/barscimpl.cpp). The plain (not dictionary
// compressed) layout: the last u16 of the file is the position of the resource index; the index is one u16 file offset per resource, resource n
// (numbered from 1) running to the next offset (the last to the start of the index). Unicode text (LTEXT with the Unicode build) is a byte holding
// the number of characters, then the UTF-16 characters. Dictionary-compressed files are not read (callers fall back to searching the bytes).

import (
	"encoding/binary"
	"errors"
	"path"
	"strings"
	"unicode/utf16"
)

// rscResources splits a plain resource file into its resources.
func rscResources(b []byte) ([][]byte, error) {
	if len(b) < 8 {
		return nil, errors.New("short resource file")
	}
	if uid1 := binary.LittleEndian.Uint32(b); uid1 == 0x101f5010 {
		return nil, errors.New("dictionary-compressed resource file")
	}
	start := int(binary.LittleEndian.Uint16(b[len(b)-2:]))
	if start < 4 || start >= len(b) || (len(b)-start)%2 != 0 {
		return nil, errors.New("bad resource index")
	}
	n := (len(b) - start) / 2
	offs := make([]int, n+1)
	for i := 0; i < n; i++ {
		offs[i] = int(binary.LittleEndian.Uint16(b[start+2*i:]))
	}
	offs[n] = start
	out := make([][]byte, n)
	for i := 0; i < n; i++ {
		if offs[i] > offs[i+1] || offs[i+1] > len(b) {
			return nil, errors.New("bad resource offsets")
		}
		out[i] = b[offs[i]:offs[i+1]]
	}
	return out, nil
}

// rscTexts lists the text fields a resource appears to hold: counted UTF-16 strings and counted 8-bit strings of printable characters.
func rscTexts(res []byte) []string {
	var out []string
	printable := func(r rune) bool { return r >= 32 && r < 0x2000 }
	for i := 0; i < len(res); i++ {
		n := int(res[i])
		if n < 2 {
			continue
		}
		if i+1+2*n <= len(res) {
			u := make([]uint16, n)
			ok := true
			for k := range u {
				u[k] = binary.LittleEndian.Uint16(res[i+1+2*k:])
				if !printable(rune(u[k])) {
					ok = false
					break
				}
			}
			if ok {
				out = append(out, string(utf16.Decode(u)))
				i += 2 * n
				continue
			}
		}
		if i+1+n <= len(res) {
			s := res[i+1 : i+1+n]
			ok := true
			for _, c := range s {
				if c < 32 || c > 126 {
					ok = false
					break
				}
			}
			if ok {
				out = append(out, string(s))
				i += n
			}
		}
	}
	return out
}

func isIconName(s string) bool {
	l := strings.ToLower(s)
	return strings.HasSuffix(l, ".mif") || strings.HasSuffix(l, ".mbm") || strings.HasSuffix(l, ".svg")
}

// regInfo is what an application registration file (xxx_reg.rsc) names: the localisable resource file (xxx_loc) and any icon file.
type regInfo struct {
	locFile string   // base name without extension, lower case
	refs    []string // stems of every path-like text (the localisable resource file is named without extension)
	icons   []string // icon files named in it, in order
}

func parseReg(b []byte) regInfo {
	var ri regInfo
	res, err := rscResources(b)
	var texts []string
	if err != nil {
		texts = rscTexts(b)
	} else {
		for _, r := range res {
			texts = append(texts, rscTexts(r)...)
		}
	}
	for _, t := range texts {
		if (strings.Contains(t, "\\") || strings.Contains(t, "/")) && !strings.Contains(t, " ") {
			ri.refs = append(ri.refs, strings.TrimSuffix(baseName(t), path.Ext(baseName(t))))
		}
		switch {
		case isIconName(t):
			ri.icons = append(ri.icons, t)
		case false:
		case ri.locFile == "" && strings.Contains(strings.ToLower(t), "_loc") && !strings.Contains(t, " "):
			ri.locFile = baseName(t)
		}
	}
	return ri
}

// locIcons returns the icon file names a localisable info resource (xxx_loc.rsc: caption, number of icons, icon file) names.
func locIcons(b []byte) []string {
	var out []string
	res, err := rscResources(b)
	var texts []string
	if err != nil {
		texts = rscTexts(b)
	} else {
		for _, r := range res {
			texts = append(texts, rscTexts(r)...)
		}
	}
	for _, t := range texts {
		if isIconName(t) {
			out = append(out, t)
		}
	}
	return out
}
