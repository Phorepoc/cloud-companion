// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package pkginfo

// SVG-T binary, the engine's DOM written element by element (documented by pdk/tools/svg2svgb.py, read by svgtopt/SVG/SVGEngine/src/Svgdecoder.cpp):
//   u32 magic 66737870, then per element: u8 element id, attributes (u16 id + typed value) until the u16 1000, the children, u8 254; the file ends with u8 255.
// Numbers are 16.16 fixed point, colours 0x00RRGGBB (0x01ffffff = none).

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

const (
	svgbMagic = 66737870
	svgbStart = 1000
	svgbEnd   = 254
	svgbFile  = 255
	svgbCData = 253
)

var svgbElements = map[byte]string{0: "svg", 3: "defs", 4: "desc", 7: "title", 11: "g", 15: "switch", 18: "a", 22: "image", 25: "text", 26: "use",
	27: "circle", 28: "ellipse", 29: "line", 30: "path", 31: "polygon", 32: "polyline", 33: "rect", 40: "linearGradient", 41: "radialGradient", 42: "stop",
	44: "textArea", 46: "solidColor", 24: "style"}

type svgbAttr struct {
	name string
	kind string
}

// svgbAttrs: attribute id -> name and value kind (the inverse of the encoder's table; ids above 25 are offset by 25).
var svgbAttrs = func() map[int]svgbAttr {
	const o = 25
	m := map[int]svgbAttr{}
	add := func(id int, name, kind string) { m[id] = svgbAttr{name, kind} }
	for id, a := range map[int][2]string{0: {"fill", "paint"}, 1: {"stroke", "color"}, 2: {"stroke-width", "fix"}, 3: {"visibility", "enum"}, 4: {"font-family", "str"},
		5: {"font-size", "fix"}, 6: {"font-style", "enum"}, 7: {"font-weight", "enum"}, 8: {"stroke-dasharray", "dash"}, 9: {"display", "enum"}, 10: {"fill-rule", "str"},
		11: {"stroke-linecap", "str"}, 12: {"stroke-linejoin", "str"}, 13: {"stroke-dashoffset", "fix"}, 14: {"stroke-miterlimit", "fix"}, 15: {"color", "color"},
		16: {"text-anchor", "enum"}, 17: {"text-decoration", "enum"}, 22: {"fill-opacity", "fix"}, 23: {"stroke-opacity", "fix"}, 24: {"opacity", "fix"}} {
		add(id, a[0], a[1])
	}
	for id, a := range map[int][2]string{1: {"width", "fix"}, 2: {"height", "fix"}, 3: {"r", "fix"}, 4: {"rx", "fix"}, 5: {"ry", "fix"}, 21: {"cx", "fix"}, 22: {"cy", "fix"},
		23: {"y", "fix"}, 24: {"x", "fix"}, 25: {"y1", "fix"}, 26: {"y2", "fix"}, 27: {"x1", "fix"}, 28: {"x2", "fix"}, 41: {"transform", "transform"},
		51: {"version", "fix"}, 53: {"points", "path"}, 54: {"d", "path"}, 56: {"stop-color", "color"}, 57: {"fx", "fix"}, 58: {"fy", "fix"}, 59: {"offset", "fix"},
		60: {"spreadMethod", "spread"}, 61: {"gradientUnits", "units"}, 62: {"stop-opacity", "fix"}, 63: {"viewBox", "viewbox"}, 64: {"baseProfile", "str"},
		65: {"zoomAndPan", "zoom"}, 67: {"id", "str"}, 68: {"xml:base", "str"}, 69: {"xml:lang", "str"}, 70: {"xml:space", "str"}, 71: {"requiredExtensions", "list"},
		72: {"requiredFeatures", "list"}, 73: {"systemLanguage", "list"}, 78: {"xlink:actuate", "href"}, 79: {"xlink:arcrole", "href"}, 80: {"xlink:role", "href"},
		81: {"xlink:show", "href"}, 82: {"xlink:title", "href"}, 83: {"xlink:type", "href"}, 84: {"xlink:href", "href"}, 98: {"gradientTransform", "gtransform"},
		111: {"xlink:href", "data"}} {
		add(id+o, a[0], a[1])
	}
	return m
}()

type svgbReader struct {
	b []byte
	p int
}

func (r *svgbReader) need(n int) {
	if r.p+n > len(r.b) {
		panic("svgb: out of range")
	}
}
func (r *svgbReader) u8() byte { r.need(1); v := r.b[r.p]; r.p++; return v }
func (r *svgbReader) u16() int {
	r.need(2)
	v := binary.LittleEndian.Uint16(r.b[r.p:])
	r.p += 2
	return int(v)
}
func (r *svgbReader) u32() uint32 {
	r.need(4)
	v := binary.LittleEndian.Uint32(r.b[r.p:])
	r.p += 4
	return v
}
func (r *svgbReader) i32() int32    { return int32(r.u32()) }
func (r *svgbReader) fix() string   { return ff(float64(r.i32()) / 65536) }
func (r *svgbReader) fixf() float64 { return float64(r.i32()) / 65536 }
func (r *svgbReader) str() string {
	n := int(r.u8())
	r.need(n)
	u := make([]uint16, n/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(r.b[r.p+2*i:])
	}
	r.p += n
	return string(utf16.Decode(u))
}

func colorStr(c uint32) string {
	if c == 0x01ffffff {
		return "none"
	}
	return fmt.Sprintf("#%06x", c&0xffffff)
}

// pathD reads a path value (types, coordinates) as SVG path data.
func (r *svgbReader) pathD() string {
	n := r.u16()
	r.need(n)
	types := r.b[r.p : r.p+n]
	r.p += n
	nc := r.u16()
	var d strings.Builder
	k := 0
	next := func() string {
		if k >= nc {
			panic("svgb: path coordinates")
		}
		k++
		return r.fix()
	}
	for _, t := range types {
		switch t {
		case 0:
			d.WriteString("M" + next() + " " + next())
		case 1:
			d.WriteString("L" + next() + " " + next())
		case 2:
			d.WriteString("Q" + next() + " " + next() + " " + next() + " " + next())
		case 3:
			d.WriteString("C" + next() + " " + next() + " " + next() + " " + next() + " " + next() + " " + next())
		case 4:
			d.WriteString("Z")
		default:
			panic("svgb: path type")
		}
	}
	return d.String()
}

// SVGBToSVG turns an SVG-T binary icon into SVG text.
func SVGBToSVG(b []byte) (svg string, err error) {
	defer func() {
		if r := recover(); r != nil {
			svg, err = "", errors.New("the SVG-T icon is damaged")
		}
	}()
	if len(b) < 6 || binary.LittleEndian.Uint32(b) != svgbMagic {
		return "", errors.New("not an SVG-T binary icon")
	}
	r := &svgbReader{b: b, p: 4}
	var out strings.Builder
	depth := 0
	for r.p < len(b) {
		id := r.u8()
		if id == svgbFile {
			break
		}
		if id == svgbEnd {
			return "", errors.New("misplaced element end")
		}
		name := svgbElements[id]
		if name == "" {
			return "", fmt.Errorf("unknown SVG-T element %d", id)
		}
		if err := r.element(&out, name, &depth); err != nil {
			return "", err
		}
		break
	}
	return out.String(), nil
}

func (r *svgbReader) element(out *strings.Builder, name string, depth *int) error {
	if *depth > 64 {
		return errors.New("too deeply nested")
	}
	*depth++
	defer func() { *depth-- }()
	type kv struct{ k, v string }
	var attrs []kv
	var points string
	for {
		aid := r.u16()
		if aid == svgbStart {
			break
		}
		a, ok := svgbAttrs[aid]
		if !ok {
			return fmt.Errorf("unknown SVG-T attribute %d", aid)
		}
		if a.kind == "href" && name == "use" {
			ref := r.str()
			r.str()
			attrs = append(attrs, kv{"xlink:href", ref})
			continue
		}
		if a.kind == "data" {
			n := int(r.i32())
			r.need(n)
			attrs = append(attrs, kv{"xlink:href", string(r.b[r.p : r.p+n])})
			r.p += n
			continue
		}
		if name == "svg" && (a.name == "width" || a.name == "height") {
			pct := r.u8()
			v := r.fix()
			if pct == 1 {
				v += "%"
			}
			attrs = append(attrs, kv{a.name, v})
			continue
		}
		var v string
		switch a.kind {
		case "fix":
			v = r.fix()
		case "str":
			v = r.str()
		case "color":
			v = colorStr(r.u32())
		case "paint":
			if r.u8() == 1 {
				v = "url(#" + r.str() + ")"
			} else {
				v = colorStr(r.u32())
			}
		case "enum":
			e := r.i32()
			switch a.name {
			case "display":
				v = map[int32]string{0: "inline", 16: "none"}[e]
			case "visibility":
				v = map[int32]string{0: "visible", 1: "hidden"}[e]
			}
		case "dash":
			n := int(int8(r.u8()))
			var parts []string
			for i := 0; i < n; i++ {
				parts = append(parts, r.fix())
			}
			v = strings.Join(parts, " ")
		case "path":
			v = r.pathD()
			if a.name == "points" {
				points = v
				continue
			}
		case "viewbox":
			v = r.fix() + " " + r.fix() + " " + r.fix() + " " + r.fix()
		case "zoom":
			r.u8()
		case "units":
			if r.u8() == 0 {
				v = "userSpaceOnUse"
			} else {
				v = "objectBoundingBox"
			}
		case "spread":
			v = map[byte]string{0: "pad", 1: "reflect", 2: "repeat"}[r.u8()]
		case "list":
			n := int(int8(r.u8()))
			var parts []string
			for i := 0; i < n; i++ {
				parts = append(parts, r.str())
			}
			v = strings.Join(parts, " ")
		case "href":
			r.str()
		case "transform", "gtransform":
			m00, m01, m02, m10, m11, m12 := r.fixf(), r.fixf(), r.fixf(), r.fixf(), r.fixf(), r.fixf()
			if a.kind == "transform" {
				r.u32()
			}
			v = fmt.Sprintf("matrix(%s %s %s %s %s %s)", ff(m00), ff(m10), ff(m01), ff(m11), ff(m02), ff(m12))
		}
		if v != "" {
			attrs = append(attrs, kv{a.name, v})
		}
	}
	if name == "polygon" || name == "polyline" {
		name = "path"
		if points != "" {
			attrs = append(attrs, kv{"d", points})
		}
	}
	out.WriteString("<" + name)
	if name == "svg" {
		out.WriteString(` xmlns="http://www.w3.org/2000/svg"`)
	}
	for _, a := range attrs {
		if strings.HasPrefix(a.k, "xlink:") {
			continue
		}
		fmt.Fprintf(out, ` %s="%s"`, a.k, xmlEsc(a.v))
	}
	out.WriteString(">")
	for {
		id := r.u8()
		if id == svgbEnd {
			break
		}
		if id == svgbCData {
			out.WriteString(xmlEsc(r.str()))
			continue
		}
		cn := svgbElements[id]
		if cn == "" {
			return fmt.Errorf("unknown SVG-T element %d", id)
		}
		if err := r.element(out, cn, depth); err != nil {
			return err
		}
	}
	out.WriteString("</" + name + ">")
	return nil
}

func xmlEsc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}
