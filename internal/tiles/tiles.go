// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package tiles cuts an image into fixed-size tiles, hashes them and encodes each as PNG (text-like, lossless,
// paletted when it has <= 256 colours) or JPEG (photo-like).
package tiles

import (
	"bytes"
	"hash/fnv"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
)

const (
	TileW        = 360
	TileH        = 256
	JPEGQuality  = 70
	photoColours = 1200 // more distinct colours than this = photo-like
)

type Tile struct {
	X, Y, W, H int
	Hash       uint64
	Fmt        string
	Data       []byte
}

// Cut returns the tile grid of img (RGBA), row-major. Encoding is deferred: Hash is set, Data empty.
func Cut(img *image.RGBA) []Tile {
	b := img.Bounds()
	var out []Tile
	for y := 0; y < b.Dy(); y += TileH {
		for x := 0; x < b.Dx(); x += TileW {
			w, h := min(TileW, b.Dx()-x), min(TileH, b.Dy()-y)
			t := Tile{X: x, Y: y, W: w, H: h}
			t.Hash = hashRect(img, b.Min.X+x, b.Min.Y+y, w, h)
			out = append(out, t)
		}
	}
	return out
}

func hashRect(img *image.RGBA, x, y, w, h int) uint64 {
	f := fnv.New64a()
	f.Write([]byte{byte(w), byte(w >> 8), byte(h), byte(h >> 8)})
	for r := 0; r < h; r++ {
		o := img.PixOffset(x, y+r)
		f.Write(img.Pix[o : o+w*4])
	}
	return f.Sum64()
}

// Encode fills t.Fmt and t.Data from the image.
func Encode(img *image.RGBA, t *Tile) error {
	b := img.Bounds()
	sub := img.SubImage(image.Rect(b.Min.X+t.X, b.Min.Y+t.Y, b.Min.X+t.X+t.W, b.Min.Y+t.Y+t.H)).(*image.RGBA)
	pal, photo := palette(sub)
	var buf bytes.Buffer
	switch {
	case pal != nil:
		p := image.NewPaletted(image.Rect(0, 0, t.W, t.H), pal.colours)
		for y := 0; y < t.H; y++ {
			o := sub.PixOffset(sub.Rect.Min.X, sub.Rect.Min.Y+y)
			for x := 0; x < t.W; x++ {
				k := [4]byte{sub.Pix[o+x*4], sub.Pix[o+x*4+1], sub.Pix[o+x*4+2], sub.Pix[o+x*4+3]}
				p.Pix[y*p.Stride+x] = pal.index[k]
			}
		}
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(&buf, p); err != nil {
			return err
		}
		t.Fmt = "png"
	case photo:
		if err := jpeg.Encode(&buf, sub, &jpeg.Options{Quality: JPEGQuality}); err != nil {
			return err
		}
		t.Fmt = "jpeg"
	default:
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(&buf, sub); err != nil {
			return err
		}
		t.Fmt = "png"
	}
	t.Data = buf.Bytes()
	return nil
}

type pal struct {
	colours color.Palette
	index   map[[4]byte]uint8
}

// palette returns a palette when img has <= 256 colours, else photo=true when it has > photoColours.
func palette(img *image.RGBA) (*pal, bool) {
	seen := map[[4]byte]struct{}{}
	r := img.Rect
	for y := r.Min.Y; y < r.Max.Y; y++ {
		o := img.PixOffset(r.Min.X, y)
		for x := 0; x < r.Dx(); x++ {
			k := [4]byte{img.Pix[o+x*4], img.Pix[o+x*4+1], img.Pix[o+x*4+2], img.Pix[o+x*4+3]}
			seen[k] = struct{}{}
		}
		if len(seen) > photoColours {
			return nil, true
		}
	}
	if len(seen) > 256 {
		return nil, false
	}
	p := &pal{index: map[[4]byte]uint8{}}
	for k := range seen {
		p.index[k] = uint8(len(p.colours))
		p.colours = append(p.colours, color.RGBA{k[0], k[1], k[2], k[3]})
	}
	return p, false
}
