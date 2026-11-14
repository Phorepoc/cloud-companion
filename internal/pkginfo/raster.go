// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package pkginfo

// A small SVG-Tiny rasteriser for icons (our own code, no dependencies): shapes (path, rect, circle, ellipse, line, polygon, polyline), solid and
// gradient fills, strokes (round joins), group and paint opacity, transforms, viewBox. Anti-aliasing by 4 sub-scanlines with exact horizontal
// coverage. Text, images, filters and dashes are not drawn.

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type pt struct{ x, y float64 }
type mat [6]float64 // a b c d e f: x' = a x + c y + e, y' = b x + d y + f

var ident = mat{1, 0, 0, 1, 0, 0}

func (m mat) mul(n mat) mat { // m after n (apply n first)
	return mat{m[0]*n[0] + m[2]*n[1], m[1]*n[0] + m[3]*n[1], m[0]*n[2] + m[2]*n[3], m[1]*n[2] + m[3]*n[3],
		m[0]*n[4] + m[2]*n[5] + m[4], m[1]*n[4] + m[3]*n[5] + m[5]}
}
func (m mat) apply(p pt) pt { return pt{m[0]*p.x + m[2]*p.y + m[4], m[1]*p.x + m[3]*p.y + m[5]} }
func (m mat) inv() (mat, bool) {
	det := m[0]*m[3] - m[1]*m[2]
	if math.Abs(det) < 1e-12 {
		return ident, false
	}
	return mat{m[3] / det, -m[1] / det, -m[2] / det, m[0] / det, (m[2]*m[5] - m[3]*m[4]) / det, (m[1]*m[4] - m[0]*m[5]) / det}, true
}

type node struct {
	name  string
	attrs map[string]string
	kids  []*node
}

func parseXML(svg string) (*node, error) {
	d := xml.NewDecoder(strings.NewReader(svg))
	d.Strict = false
	d.CharsetReader = func(string, io.Reader) (io.Reader, error) { return strings.NewReader(svg), nil }
	var root *node
	var stack []*node
	for {
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch e := t.(type) {
		case xml.StartElement:
			n := &node{name: e.Name.Local, attrs: map[string]string{}}
			for _, a := range e.Attr {
				n.attrs[a.Name.Local] = a.Value
			}
			if st := n.attrs["style"]; st != "" {
				for _, decl := range strings.Split(st, ";") {
					if k, v, ok := strings.Cut(decl, ":"); ok {
						n.attrs[strings.TrimSpace(k)] = strings.TrimSpace(v)
					}
				}
			}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.kids = append(p.kids, n)
			} else if root == nil {
				root = n
			}
			stack = append(stack, n)
			if len(stack) > 64 {
				return nil, errors.New("too deeply nested")
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if root == nil || root.name != "svg" {
		return nil, errors.New("not an SVG document")
	}
	return root, nil
}

var numRe = regexp.MustCompile(`[-+]?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?`)

func nums(s string) []float64 {
	var out []float64
	for _, m := range numRe.FindAllString(s, -1) {
		v, _ := strconv.ParseFloat(m, 64)
		out = append(out, v)
	}
	return out
}

func num1(s string, def float64) float64 {
	if m := numRe.FindString(s); m != "" {
		v, _ := strconv.ParseFloat(m, 64)
		return v
	}
	return def
}

var transformRe = regexp.MustCompile(`(matrix|translate|scale|rotate|skewX|skewY)\s*\(([^)]*)\)`)

func parseTransform(s string) mat {
	m := ident
	for _, f := range transformRe.FindAllStringSubmatch(s, -1) {
		v := nums(f[2])
		var t mat
		switch {
		case f[1] == "matrix" && len(v) >= 6:
			t = mat{v[0], v[1], v[2], v[3], v[4], v[5]}
		case f[1] == "translate" && len(v) >= 1:
			ty := 0.0
			if len(v) > 1 {
				ty = v[1]
			}
			t = mat{1, 0, 0, 1, v[0], ty}
		case f[1] == "scale" && len(v) >= 1:
			sy := v[0]
			if len(v) > 1 {
				sy = v[1]
			}
			t = mat{v[0], 0, 0, sy, 0, 0}
		case f[1] == "rotate" && len(v) >= 1:
			a := v[0] * math.Pi / 180
			t = mat{math.Cos(a), math.Sin(a), -math.Sin(a), math.Cos(a), 0, 0}
			if len(v) >= 3 {
				t = mat{1, 0, 0, 1, v[1], v[2]}.mul(t).mul(mat{1, 0, 0, 1, -v[1], -v[2]})
			}
		case f[1] == "skewX" && len(v) >= 1:
			t = mat{1, 0, math.Tan(v[0] * math.Pi / 180), 1, 0, 0}
		case f[1] == "skewY" && len(v) >= 1:
			t = mat{1, math.Tan(v[0] * math.Pi / 180), 0, 1, 0, 0}
		default:
			continue
		}
		m = m.mul(t)
	}
	return m
}

type col struct{ r, g, b, a float64 }

var colorNames = map[string]uint32{"black": 0x000000, "white": 0xffffff, "red": 0xff0000, "green": 0x008000, "blue": 0x0000ff, "yellow": 0xffff00, "gray": 0x808080,
	"grey": 0x808080, "silver": 0xc0c0c0, "maroon": 0x800000, "purple": 0x800080, "fuchsia": 0xff00ff, "lime": 0x00ff00, "olive": 0x808000, "navy": 0x000080,
	"teal": 0x008080, "aqua": 0x00ffff, "orange": 0xffa500}

func parseColor(v string) (col, bool) {
	v = strings.TrimSpace(v)
	switch {
	case strings.HasPrefix(v, "#"):
		h := v[1:]
		if len(h) == 3 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		if len(h) >= 6 {
			if x, err := strconv.ParseUint(h[:6], 16, 32); err == nil {
				return col{float64(x>>16&255) / 255, float64(x>>8&255) / 255, float64(x&255) / 255, 1}, true
			}
		}
	case strings.HasPrefix(v, "rgb("):
		f := nums(v)
		if len(f) >= 3 {
			c := func(x float64) float64 {
				if strings.Contains(v, "%") {
					return x / 100
				}
				return x / 255
			}
			return col{c(f[0]), c(f[1]), c(f[2]), 1}, true
		}
	default:
		if x, ok := colorNames[strings.ToLower(v)]; ok {
			return col{float64(x>>16&255) / 255, float64(x>>8&255) / 255, float64(x&255) / 255, 1}, true
		}
		if strings.EqualFold(v, "currentColor") {
			return col{0, 0, 0, 1}, true
		}
	}
	return col{}, false
}

type stop struct {
	off float64
	c   col
}

type gradient struct {
	radial                    bool
	x1, y1, x2, y2, cx, cy, r float64
	bbox                      bool
	tr                        mat
	stops                     []stop
}

func (g *gradient) at(t float64) col {
	if len(g.stops) == 0 {
		return col{}
	}
	t = math.Max(0, math.Min(1, t))
	s := g.stops
	if t <= s[0].off {
		return s[0].c
	}
	for i := 1; i < len(s); i++ {
		if t <= s[i].off {
			span := s[i].off - s[i-1].off
			if span <= 0 {
				return s[i].c
			}
			u := (t - s[i-1].off) / span
			a, b := s[i-1].c, s[i].c
			return col{a.r + (b.r-a.r)*u, a.g + (b.g-a.g)*u, a.b + (b.b-a.b)*u, a.a + (b.a-a.a)*u}
		}
	}
	return s[len(s)-1].c
}

type ras struct {
	w, h  int
	buf   []float64 // premultiplied col
	defs  map[string]*node
	grads map[string]*gradient
}

type style struct {
	fill, stroke         string
	fillOp, strokeOp, op float64
	sw, miter            float64
	cap                  string
	rule                 string
}

func (st style) with(n *node) style {
	for k, v := range n.attrs {
		switch k {
		case "fill":
			st.fill = v
		case "stroke":
			st.stroke = v
		case "fill-opacity":
			st.fillOp = num1(v, 1)
		case "stroke-opacity":
			st.strokeOp = num1(v, 1)
		case "stroke-width":
			st.sw = num1(v, 1)
		case "stroke-linecap":
			st.cap = v
		case "fill-rule":
			st.rule = v
		}
	}
	if v, ok := n.attrs["opacity"]; ok {
		st.op *= math.Max(0, math.Min(1, num1(v, 1)))
	}
	return st
}

// SVGToPNG renders SVG text to a PNG whose longer side is size pixels.
func SVGToPNG(svg string, size int) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, errors.New("the vector icon could not be drawn")
		}
	}()
	root, err := parseXML(svg)
	if err != nil {
		return nil, err
	}
	vb := nums(root.attrs["viewBox"])
	w, h := num1(root.attrs["width"], 0), num1(root.attrs["height"], 0)
	if len(vb) < 4 {
		vb = []float64{0, 0, w, h}
	}
	if vb[2] <= 0 || vb[3] <= 0 {
		return nil, errors.New("the vector icon has no size")
	}
	sc := float64(size) / math.Max(vb[2], vb[3])
	pw, ph := int(math.Ceil(vb[2]*sc)), int(math.Ceil(vb[3]*sc))
	if pw < 1 || ph < 1 || pw > 2048 || ph > 2048 {
		return nil, errors.New("bad size")
	}
	r := &ras{w: pw, h: ph, buf: make([]float64, pw*ph*4), defs: map[string]*node{}, grads: map[string]*gradient{}}
	var collect func(n *node)
	collect = func(n *node) {
		if id := n.attrs["id"]; id != "" {
			r.defs[id] = n
		}
		for _, k := range n.kids {
			collect(k)
		}
	}
	collect(root)
	base := mat{sc, 0, 0, sc, -vb[0] * sc, -vb[1] * sc}
	r.draw(root, style{fill: "black", stroke: "none", fillOp: 1, strokeOp: 1, op: 1, sw: 1, miter: 4, cap: "butt"}, base)
	img := image.NewNRGBA(image.Rect(0, 0, pw, ph))
	for i := 0; i < pw*ph; i++ {
		a := r.buf[4*i+3]
		if a <= 0 {
			continue
		}
		c := func(v float64) uint8 { return uint8(math.Round(math.Max(0, math.Min(1, v/a)) * 255)) }
		img.Pix[4*i], img.Pix[4*i+1], img.Pix[4*i+2], img.Pix[4*i+3] = c(r.buf[4*i]), c(r.buf[4*i+1]), c(r.buf[4*i+2]), uint8(math.Round(math.Min(1, a)*255))
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (r *ras) draw(n *node, st style, ctm mat) {
	switch n.name {
	case "defs", "linearGradient", "radialGradient", "title", "desc", "metadata", "text", "switch", "style", "solidColor":
		return
	}
	if n.attrs["display"] == "none" || n.attrs["visibility"] == "hidden" {
		return
	}
	st = st.with(n)
	if t := n.attrs["transform"]; t != "" {
		ctm = ctm.mul(parseTransform(t))
	}
	if n.name == "image" {
		r.drawImage(n, st, ctm)
		return
	}
	switch n.name {
	case "svg", "g", "a":
		for _, k := range n.kids {
			r.draw(k, st, ctm)
		}
		return
	}
	subs, closed := shapeSubpaths(n)
	if len(subs) == 0 {
		return
	}
	// fill
	if st.fill != "none" && n.name != "line" {
		polys := make([][]pt, len(subs))
		for i, s := range subs {
			polys[i] = transformPts(s, ctm)
		}
		cov := r.coverage(polys, st.rule == "evenodd")
		r.paint(cov, st.fill, st.fillOp*st.op, subs, ctm)
	}
	stroke := st.stroke
	if n.name == "line" && stroke == "none" && st.fill != "none" {
		stroke = st.fill
	}
	if stroke != "none" && st.sw > 0 {
		var polys [][]pt
		for i, s := range subs {
			for _, p := range strokePolys(s, closed[i], st.sw, st.cap) {
				polys = append(polys, orient(transformPts(p, ctm)))
			}
		}
		cov := r.coverage(polys, false)
		r.paint(cov, stroke, st.strokeOp*st.op, subs, ctm)
	}
}

func transformPts(p []pt, m mat) []pt {
	out := make([]pt, len(p))
	for i, q := range p {
		out[i] = m.apply(q)
	}
	return out
}

func orient(p []pt) []pt {
	a := 0.0
	for i := range p {
		j := (i + 1) % len(p)
		a += p[i].x*p[j].y - p[j].x*p[i].y
	}
	if a < 0 {
		for i, j := 0, len(p)-1; i < j; i, j = i+1, j-1 {
			p[i], p[j] = p[j], p[i]
		}
	}
	return p
}

func circlePoly(c pt, r float64) []pt {
	out := make([]pt, 16)
	for i := range out {
		a := 2 * math.Pi * float64(i) / 16
		out[i] = pt{c.x + r*math.Cos(a), c.y + r*math.Sin(a)}
	}
	return out
}

// strokePolys: one rectangle per segment, a disc at every join and round cap (all unioned by the non-zero rule).
func strokePolys(s []pt, closed bool, sw float64, capStyle string) [][]pt {
	h := sw / 2
	var out [][]pt
	n := len(s)
	segs := n - 1
	if closed {
		segs = n
	}
	for i := 0; i < segs; i++ {
		p, q := s[i], s[(i+1)%n]
		dx, dy := q.x-p.x, q.y-p.y
		l := math.Hypot(dx, dy)
		if l == 0 {
			continue
		}
		ux, uy := dx/l, dy/l
		if capStyle == "square" && !closed {
			if i == 0 {
				p = pt{p.x - ux*h, p.y - uy*h}
			}
			if i == segs-1 {
				q = pt{q.x + ux*h, q.y + uy*h}
			}
		}
		nx, ny := -uy*h, ux*h
		out = append(out, []pt{{p.x + nx, p.y + ny}, {q.x + nx, q.y + ny}, {q.x - nx, q.y - ny}, {p.x - nx, p.y - ny}})
	}
	for i := range s {
		if (i == 0 || i == n-1) && !closed && capStyle != "round" {
			continue
		}
		out = append(out, circlePoly(s[i], h))
	}
	return out
}

func (r *ras) coverage(polys [][]pt, evenodd bool) []float32 {
	cov := make([]float32, r.w*r.h)
	type cross struct {
		x   float64
		dir int
	}
	const sub = 4
	for sy := 0; sy < r.h*sub; sy++ {
		y := (float64(sy) + 0.5) / sub
		var xs []cross
		for _, p := range polys {
			for i := range p {
				a, b := p[i], p[(i+1)%len(p)]
				if a.y == b.y {
					continue
				}
				dir := 1
				if a.y > b.y {
					a, b, dir = b, a, -1
				}
				if y < a.y || y >= b.y {
					continue
				}
				xs = append(xs, cross{a.x + (y-a.y)*(b.x-a.x)/(b.y-a.y), dir})
			}
		}
		if len(xs) < 2 {
			continue
		}
		sort.Slice(xs, func(i, j int) bool { return xs[i].x < xs[j].x })
		wind := 0
		row := sy / sub * r.w
		for i := 0; i < len(xs)-1; i++ {
			wind += xs[i].dir
			in := wind != 0
			if evenodd {
				in = (i+1)%2 == 1
			}
			if !in {
				continue
			}
			x0, x1 := math.Max(xs[i].x, 0), math.Min(xs[i+1].x, float64(r.w))
			if x1 <= x0 {
				continue
			}
			for px := int(x0); px <= int(x1) && px < r.w; px++ {
				l, rr := math.Max(x0, float64(px)), math.Min(x1, float64(px+1))
				if rr > l {
					cov[row+px] += float32((rr - l) / sub)
				}
			}
		}
	}
	return cov
}

func (r *ras) gradientOf(id string) *gradient {
	if g, ok := r.grads[id]; ok {
		return g
	}
	n := r.defs[id]
	if n == nil || (n.name != "linearGradient" && n.name != "radialGradient") {
		return nil
	}
	g := &gradient{radial: n.name == "radialGradient", tr: ident, x2: 1, cx: .5, cy: .5, r: .5, bbox: true}
	src := n
	for hop := 0; hop < 8 && src != nil; hop++ {
		for _, k := range src.kids {
			if k.name != "stop" {
				continue
			}
			c, ok := parseColor(k.attrs["stop-color"])
			if !ok {
				c = col{0, 0, 0, 1}
			}
			if v, ok := k.attrs["stop-opacity"]; ok {
				c.a = num1(v, 1)
			}
			off := num1(k.attrs["offset"], 0)
			if strings.HasSuffix(strings.TrimSpace(k.attrs["offset"]), "%") {
				off /= 100
			}
			g.stops = append(g.stops, stop{off, c})
		}
		if len(g.stops) > 0 {
			break
		}
		src = r.defs[strings.TrimPrefix(src.attrs["href"], "#")]
	}
	get := func(k string, def float64) float64 {
		if v, ok := n.attrs[k]; ok {
			return num1(v, def)
		}
		return def
	}
	g.x1, g.y1, g.x2, g.y2 = get("x1", 0), get("y1", 0), get("x2", 1), get("y2", 0)
	g.cx, g.cy, g.r = get("cx", .5), get("cy", .5), get("r", .5)
	g.bbox = n.attrs["gradientUnits"] != "userSpaceOnUse"
	if t := n.attrs["gradientTransform"]; t != "" {
		g.tr = parseTransform(t)
	}
	r.grads[id] = g
	return g
}

var urlRe = regexp.MustCompile(`url\(\s*#([^)\s]+)\s*\)`)

// paint composites the coverage mask with a paint value.
func (r *ras) paint(cov []float32, value string, opacity float64, subs [][]pt, ctm mat) {
	var solid col
	var g *gradient
	var inv mat
	if m := urlRe.FindStringSubmatch(value); m != nil {
		g = r.gradientOf(m[1])
		if g == nil || len(g.stops) == 0 {
			return
		}
		full := ctm
		if g.bbox {
			x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
			for _, s := range subs {
				for _, p := range s {
					x0, y0, x1, y1 = math.Min(x0, p.x), math.Min(y0, p.y), math.Max(x1, p.x), math.Max(y1, p.y)
				}
			}
			full = full.mul(mat{x1 - x0, 0, 0, y1 - y0, x0, y0})
		}
		full = full.mul(g.tr)
		var ok bool
		if inv, ok = full.inv(); !ok {
			return
		}
	} else {
		c, ok := parseColor(value)
		if !ok {
			return
		}
		solid = c
	}
	for y := 0; y < r.h; y++ {
		for x := 0; x < r.w; x++ {
			cv := float64(cov[y*r.w+x])
			if cv <= 0 {
				continue
			}
			c := solid
			if g != nil {
				p := inv.apply(pt{float64(x) + .5, float64(y) + .5})
				var t float64
				if g.radial {
					t = math.Hypot(p.x-g.cx, p.y-g.cy) / g.r
				} else {
					dx, dy := g.x2-g.x1, g.y2-g.y1
					if d := dx*dx + dy*dy; d > 0 {
						t = ((p.x-g.x1)*dx + (p.y-g.y1)*dy) / d
					}
				}
				c = g.at(t)
			}
			a := math.Min(1, cv) * c.a * opacity
			i := 4 * (y*r.w + x)
			r.buf[i] = c.r*a + r.buf[i]*(1-a)
			r.buf[i+1] = c.g*a + r.buf[i+1]*(1-a)
			r.buf[i+2] = c.b*a + r.buf[i+2]*(1-a)
			r.buf[i+3] = a + r.buf[i+3]*(1-a)
		}
	}
}

// ---- shapes

func shapeSubpaths(n *node) (subs [][]pt, closed []bool) {
	f := func(k string) float64 { return num1(n.attrs[k], 0) }
	add := func(p []pt, c bool) {
		if len(p) > 0 {
			subs, closed = append(subs, p), append(closed, c)
		}
	}
	switch n.name {
	case "rect":
		x, y, w, h := f("x"), f("y"), f("width"), f("height")
		if w > 0 && h > 0 {
			add([]pt{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}, true)
		}
	case "circle", "ellipse":
		rx, ry := f("rx"), f("ry")
		if n.name == "circle" {
			rx, ry = f("r"), f("r")
		}
		if rx > 0 && ry > 0 {
			var p []pt
			for i := 0; i < 48; i++ {
				a := 2 * math.Pi * float64(i) / 48
				p = append(p, pt{f("cx") + rx*math.Cos(a), f("cy") + ry*math.Sin(a)})
			}
			add(p, true)
		}
	case "line":
		add([]pt{{f("x1"), f("y1")}, {f("x2"), f("y2")}}, false)
	case "polygon", "polyline":
		v := nums(n.attrs["points"])
		var p []pt
		for i := 0; i+1 < len(v); i += 2 {
			p = append(p, pt{v[i], v[i+1]})
		}
		add(p, n.name == "polygon")
	case "path":
		return pathSubpaths(n.attrs["d"])
	}
	return
}

var pathTok = regexp.MustCompile(`[MmLlHhVvCcSsQqTtAaZz]|[-+]?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?`)

func pathSubpaths(d string) (subs [][]pt, closed []bool) {
	toks := pathTok.FindAllString(d, -1)
	var cur []pt
	var pos, start, lastCtl pt
	var prev byte
	flush := func(c bool) {
		if len(cur) > 1 || (c && len(cur) > 0) {
			subs, closed = append(subs, cur), append(closed, c)
		}
		cur = nil
	}
	i := 0
	num := func() float64 {
		if i >= len(toks) {
			panic("path")
		}
		v, _ := strconv.ParseFloat(toks[i], 64)
		i++
		return v
	}
	var cmd byte
	for i < len(toks) {
		if t := toks[i]; len(t) == 1 && strings.ContainsAny(t, "MmLlHhVvCcSsQqTtAaZz") {
			cmd = t[0]
			i++
			if cmd == 'Z' || cmd == 'z' {
				flush(true)
				pos = start
				prev = 'z'
				continue
			}
		} else if cmd == 0 || cmd == 'Z' || cmd == 'z' {
			break
		} else if cmd == 'M' {
			cmd = 'L'
		} else if cmd == 'm' {
			cmd = 'l'
		}
		rel := cmd >= 'a'
		up := cmd &^ 0x20
		abs := func(x, y float64) pt {
			if rel {
				return pt{pos.x + x, pos.y + y}
			}
			return pt{x, y}
		}
		if len(cur) == 0 && up != 'M' {
			cur = []pt{pos}
		}
		switch up {
		case 'M':
			flush(false)
			pos = abs(num(), num())
			start = pos
			cur = []pt{pos}
		case 'L':
			pos = abs(num(), num())
			cur = append(cur, pos)
		case 'H':
			x := num()
			if rel {
				x += pos.x
			}
			pos = pt{x, pos.y}
			cur = append(cur, pos)
		case 'V':
			y := num()
			if rel {
				y += pos.y
			}
			pos = pt{pos.x, y}
			cur = append(cur, pos)
		case 'C', 'S':
			var c1 pt
			if up == 'C' {
				c1 = abs(num(), num())
			} else {
				c1 = pos
				if prev == 'c' {
					c1 = pt{2*pos.x - lastCtl.x, 2*pos.y - lastCtl.y}
				}
			}
			c2 := abs(num(), num())
			e := abs(num(), num())
			cur = append(cur, cubic(pos, c1, c2, e)...)
			pos, lastCtl = e, c2
			prev = 'c'
			continue
		case 'Q', 'T':
			var c1 pt
			if up == 'Q' {
				c1 = abs(num(), num())
			} else {
				c1 = pos
				if prev == 'q' {
					c1 = pt{2*pos.x - lastCtl.x, 2*pos.y - lastCtl.y}
				}
			}
			e := abs(num(), num())
			cur = append(cur, cubic(pos, pt{pos.x + 2.0/3*(c1.x-pos.x), pos.y + 2.0/3*(c1.y-pos.y)}, pt{e.x + 2.0/3*(c1.x-e.x), e.y + 2.0/3*(c1.y-e.y)}, e)...)
			pos, lastCtl = e, c1
			prev = 'q'
			continue
		case 'A':
			rx, ry, rot, large, sweep := num(), num(), num(), num(), num()
			e := abs(num(), num())
			cur = append(cur, arc(pos, rx, ry, rot, large != 0, sweep != 0, e)...)
			pos = e
		}
		prev = byte(up | 0x20)
	}
	flush(false)
	return
}

func cubic(p0, p1, p2, p3 pt) []pt {
	const n = 16
	out := make([]pt, 0, n)
	for i := 1; i <= n; i++ {
		t := float64(i) / n
		u := 1 - t
		out = append(out, pt{u*u*u*p0.x + 3*u*u*t*p1.x + 3*u*t*t*p2.x + t*t*t*p3.x, u*u*u*p0.y + 3*u*u*t*p1.y + 3*u*t*t*p2.y + t*t*t*p3.y})
	}
	return out
}

// arc: SVG endpoint arc to a polyline (F.6.5 of the SVG specification).
func arc(p0 pt, rx, ry, rotDeg float64, large, sweep bool, p1 pt) []pt {
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 || p0 == p1 {
		return []pt{p1}
	}
	phi := rotDeg * math.Pi / 180
	cp, sp := math.Cos(phi), math.Sin(phi)
	dx, dy := (p0.x-p1.x)/2, (p0.y-p1.y)/2
	x1, y1 := cp*dx+sp*dy, -sp*dx+cp*dy
	if l := x1*x1/(rx*rx) + y1*y1/(ry*ry); l > 1 {
		s := math.Sqrt(l)
		rx, ry = rx*s, ry*s
	}
	num := rx*rx*ry*ry - rx*rx*y1*y1 - ry*ry*x1*x1
	den := rx*rx*y1*y1 + ry*ry*x1*x1
	k := 0.0
	if den != 0 && num > 0 {
		k = math.Sqrt(num / den)
	}
	if large == sweep {
		k = -k
	}
	cx1, cy1 := k*rx*y1/ry, -k*ry*x1/rx
	cx, cy := cp*cx1-sp*cy1+(p0.x+p1.x)/2, sp*cx1+cp*cy1+(p0.y+p1.y)/2
	ang := func(ux, uy, vx, vy float64) float64 {
		a := math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
		return a
	}
	th1 := ang(1, 0, (x1-cx1)/rx, (y1-cy1)/ry)
	dth := ang((x1-cx1)/rx, (y1-cy1)/ry, (-x1-cx1)/rx, (-y1-cy1)/ry)
	if !sweep && dth > 0 {
		dth -= 2 * math.Pi
	} else if sweep && dth < 0 {
		dth += 2 * math.Pi
	}
	n := int(math.Ceil(math.Abs(dth)/(math.Pi/16))) + 1
	out := make([]pt, 0, n)
	for i := 1; i <= n; i++ {
		t := th1 + dth*float64(i)/float64(n)
		x, y := rx*math.Cos(t), ry*math.Sin(t)
		out = append(out, pt{cp*x - sp*y + cx, sp*x + cp*y + cy})
	}
	out[len(out)-1] = p1
	return out
}

var _ = color.NRGBA{}

// drawImage draws an embedded picture (a data: URI holding a PNG or JPEG), as SVG-T icons carry their artwork.
func (r *ras) drawImage(n *node, st style, ctm mat) {
	href := n.attrs["href"]
	i := strings.Index(href, "base64,")
	if !strings.HasPrefix(href, "data:") || i < 0 {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(href[i+7:]), ""))
	if err != nil {
		return
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return
	}
	sb := src.Bounds()
	x, y := num1(n.attrs["x"], 0), num1(n.attrs["y"], 0)
	w, h := num1(n.attrs["width"], float64(sb.Dx())), num1(n.attrs["height"], float64(sb.Dy()))
	if w <= 0 || h <= 0 {
		return
	}
	// image pixel -> device
	full := ctm.mul(mat{w / float64(sb.Dx()), 0, 0, h / float64(sb.Dy()), x, y})
	inv, ok := full.inv()
	if !ok {
		return
	}
	corners := transformPts([]pt{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}, ctm)
	cov := r.coverage([][]pt{orient(corners)}, false)
	at := func(ix, iy int) (rr, gg, bb, aa float64) {
		ix = max(0, min(sb.Dx()-1, ix))
		iy = max(0, min(sb.Dy()-1, iy))
		c := color.NRGBAModel.Convert(src.At(sb.Min.X+ix, sb.Min.Y+iy)).(color.NRGBA)
		return float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255, float64(c.A) / 255
	}
	for py := 0; py < r.h; py++ {
		for px := 0; px < r.w; px++ {
			cv := float64(cov[py*r.w+px])
			if cv <= 0 {
				continue
			}
			p := inv.apply(pt{float64(px) + .5, float64(py) + .5})
			fx, fy := p.x-.5, p.y-.5
			x0, y0 := int(math.Floor(fx)), int(math.Floor(fy))
			tx, ty := fx-float64(x0), fy-float64(y0)
			var cr, cg, cb, ca float64
			for k := 0; k < 4; k++ {
				wx, wy := tx, ty
				dx, dy := k&1, k>>1
				if dx == 0 {
					wx = 1 - tx
				}
				if dy == 0 {
					wy = 1 - ty
				}
				a, b2, c2, d := at(x0+dx, y0+dy)
				wgt := wx * wy
				cr, cg, cb, ca = cr+a*d*wgt, cg+b2*d*wgt, cb+c2*d*wgt, ca+d*wgt // premultiplied
			}
			if ca <= 0 {
				continue
			}
			a := math.Min(1, cv) * ca * st.op
			i := 4 * (py*r.w + px)
			k := a / ca
			r.buf[i] = cr*k + r.buf[i]*(1-a)
			r.buf[i+1] = cg*k + r.buf[i+1]*(1-a)
			r.buf[i+2] = cb*k + r.buf[i+2]*(1-a)
			r.buf[i+3] = a + r.buf[i+3]*(1-a)
		}
	}
}
