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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/phorepoc/phecloud/internal/proto"
	"github.com/phorepoc/phecloud/internal/tlsutil"
)

var summaryFlag = flag.Bool("summary", false, "print one machine-readable SUMMARY line per step (compared with the C client)")

type urls []string

func (u *urls) String() string     { return strings.Join(*u, ",") }
func (u *urls) Set(s string) error { *u = append(*u, s); return nil }

type state struct {
	Dev string `json:"dev"`
	Key []byte `json:"key"`
	FP  string `json:"fp"` // pinned server certificate fingerprint (sha256 hex)
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
	fp := flag.String("fp", "", "expected server certificate fingerprint at pairing (else trust on first use)")
	scenario := flag.String("scenario", "", "gaps: focus, select, dialog and download steps on the first page")
	typ := flag.String("type", "", "after the first page, type this text into the first input")
	var us urls
	flag.Var(&us, "url", "URL to open (repeatable)")
	flag.Parse()

	var st state
	if b, err := os.ReadFile(*stf); err == nil {
		json.Unmarshal(b, &st)
	}
	var seenFP string
	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, // we pin the certificate fingerprint instead
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			seenFP = tlsutil.Fingerprint(raw[0])
			switch {
			case st.Dev != "" && st.FP != "" && seenFP != st.FP:
				return fmt.Errorf("server certificate changed: %s (pinned %s)", seenFP, st.FP)
			case st.Dev == "" && *fp != "" && !strings.EqualFold(seenFP, *fp):
				return fmt.Errorf("server certificate %s does not match -fp", seenFP)
			}
			return nil
		},
	}
	nc, err := tls.Dial("tcp", *addr, cfg)
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
		st = state{w.Dev, w.Key, seenFP}
		b, _ := json.Marshal(st)
		os.WriteFile(*stf, b, 0o600)
		fmt.Println("paired as", w.Dev, "pinned certificate", seenFP)
	} else {
		fmt.Println("authenticated as", w.Dev)
	}

	wconn = nc
	var totalWire int64
	step := func(name, label string, send func()) *summary {
		before := cr.n
		start := time.Now()
		send()
		s := collect(cr, filepath.Join(*out, name), start)
		s.print(name, label, cr.n-before)
		totalWire += cr.n - before
		return s
	}
	tapRegion := func(name string, r proto.Region) *summary {
		fmt.Printf("tapping #%d %q at (%d,%d)\n", r.ID, r.Label, r.X+r.W/2, r.Y+r.H/2)
		return step(name, "(after tap)", func() {
			proto.Write(nc, proto.Input{T: proto.TInput, Kind: "tap", X: r.X + r.W/2, Y: r.Y + r.H/2})
		})
	}
	find := func(rs []proto.Region, pred func(proto.Region) bool) *proto.Region {
		for i := range rs {
			if pred(rs[i]) {
				return &rs[i]
			}
		}
		return nil
	}
	for i, u := range us {
		sum := step("open", u, func() { proto.Write(nc, proto.Open{T: proto.TOpen, URL: u, Width: 360, DPR: 1}) })
		if *tap && i == 0 {
			if r := find(sum.regions, func(r proto.Region) bool { return r.Kind == "link" && r.Layer == 0 }); r != nil {
				tapRegion("tap", *r)
				step("back", "(back)", func() { proto.Write(nc, proto.Navigate{T: proto.TNavigate, Action: "back"}) })
			}
		}
		if *typ != "" && i == 0 {
			if r := find(sum.regions, func(r proto.Region) bool { return r.Kind == "input" || r.Kind == "textarea" }); r != nil {
				fmt.Printf("typing %q into region #%d (%s)\n", *typ, r.ID, r.Name)
				step("typed", "(after text input)", func() {
					proto.Write(nc, proto.Input{T: proto.TInput, Kind: "text", ID: r.ID, Text: *typ})
				})
			}
		}
		if *scenario == "gaps" && i == 0 {
			if r := find(sum.regions, func(r proto.Region) bool { return r.Kind == "input" }); r != nil {
				tapRegion("focus", *r)
				step("text", "(text into the focused field)", func() {
					proto.Write(nc, proto.Input{T: proto.TInput, Kind: "text", Text: "abc"})
				})
				step("key", "(Backspace)", func() {
					proto.Write(nc, proto.Input{T: proto.TInput, Kind: "key", Text: "Backspace"})
				})
			}
			if r := find(sum.regions, func(r proto.Region) bool { return r.Kind == "select" }); r != nil {
				step("select", "(select option 2)", func() {
					proto.Write(nc, proto.Input{T: proto.TInput, Kind: "select", ID: r.ID, Index: 2})
				})
			}
			if r := find(sum.regions, func(r proto.Region) bool { return r.Label == "ask" }); r != nil {
				tapRegion("dialog", *r)
			}
			if r := find(sum.regions, func(r proto.Region) bool { return r.Label == "dl" }); r != nil {
				tapRegion("download", *r)
			}
		}
	}
	fmt.Printf("TOTAL wire bytes: %d\n", totalWire)
}

var wconn io.Writer

type summary struct {
	page             proto.Page
	tiles, png, jpg  int
	hx               uint64
	pngB, jpgB       int
	layers, regions2 int
	regions          []proto.Region
	srvMs            int
	rtt              time.Duration
	firstTile        time.Duration
	focus            proto.Focus
	dialogs          int
	dlgKind, dlgMsg  string
	downloads        int
	dlName           string
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
		case proto.TFocus:
			proto.Decode(raw, &s.focus)
		case proto.TDialog:
			var d proto.Dialog
			proto.Decode(raw, &d)
			s.dialogs++
			if s.dialogs == 1 {
				s.dlgKind, s.dlgMsg = d.Kind, d.Message
			}
			fmt.Printf("  dialog #%d %s %q default %q -> replying ok\n", d.ID, d.Kind, d.Message, d.Default)
			proto.Write(wconn, proto.DialogReply{T: proto.TDialogReply, ID: d.ID, OK: true, Text: "Zed"})
		case proto.TDownload:
			var d proto.Download
			proto.Decode(raw, &d)
			s.downloads++
			s.dlName = d.Name
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
			s.hx ^= tl.Hash
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

func (s *summary) print(step, label string, wire int64) {
	if *summaryFlag {
		u := s.page.URL
		if len(u) > 100 {
			u = u[:100]
		}
		nopts, sel := 0, -1
		for _, r := range s.regions {
			if r.Kind == "select" {
				nopts = len(r.Opts)
				for i, o := range r.Opts {
					if o.Sel {
						sel = i
					}
				}
				break
			}
		}
		sp := func(v string) string {
			if v == "" {
				return "-"
			}
			return strings.NewReplacer(" ", "_", "\n", "_").Replace(v)
		}
		fmt.Printf("SUMMARY step=%s url=%s h=%d tiles=%d tilebytes=%d hx=%016x regions=%d layers=%d trunc=%v "+
			"title=%s back=%v fwd=%v focus=%s ftype=%s fvalue=%s opts=%d sel=%d dialogs=%d dlgkind=%s dlgmsg=%s dl=%d dlname=%s\n",
			step, u, s.page.H, s.tiles, s.pngB+s.jpgB, s.hx, len(s.regions), s.layers, s.page.Trunc,
			sp(s.page.Title), s.page.Back, s.page.Fwd, s.focus.Kind, sp(s.focus.Type), sp(s.focus.Value), nopts, sel,
			s.dialogs, sp(s.dlgKind), sp(s.dlgMsg), s.downloads, sp(s.dlName))
	}
	kinds := map[string]int{}
	for _, r := range s.regions {
		kinds[r.Kind]++
	}
	fmt.Printf("== %s\n   title %q  page %dx%d  url %s\n", cut(label), s.page.Title, s.page.W, s.page.H, cut(s.page.URL))
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
