// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package pkginfo

// Symbian multi-bitmap files (.mbm): 16-byte header (UIDs 0x10000037, 0x10000042), the trailer offset at 16; the trailer is a count and the offsets
// of the bitmaps; each bitmap has a 40-byte header (size, header size, width, height, twips w/h, bits per pixel, colour flag, palette entries,
// compression) and rows padded to 4 bytes, raw or run-length coded (byte / 12 / 16 / 24 bit units). Icons come as (picture, mask) pairs.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

type mbmBitmap struct {
	w, h, bpp, colour, comp int
	data                    []byte // decompressed rows
}

func le32(b []byte, o int) int { return int(int32(binary.LittleEndian.Uint32(b[o:]))) }

func parseMBM(b []byte) ([]mbmBitmap, error) {
	if len(b) < 24 || binary.LittleEndian.Uint32(b) != 0x10000037 {
		return nil, errors.New("not a multi-bitmap file")
	}
	tr := le32(b, 16)
	if tr < 0 || tr+4 > len(b) {
		return nil, errors.New("bad trailer")
	}
	n := le32(b, tr)
	if n < 0 || n > 4096 || tr+4+4*n > len(b) {
		return nil, errors.New("bad bitmap count")
	}
	var out []mbmBitmap
	for i := 0; i < n; i++ {
		off := le32(b, tr+4+4*i)
		if off < 0 || off+40 > len(b) {
			return nil, errors.New("bad bitmap offset")
		}
		size, hs := le32(b, off), le32(b, off+4)
		bm := mbmBitmap{w: le32(b, off+8), h: le32(b, off+12), bpp: le32(b, off+24), colour: le32(b, off+28), comp: le32(b, off+36)}
		if hs < 40 || size < hs || off+size > len(b) || bm.w <= 0 || bm.h <= 0 || bm.w > 4096 || bm.h > 4096 {
			return nil, errors.New("bad bitmap header")
		}
		raw := b[off+hs : off+size]
		stride := ((bm.w*bm.bpp + 31) / 32) * 4
		total := stride * bm.h
		var err error
		if bm.data, err = unrle(raw, bm.comp, total); err != nil {
			return nil, err
		}
		out = append(out, bm)
	}
	return out, nil
}

// unrle expands the run-length coding of a bitmap's rows to total bytes.
func unrle(raw []byte, comp, total int) ([]byte, error) {
	unit := 0
	switch comp {
	case 0:
		if len(raw) < total {
			return nil, errors.New("short bitmap data")
		}
		return raw[:total], nil
	case 1:
		unit = 1
	case 2, 3:
		unit = 2
	case 4, 5:
		unit = 3
	case 6:
		unit = 4
	default:
		return nil, fmt.Errorf("compression %d", comp)
	}
	out := make([]byte, 0, total)
	for i := 0; i < len(raw) && len(out) < total; {
		c := int(int8(raw[i]))
		i++
		if c >= 0 {
			if i+unit > len(raw) {
				break
			}
			for k := 0; k <= c; k++ {
				out = append(out, raw[i:i+unit]...)
			}
			i += unit
		} else {
			n := (-c) * unit
			if i+n > len(raw) {
				n = len(raw) - i
			}
			out = append(out, raw[i:i+n]...)
			i += n
		}
	}
	if len(out) < total {
		return nil, errors.New("short bitmap data")
	}
	return out[:total], nil
}

func (bm mbmBitmap) pixel(x, y int) (r, g, b, a uint8) {
	stride := ((bm.w*bm.bpp + 31) / 32) * 4
	row := bm.data[y*stride:]
	switch bm.bpp {
	case 1:
		v := uint8(0)
		if row[x/8]>>(x%8)&1 != 0 {
			v = 255
		}
		return v, v, v, 255
	case 2:
		v := (row[x/4] >> (2 * (x % 4))) & 3 * 85
		return v, v, v, 255
	case 4:
		v := (row[x/2] >> (4 * (x % 2))) & 15 * 17
		return v, v, v, 255
	case 8:
		v := row[x]
		return v, v, v, 255
	case 12:
		p := uint16(row[2*x]) | uint16(row[2*x+1])<<8
		return uint8(p>>8&15) * 17, uint8(p>>4&15) * 17, uint8(p&15) * 17, 255
	case 16:
		p := uint16(row[2*x]) | uint16(row[2*x+1])<<8
		return uint8(p >> 11 << 3), uint8(p >> 5 & 63 << 2), uint8(p & 31 << 3), 255
	case 24:
		return row[3*x+2], row[3*x+1], row[3*x], 255
	case 32:
		return row[4*x+2], row[4*x+1], row[4*x], row[4*x+3]
	}
	return 0, 0, 0, 255
}

// MBMToPNG decodes the best icon of a multi-bitmap file: the largest (picture, mask) pair, or the largest picture, as a PNG with the mask as alpha.
func MBMToPNG(b []byte) ([]byte, error) {
	bms, err := parseMBM(b)
	if err != nil {
		return nil, err
	}
	if len(bms) == 0 {
		return nil, errors.New("no bitmaps")
	}
	best, bestMask, area := -1, -1, 0
	for i := 0; i < len(bms); i++ {
		a := bms[i].w * bms[i].h
		mask := -1
		if i+1 < len(bms) && bms[i+1].w == bms[i].w && bms[i+1].h == bms[i].h && bms[i+1].bpp <= 8 && bms[i].bpp > 1 {
			mask = i + 1
		}
		if a > area || (a == area && mask >= 0 && bestMask < 0) {
			best, bestMask, area = i, mask, a
		}
		if mask >= 0 {
			i++ // the mask belongs to this picture
		}
	}
	return renderMBM(bms, best, bestMask)
}

// MBMBitmapToPNG decodes bitmap id of a multi-bitmap file with the following bitmap as its mask when it has the same size (icons are stored as such pairs).
func MBMBitmapToPNG(b []byte, id int) ([]byte, error) {
	bms, err := parseMBM(b)
	if err != nil {
		return nil, err
	}
	if id < 0 || id >= len(bms) {
		return nil, errors.New("no such bitmap")
	}
	mask := -1
	if id+1 < len(bms) && bms[id+1].w == bms[id].w && bms[id+1].h == bms[id].h && bms[id+1].bpp <= 8 && bms[id].bpp > 1 {
		mask = id + 1
	}
	return renderMBM(bms, id, mask)
}

func renderMBM(bms []mbmBitmap, best, bestMask int) ([]byte, error) {
	pic := bms[best]
	img := image.NewNRGBA(image.Rect(0, 0, pic.w, pic.h))
	for y := 0; y < pic.h; y++ {
		for x := 0; x < pic.w; x++ {
			r, g, bl, a := pic.pixel(x, y)
			if bestMask >= 0 {
				mr, _, _, _ := bms[bestMask].pixel(x, y)
				a = mr
			}
			img.SetNRGBA(x, y, color.NRGBA{r, g, bl, a})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
