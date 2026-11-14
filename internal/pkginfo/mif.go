// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package pkginfo

// S60 icon files (.mif), as AknIconLoader reads them (oss.FCL.sf.mw.uiresources/uigraphics/AknIcon):
//   header: i32 uid 0x34232342, i32 version (1..3), i32 offset of the index, i32 number of index entries, (version 3: i32 uid of an index DLL)
//   index entry: i32 offset, i32 length of the icon; from version 2 an offset <= 0 means "bitmap number -offset of the .mbm file of the same name"
//   icon: i32 uid 0x34232343, i32 version 1, i32 data offset (from the start of the icon), i32 data length, i32 type (0 bitmap, 1 SVG-T binary, 2 NVG),
//         i32 depth, i32 animated, i32 mask depth.
// In version 1 an icon takes two numbers (picture and mask), so icon n is entry n.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

const (
	mifFileUID = 0x34232342
	mifIconUID = 0x34232343
)

// MifIcon is one icon of a .mif file.
type MifIcon struct {
	Type  int    // 0 bitmap, 1 SVG-T binary, 2 NVG
	Data  []byte // the vector data (types 1, 2) or an embedded bitmap file
	MBMID int    // for a bitmap kept in the .mbm file beside the .mif: its number, else -1
}

// ParseMIF lists the icons of a .mif file.
func ParseMIF(b []byte) (icons []MifIcon, err error) {
	defer func() {
		if r := recover(); r != nil {
			icons, err = nil, errors.New("the icon file is damaged")
		}
	}()
	if len(b) < 16 || int32(binary.LittleEndian.Uint32(b)) != mifFileUID {
		return nil, errors.New("not an S60 icon file")
	}
	i32 := func(o int) int {
		if o < 0 || o+4 > len(b) {
			panic("range")
		}
		return int(int32(binary.LittleEndian.Uint32(b[o:])))
	}
	ver := i32(4)
	if ver < 1 || ver > 3 {
		return nil, fmt.Errorf("icon file version %d", ver)
	}
	off, n := i32(8), i32(12)
	if n < 0 || n > 1<<16 || off < 16 || off+8*n > len(b) {
		if ver == 3 {
			return nil, errors.New("the icon index is kept in a separate library")
		}
		return nil, errors.New("bad icon index")
	}
	for k := 0; k < n; k++ {
		o, l := i32(off+8*k), i32(off+8*k+4)
		if ver >= 2 && o <= 0 {
			icons = append(icons, MifIcon{Type: 0, MBMID: -o})
			continue
		}
		if o < 0 || l < 32 || o+l > len(b) || i32(o) != mifIconUID {
			return nil, errors.New("bad icon entry")
		}
		do, dl, typ := i32(o+8), i32(o+12), i32(o+16)
		if do < 32 || dl < 0 || do+dl > l {
			return nil, errors.New("bad icon data")
		}
		icons = append(icons, MifIcon{Type: typ, Data: b[o+do : o+do+dl], MBMID: -1})
	}
	return icons, nil
}

// MIFIcon renders icon number index (0 = the application icon) of a .mif file to PNG. mbm is the .mbm of the same name, when the package has one.
// svg is the vector source (empty for a bitmap icon).
func MIFIcon(mif, mbm []byte, index int) (png []byte, svg string, err error) {
	icons, err := ParseMIF(mif)
	if err != nil {
		return nil, "", err
	}
	if index < 0 || index >= len(icons) {
		return nil, "", errors.New("the icon file has no such icon")
	}
	ic := icons[index]
	switch {
	case ic.MBMID >= 0:
		if mbm == nil {
			return nil, "", errors.New("the bitmap file of the icons is not in the package")
		}
		png, err = MBMBitmapToPNG(mbm, ic.MBMID)
		return png, "", err
	case ic.Type == 2:
		svg, err = NVGToSVG(ic.Data)
	case ic.Type == 1:
		if len(ic.Data) >= 4 && binary.LittleEndian.Uint32(ic.Data) == svgbMagic {
			svg, err = SVGBToSVG(ic.Data)
		} else if s := strings.TrimSpace(string(ic.Data)); strings.HasPrefix(s, "<") {
			svg = s
		} else {
			err = errors.New("unknown SVG icon encoding")
		}
	case ic.Type == 0:
		png, err = MBMToPNG(ic.Data)
		return png, "", err
	default:
		err = fmt.Errorf("icon type %d", ic.Type)
	}
	if err != nil {
		return nil, "", err
	}
	png, err = SVGToPNG(svg, 96)
	if err != nil {
		return nil, svg, err
	}
	return png, svg, nil
}
