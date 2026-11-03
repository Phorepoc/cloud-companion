// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package pkginfo

// NVG-CS, the vector icon format of S60 (Symbian^3 / Belle) icon files, read back into SVG. The layout is the one documented by the encoder
// pdk/tools/svg2nvg.py (written from the release's nvgdecoder, svgtopt/nvgdecoder/src/nvg.cpp):
//
//	0   "nvg", version 2, header size u16 (52)                       26  path data type i16 (3 = 16.16 fixed point)
//	28  6 floats: 16.0, 0.0, then the viewBox x, y, width, height
//	52  u16 section count N, N u16 file offsets (paint sections first, then path sections), pad to 4 bytes
//	    u16 command count, u16 0, the commands, u16 path count
//	command word = op<<24 | b<<16 | index:  4 fill and 5 stroke (b = alpha, index = paint section), 6 ramp (index = ramp section, follows a gradient
//	    paint), 7 path (b = flags: bit 0 stroke, bit 1 fill; index = section number), 8 transform (b = type; floats follow), 9 stroke width (float),
//	    10 join/cap (join<<8|cap), 11 miter limit (float)
//	paint section: word type 1 = solid (then 0xRRGGBBAA), 2 linear gradient (4 floats), 3 radial (5 floats), bit 16 of the word: a 6 float matrix follows;
//	    type 4 = colour ramp (count in the high half, then 5 floats per stop: offset, r, g, b, opacity)
//	path section: u16 segment count, segment bytes (OpenVG: 0 close, 2 move, 4 line, 6 hline, 8 vline, 10 quad, 12 cubic, 14 smooth quad,
//	    16 smooth cubic, 18..24 arcs; | 1 = relative), pad to 4, then 16.16 coordinates.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
)

type nvgReader struct {
	b []byte
}

func (r nvgReader) u16(o int) int {
	if o < 0 || o+2 > len(r.b) {
		panic("nvg: out of range")
	}
	return int(binary.LittleEndian.Uint16(r.b[o:]))
}
func (r nvgReader) u32(o int) uint32 {
	if o < 0 || o+4 > len(r.b) {
		panic("nvg: out of range")
	}
	return binary.LittleEndian.Uint32(r.b[o:])
}
func (r nvgReader) f32(o int) float64 { return float64(math.Float32frombits(r.u32(o))) }
func (r nvgReader) fix(o int) float64 { return float64(int32(r.u32(o))) / 65536 }

func ff(v float64) string {
	s := fmt.Sprintf("%.5f", v)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}

// the number of coordinates of each segment type (without the relative bit)
var nvgCoords = map[int]int{0: 0, 2: 2, 4: 2, 6: 1, 8: 1, 10: 4, 12: 6, 14: 2, 16: 4, 18: 5, 20: 5, 22: 5, 24: 5}
var nvgLetter = map[int]string{0: "z", 2: "m", 4: "l", 6: "h", 8: "v", 10: "q", 12: "c", 14: "t", 16: "s", 18: "a", 20: "a", 22: "a", 24: "a"}

// NVGToSVG turns an NVG-CS icon into an SVG document (user-space gradients, one <path> per path command).
func NVGToSVG(b []byte) (svg string, err error) {
	defer func() {
		if r := recover(); r != nil {
			svg, err = "", errors.New("the NVG icon is damaged")
		}
	}()
	if len(b) < 60 || string(b[:3]) != "nvg" {
		return "", errors.New("not an NVG icon")
	}
	r := nvgReader{b}
	vb := [4]float64{r.f32(36), r.f32(40), r.f32(44), r.f32(48)}
	if vb[2] <= 0 || vb[3] <= 0 {
		return "", errors.New("the NVG icon has no size")
	}
	nofs := r.u16(52)
	offs := make([]int, nofs)
	for i := range offs {
		offs[i] = r.u16(54 + 2*i)
	}
	p := 54 + 2*nofs
	if nofs%2 == 0 {
		p += 2
	}
	ncmd := r.u16(p)
	p += 4

	var defs, body strings.Builder
	ngrad := 0
	paintOf := func(idx int, alpha float64, ramp int) (string, string) { // -> fill value, opacity
		o := offs[idx]
		w := r.u32(o)
		switch w & 0xffff {
		case 1:
			c := r.u32(o + 4)
			return fmt.Sprintf("#%02x%02x%02x", c>>24, c>>16&255, c>>8&255), ff(alpha)
		case 2, 3:
			if ramp < 0 {
				return "none", "1"
			}
			ngrad++
			id := fmt.Sprintf("g%d", ngrad)
			q := o + 4
			var tag, geom string
			if w&0xffff == 2 {
				tag = "linearGradient"
				geom = fmt.Sprintf(`x1="%s" y1="%s" x2="%s" y2="%s"`, ff(r.f32(q)), ff(r.f32(q+4)), ff(r.f32(q+8)), ff(r.f32(q+12)))
				q += 16
			} else {
				tag = "radialGradient"
				geom = fmt.Sprintf(`cx="%s" cy="%s" fx="%s" fy="%s" r="%s"`, ff(r.f32(q)), ff(r.f32(q+4)), ff(r.f32(q+8)), ff(r.f32(q+12)), ff(r.f32(q+16)))
				q += 20
			}
			tr := ""
			if w>>16&1 != 0 { // a, c, e, b, d, f
				tr = fmt.Sprintf(` gradientTransform="matrix(%s %s %s %s %s %s)"`, ff(r.f32(q)), ff(r.f32(q+12)), ff(r.f32(q+4)), ff(r.f32(q+16)), ff(r.f32(q+8)), ff(r.f32(q+20)))
			}
			ro := offs[ramp]
			n := int(r.u32(ro) >> 16)
			fmt.Fprintf(&defs, `<%s id="%s" gradientUnits="userSpaceOnUse" %s%s>`, tag, id, geom, tr)
			for i := 0; i < n; i++ {
				s := ro + 4 + 20*i
				fmt.Fprintf(&defs, `<stop offset="%s" stop-color="#%02x%02x%02x" stop-opacity="%s"/>`, ff(r.f32(s)),
					int(math.Round(r.f32(s+4)*255)), int(math.Round(r.f32(s+8)*255)), int(math.Round(r.f32(s+12)*255)), ff(r.f32(s+16)))
			}
			fmt.Fprintf(&defs, `</%s>`, tag)
			return "url(#" + id + ")", ff(alpha)
		}
		return "none", "1"
	}

	var fill, stroke, fillOp, strokeOp = "black", "none", "1", "1"
	width, join, cap, miter := 1.0, 2, 1, 4.0
	xform := ""
	lastKind := byte(0) // the paint command a ramp belongs to: 4 fill, 5 stroke
	var lastIdx int
	var lastAlpha float64
	for i := 0; i < ncmd; i++ {
		w := r.u32(p)
		op := byte(w >> 24)
		mid := int(w >> 16 & 255)
		idx := int(w & 0xffff)
		p += 4
		switch op {
		case 4, 5:
			lastKind, lastIdx, lastAlpha = op, idx, float64(mid)/255
			v, o := paintOf(idx, lastAlpha, -1)
			if op == 4 {
				fill, fillOp = v, o
			} else {
				stroke, strokeOp = v, o
			}
		case 6:
			v, o := paintOf(lastIdx, lastAlpha, idx)
			if lastKind == 4 {
				fill, fillOp = v, o
			} else if lastKind == 5 {
				stroke, strokeOp = v, o
			}
		case 8:
			t := mid
			f := func(n int) []float64 {
				out := make([]float64, n)
				for k := range out {
					out[k] = r.f32(p + 4*k)
				}
				p += 4 * n
				return out
			}
			switch {
			case t == 1:
				xform = ""
			case t == 0:
				v := f(6) // a d c b e f
				xform = fmt.Sprintf(` transform="matrix(%s %s %s %s %s %s)"`, ff(v[0]), ff(v[3]), ff(v[2]), ff(v[1]), ff(v[4]), ff(v[5]))
			default:
				a, d, e, g := 1.0, 1.0, 0.0, 0.0
				if t&2 != 0 {
					v := f(2)
					a, d = v[0], v[1]
				}
				if t&16 != 0 {
					v := f(2)
					e, g = v[0], v[1]
				}
				xform = fmt.Sprintf(` transform="matrix(%s 0 0 %s %s %s)"`, ff(a), ff(d), ff(e), ff(g))
			}
		case 9:
			width = r.f32(p)
			p += 4
		case 10:
			join, cap = int(w>>8&255), int(w&255)
		case 11:
			miter = r.f32(p)
			p += 4
		case 7:
			flags := mid
			so := offs[idx]
			nseg := r.u16(so)
			segs := b[so+2 : so+2+nseg]
			co := so + 2 + nseg
			co += (4 - co%4) % 4
			var d strings.Builder
			for _, s := range segs {
				t := int(s) &^ 1
				k, ok := nvgCoords[t]
				if !ok {
					return "", fmt.Errorf("unknown path segment %d", s)
				}
				letter := nvgLetter[t]
				if s&1 == 0 && letter != "z" {
					letter = strings.ToUpper(letter)
				} else if letter == "z" {
					letter = "z"
				}
				d.WriteString(letter)
				if t >= 18 {
					rx, ry, rot, x, y := r.fix(co), r.fix(co+4), r.fix(co+8), r.fix(co+12), r.fix(co+16)
					large, sweep := 0, 0
					if t == 22 || t == 24 {
						large = 1
					}
					if t == 20 || t == 24 {
						sweep = 1
					}
					fmt.Fprintf(&d, "%s %s %s %d %d %s %s", ff(rx), ff(ry), ff(rot), large, sweep, ff(x), ff(y))
					co += 20
					continue
				}
				for j := 0; j < k; j++ {
					if j > 0 {
						d.WriteByte(' ')
					}
					d.WriteString(ff(r.fix(co)))
					co += 4
				}
			}
			fv, fo, sv, so2 := "none", "1", "none", "1"
			if flags&2 != 0 {
				fv, fo = fill, fillOp
			}
			if flags&1 != 0 {
				sv, so2 = stroke, strokeOp
			}
			fmt.Fprintf(&body, `<path d="%s" fill="%s" fill-opacity="%s"`, d.String(), fv, fo)
			if sv != "none" {
				lj := map[int]string{1: "bevel", 2: "miter", 3: "round"}[join]
				lc := map[int]string{1: "butt", 2: "square", 3: "round"}[cap]
				fmt.Fprintf(&body, ` stroke="%s" stroke-opacity="%s" stroke-width="%s" stroke-linejoin="%s" stroke-linecap="%s" stroke-miterlimit="%s"`, sv, so2, ff(width), lj, lc, ff(miter))
			}
			body.WriteString(xform + "/>\n")
		default:
			return "", fmt.Errorf("unknown NVG command %d", op)
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" version="1.1" width="%s" height="%s" viewBox="%s %s %s %s">`+"\n", ff(vb[2]), ff(vb[3]), ff(vb[0]), ff(vb[1]), ff(vb[2]), ff(vb[3]))
	if defs.Len() > 0 {
		out.WriteString("<defs>" + defs.String() + "</defs>\n")
	}
	out.WriteString(body.String() + "</svg>\n")
	return out.String(), nil
}
