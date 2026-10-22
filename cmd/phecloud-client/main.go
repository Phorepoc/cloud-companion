// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Command phecloud-client: a test client. Pairs (once), opens URLs, saves the tiles, prints byte totals and timing.
//
//	phecloud-client -addr host:7400 -code ABCD-EFGH -url https://example.com -url https://en.m.wikipedia.org/wiki/Nokia
package main

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/phorepoc/phecloud/internal/proto"
)

type urls []string

func (u *urls) String() string     { return strings.Join(*u, ",") }
func (u *urls) Set(s string) error { *u = append(*u, s); return nil }

type state struct {
	Dev string `json:"dev"`
	Key []byte `json:"key"`
}

type counter struct {
	n  int64
	rd *bufio.Reader
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.rd.Read(p)
	c.n += int64(n)
	return n, err
}

func main() {
	addr := flag.String("addr", "127.0.0.1:7400", "server")
	code := flag.String("code", "", "pairing code (first run)")
	name := flag.String("name", "test-client", "device name")
	stf := flag.String("state", "client-state.json", "saved device key")
	out := flag.String("out", "tiles", "output dir")
	tap := flag.Bool("tap-first-link", false, "after the first page, tap the first link and report the update")
	typ := flag.String("type", "", "after the first page, type this text into the first input")
	var us urls
	flag.Var(&us, "url", "URL to open (repeatable)")
	flag.Parse()

	nc, err := net.Dial("tcp", *addr)
	if err != nil {
		fatal(err)
	}
	defer nc.Close()
	cr := &counter{rd: bufio.NewReader(nc)}

	t, raw, err := proto.Read(cr)
	if err != nil || t != proto.TChallenge {
		fatal(fmt.Errorf("expected challenge: %v %v", t, err))
	}
	var ch proto.Challenge
	proto.Decode(raw, &ch)

	var st state
	if b, err := os.ReadFile(*stf); err == nil {
		json.Unmarshal(b, &st)
	}
	if st.Dev == "" {
		if *code == "" {
			fatal(fmt.Errorf("not paired: pass -code"))
		}
		proto.Write(nc, proto.Pair{T: proto.TPair, Code: *code, Name: *name})
	} else {
		m := hmac.New(sha256.New, st.Key)
		m.Write(ch.Nonce)
		m.Write([]byte("phecloud-v1"))
		proto.Write(nc, proto.Hello{T: proto.THello, Dev: st.Dev, Mac: m.Sum(nil)})
	}
	t, raw, err = proto.Read(cr)
	if err != nil {
		fatal(err)
	}
	if t == proto.TError {
		var e proto.Error
		proto.Decode(raw, &e)
		fatal(fmt.Errorf("server: %s", e.Msg))
	}
	var w proto.Welcome
	proto.Decode(raw, &w)
	if len(w.Key) > 0 {
		st = state{w.Dev, w.Key}
		b, _ := json.Marshal(st)
		os.WriteFile(*stf, b, 0o600)
		fmt.Println("paired as", w.Dev)
	} else {
		fmt.Println("authenticated as", w.Dev)
	}

	var totalWire int64
	for i, u := range us {
		before := cr.n
		start := time.Now()
		proto.Write(nc, proto.Open{T: proto.TOpen, URL: u, Width: 360, DPR: 1})
		sum := collect(cr, filepath.Join(*out, fmt.Sprintf("p%d", i)), start)
		sum.print(u, cr.n-before)
		totalWire += cr.n - before
		if *tap && i == 0 && len(sum.regions) > 0 {
			for _, r := range sum.regions {
				if r.Kind == "link" && r.Layer == 0 {
					fmt.Printf("tapping link #%d %q -> %s at (%d,%d)\n", r.ID, r.Label, r.Href, r.X+r.W/2, r.Y+r.H/2)
					before = cr.n
					start = time.Now()
					proto.Write(nc, proto.Input{T: proto.TInput, Kind: "tap", X: r.X + r.W/2, Y: r.Y + r.H/2})
					s2 := collect(cr, filepath.Join(*out, "tap"), start)
					s2.print("(after tap)", cr.n-before)
					break
				}
			}
		}
		if *typ != "" && i == 0 {
			for _, r := range sum.regions {
				if r.Kind == "input" || r.Kind == "textarea" {
					fmt.Printf("typing %q into region #%d (%s)\n", *typ, r.ID, r.Name)
					before = cr.n
					start = time.Now()
					proto.Write(nc, proto.Input{T: proto.TInput, Kind: "text", ID: r.ID, Text: *typ})
					s2 := collect(cr, filepath.Join(*out, "typed"), start)
					s2.print("(after text input)", cr.n-before)
					break
				}
			}
		}
	}
	fmt.Printf("TOTAL wire bytes: %d\n", totalWire)
}

type summary struct {
	page             proto.Page
	tiles, png, jpg  int
	pngB, jpgB       int
	layers, regions2 int
	regions          []proto.Region
	srvMs            int
	rtt              time.Duration
	firstTile        time.Duration
}

func collect(r *counter, dir string, start time.Time) *summary {
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0o755)
	s := &summary{}
	for {
		t, raw, err := proto.Read(r)
		if err != nil {
			fatal(err)
		}
		switch t {
		case proto.TPage:
			proto.Decode(raw, &s.page)
		case proto.TLayers:
			var l proto.Layers
			proto.Decode(raw, &l)
			s.layers = len(l.List)
			for _, x := range l.List {
				fmt.Printf("  layer %d %s %dx%d at (%d,%d) z=%d\n", x.ID, x.Kind, x.W, x.H, x.X, x.Y, x.Z)
			}
		case proto.TRegions:
			var rg proto.Regions
			proto.Decode(raw, &rg)
			s.regions = rg.List
		case proto.TTile:
			var tl proto.Tile
			proto.Decode(raw, &tl)
			if s.tiles == 0 {
				s.firstTile = time.Since(start)
			}
			s.tiles++
			if tl.Fmt == "jpeg" {
				s.jpg++
				s.jpgB += len(tl.Data)
			} else {
				s.png++
				s.pngB += len(tl.Data)
			}
			ext := map[string]string{"jpeg": "jpg", "png": "png"}[tl.Fmt]
			os.WriteFile(filepath.Join(dir, fmt.Sprintf("L%d_y%05d_x%03d.%s", tl.Layer, tl.Y, tl.X, ext)), tl.Data, 0o644)
		case proto.TDone:
			var d proto.Done
			proto.Decode(raw, &d)
			s.srvMs = d.Ms
			s.rtt = time.Since(start)
			return s
		case proto.TError:
			var e proto.Error
			proto.Decode(raw, &e)
			fatal(fmt.Errorf("server error: %s", e.Msg))
		}
	}
}

func (s *summary) print(label string, wire int64) {
	kinds := map[string]int{}
	for _, r := range s.regions {
		kinds[r.Kind]++
	}
	fmt.Printf("== %s\n   title %q  page %dx%d  url %s\n", label, s.page.Title, s.page.W, s.page.H, s.page.URL)
	fmt.Printf("   tiles %d (png %d = %d B, jpeg %d = %d B)  tile bytes %d  wire bytes %d\n",
		s.tiles, s.png, s.pngB, s.jpg, s.jpgB, s.pngB+s.jpgB, wire)
	fmt.Printf("   layers %d  regions %d %v\n", s.layers, len(s.regions), kinds)
	fmt.Printf("   server render %d ms  first tile %d ms  complete %d ms\n", s.srvMs, s.firstTile.Milliseconds(), s.rtt.Milliseconds())
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func cut(s string) string {
	if len(s) > 100 {
		return s[:100] + "..."
	}
	return s
}
