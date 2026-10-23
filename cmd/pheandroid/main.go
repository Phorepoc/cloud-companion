// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// pheandroid streams an Android instance (Redroid, or any adb device) to a browser page, with touch and keyboard going back.
//
//	browser <-- WebSocket (H.264 access units + small JSON) --> pheandroid <-- adb forward --> scrcpy-server (on the device)
//
// The scrcpy server (Apache-2.0, https://github.com/Genymobile/scrcpy) is NOT part of this program or this repository: the operator provides the jar
// (PHE_ANDROID_JAR) and its exact version (PHE_ANDROID_SCRCPY_VERSION); the adb command-line tool (Apache-2.0) must be on the PATH. The browser decodes with
// WebCodecs (H.264 hardware or software decoder of the browser). Access: /ws needs a short-lived ticket minted from the shared secret (PHE_ANDROID_SECRET),
// so only a signed-in Phecloud user reaches the device; the page itself carries no secret.
package main

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

//go:embed index.html
var page []byte

var (
	serial  = env("PHE_ANDROID_SERIAL", "phe-redroid:5555")
	jar     = env("PHE_ANDROID_JAR", "/scrcpy-server.jar")
	version = env("PHE_ANDROID_SCRCPY_VERSION", "5.0.1")
	secret  = os.Getenv("PHE_ANDROID_SECRET")
	prefix  = strings.TrimRight(env("PHE_ANDROID_PREFIX", ""), "/") // e.g. /android when served behind a reverse proxy that keeps the path
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// Ticket is "<expiry unix>.<hex hmac>"; phecloud mints it for a signed-in user (internal/webui/android.go), the bridge only verifies.
func ticketOK(t string) bool {
	if secret == "" {
		return false
	}
	i := strings.IndexByte(t, '.')
	if i < 0 {
		return false
	}
	exp, err := strconv.ParseInt(t[:i], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte("android." + t[:i]))
	return hmac.Equal([]byte(hex.EncodeToString(m.Sum(nil))), []byte(t[i+1:]))
}

func adb(args ...string) *exec.Cmd {
	return exec.Command("adb", append([]string{"-s", serial}, args...)...)
}

var active struct {
	sync.Mutex
	cancel context.CancelFunc
}

func main() {
	if secret == "" {
		log.Println("PHE_ANDROID_SECRET is empty: every /ws connection will be refused")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handleWS)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(page)
	})
	mux.HandleFunc("/apk", handleAPK)
	mux.HandleFunc("/fdroid", handleFDroid)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	addr := env("PHE_ANDROID_ADDR", ":8090")
	go prepare()
	log.Println("pheandroid on", addr, "device", serial)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// prepare connects adb to the device and pushes the server jar (retries: the device may still be booting).
func prepare() {
	for {
		exec.Command("adb", "connect", serial).Run()
		out, _ := adb("shell", "getprop", "sys.boot_completed").Output()
		if strings.TrimSpace(string(out)) == "1" {
			if o, err := adb("push", jar, "/data/local/tmp/scrcpy-server.jar").CombinedOutput(); err != nil {
				log.Println("push:", err, string(o))
			} else {
				log.Println("device ready, server jar pushed")
				return
			}
		}
		time.Sleep(3 * time.Second)
	}
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	if !ticketOK(r.URL.Query().Get("t")) {
		http.Error(w, "forbidden", 403)
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: false, OriginPatterns: []string{"*"}})
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 17)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	active.Lock() // one viewer at a time: a new one replaces the old
	if active.cancel != nil {
		active.cancel()
	}
	active.cancel = cancel
	active.Unlock()
	q := r.URL.Query()
	if err := session(ctx, c, q.Get("size"), q.Get("fps"), q.Get("kbps"), q.Get("codec"), q.Get("audio") != "0"); err != nil && ctx.Err() == nil {
		log.Println("session:", err)
		c.Close(websocket.StatusInternalError, trunc(err.Error(), 100))
	}
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func num(s string, d, lo, hi int) int {
	v, err := strconv.Atoi(s)
	if err != nil || v < lo || v > hi {
		return d
	}
	return v
}

func session(ctx context.Context, ws *websocket.Conn, size, fps, kbps, codec string, wantAudio bool) error {
	if codec != "vp8" && codec != "vp9" {
		codec = "h264"
	}
	var b [4]byte
	rand.Read(b[:])
	scid := fmt.Sprintf("%08x", binary.BigEndian.Uint32(b[:])&0x7fffffff)
	out, err := adb("forward", "tcp:0", "localabstract:scrcpy_"+scid).Output()
	if err != nil {
		return fmt.Errorf("adb forward: %w", err)
	}
	port := strings.TrimSpace(string(out))
	defer exec.Command("adb", "-s", serial, "forward", "--remove", "tcp:"+port).Run()

	srvCtx, stop := context.WithCancel(ctx)
	defer stop()
	srv := exec.CommandContext(srvCtx, "adb", "-s", serial, "shell", "CLASSPATH=/data/local/tmp/scrcpy-server.jar", "app_process", "/", "com.genymobile.scrcpy.Server", version,
		"scid="+scid, "video_codec="+codec, "tunnel_forward=true", "audio="+strconv.FormatBool(wantAudio), "audio_codec=opus", "cleanup=false", "send_device_meta=true",
		"max_size="+strconv.Itoa(num(size, 720, 240, 2000)), "max_fps="+strconv.Itoa(num(fps, 30, 5, 60)), "video_bit_rate="+strconv.Itoa(num(kbps, 2000, 200, 20000)*1000),
		"power_off_on_close=false", "stay_awake=true")
	srv.Stdout, srv.Stderr = io.Discard, io.Discard
	if err := srv.Start(); err != nil {
		return err
	}
	defer func() { stop(); srv.Wait() }()

	dial := func() (net.Conn, error) {
		var last error
		for i := 0; i < 60; i++ {
			c, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 2*time.Second)
			if err == nil {
				return c, nil
			}
			last = err
			time.Sleep(250 * time.Millisecond)
		}
		return nil, last
	}
	// the adb forward accepts at once, even before the server listens: only the dummy byte proves the stream, so retry the whole step
	var video net.Conn
	var vr *bufio.Reader
	for try := 0; ; try++ {
		c, err := dial()
		if err != nil {
			return err
		}
		c.SetReadDeadline(time.Now().Add(5 * time.Second))
		br := bufio.NewReaderSize(c, 1<<20)
		if _, err := io.ReadFull(br, make([]byte, 1)); err == nil {
			video, vr = c, br
			break
		} else if try >= 20 || ctx.Err() != nil {
			c.Close()
			return fmt.Errorf("the device did not start the stream (%v)", err)
		}
		c.Close()
		time.Sleep(500 * time.Millisecond)
	}
	defer video.Close()
	var ar *bufio.Reader
	if wantAudio { // sockets open in the order video, audio, control
		ac, err := dial()
		if err != nil {
			return err
		}
		defer ac.Close()
		ar = bufio.NewReaderSize(ac, 1<<16)
	}
	ctl, err := dial()
	if err != nil {
		return err
	}
	defer ctl.Close()
	meta := make([]byte, 64+4)
	if _, err := io.ReadFull(vr, meta); err != nil { // device name, codec id
		return err
	}
	if string(meta[64:68]) != map[string]string{"h264": "h264", "vp8": "\x00vp8", "vp9": "\x00vp9"}[codec] {
		return errors.New("unexpected codec " + hex.EncodeToString(meta[64:68]))
	}
	video.SetReadDeadline(time.Time{})

	var (
		mu      sync.Mutex
		lastIn  time.Time
		i2f     []float64
		frames  int
		bytesIn int
		width   = 1
		height  = 1
		cfg     []byte
	)
	wsend := func(typ websocket.MessageType, p []byte) error {
		cx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return ws.Write(cx, typ, p)
	}
	errc := make(chan error, 4)
	sid := newSession()
	defer endSession(sid)
	hello, _ := json.Marshal(map[string]any{"t": "hello", "sid": sid})
	wsend(websocket.MessageText, hello)

	// device audio (Opus) -> browser: [0x10|config(1)] [pts u64] [packet]
	if ar != nil {
		go func() {
			var id [4]byte
			if _, err := io.ReadFull(ar, id[:]); err != nil {
				return
			}
			on := string(id[:]) == "opus"
			m, _ := json.Marshal(map[string]any{"t": "audio", "on": on})
			wsend(websocket.MessageText, m)
			if !on { // the device could not capture audio (codec id 0: disabled, 1: error); video goes on
				return
			}
			hdr := make([]byte, 12)
			for {
				if _, err := io.ReadFull(ar, hdr); err != nil {
					return
				}
				pts := binary.BigEndian.Uint64(hdr[:8])
				n := binary.BigEndian.Uint32(hdr[8:12])
				if n > 1<<20 {
					return
				}
				p := make([]byte, 9+n)
				p[0] = 0x10
				if pts&(1<<62) != 0 {
					p[0] |= 1
				}
				binary.BigEndian.PutUint64(p[1:9], pts&(1<<61-1))
				if _, err := io.ReadFull(ar, p[9:]); err != nil {
					return
				}
				if err := wsend(websocket.MessageBinary, p); err != nil {
					return
				}
			}
		}()
	}
	// device messages on the control socket: clipboard text (type 0), set-clipboard ack (1), uhid output (2, ignored)
	go func() {
		cr := bufio.NewReader(ctl)
		for {
			t, err := cr.ReadByte()
			if err != nil {
				return
			}
			switch t {
			case 0:
				var l [4]byte
				if _, err := io.ReadFull(cr, l[:]); err != nil {
					return
				}
				n := binary.BigEndian.Uint32(l[:])
				if n > 1<<20 {
					return
				}
				txt := make([]byte, n)
				if _, err := io.ReadFull(cr, txt); err != nil {
					return
				}
				m, _ := json.Marshal(map[string]any{"t": "clip", "s": string(txt)})
				wsend(websocket.MessageText, m)
			case 1:
				if _, err := io.ReadFull(cr, make([]byte, 8)); err != nil {
					return
				}
			case 2:
				var h [4]byte
				if _, err := io.ReadFull(cr, h[:]); err != nil {
					return
				}
				if _, err := io.ReadFull(cr, make([]byte, binary.BigEndian.Uint16(h[2:]))); err != nil {
					return
				}
			default:
				return
			}
		}
	}()

	// device -> browser
	go func() {
		hdr := make([]byte, 12)
		for {
			if _, err := io.ReadFull(vr, hdr); err != nil {
				errc <- err
				return
			}
			if hdr[0]&0x80 != 0 { // session packet: video size
				mu.Lock()
				width, height = int(binary.BigEndian.Uint32(hdr[4:8])), int(binary.BigEndian.Uint32(hdr[8:12]))
				w, h := width, height
				mu.Unlock()
				if codec != "h264" { // no parameter-set packet: announce the decoder right away
					m, _ := json.Marshal(map[string]any{"t": "cfg", "codec": map[string]string{"vp8": "vp8", "vp9": "vp09.00.10.08"}[codec], "w": w, "h": h})
					if err := wsend(websocket.MessageText, m); err != nil {
						errc <- err
						return
					}
				}
				continue
			}
			pts := binary.BigEndian.Uint64(hdr[:8])
			n := binary.BigEndian.Uint32(hdr[8:12])
			if n > 8<<20 {
				errc <- errors.New("oversized packet")
				return
			}
			p := make([]byte, n)
			if _, err := io.ReadFull(vr, p); err != nil {
				errc <- err
				return
			}
			isCfg, isKey := pts&(1<<62) != 0, pts&(1<<61) != 0
			if isCfg {
				cfg = p
				mu.Lock()
				w, h := width, height
				mu.Unlock()
				m, _ := json.Marshal(map[string]any{"t": "cfg", "codec": codecString(p), "w": w, "h": h})
				if err := wsend(websocket.MessageText, m); err != nil {
					errc <- err
					return
				}
				continue
			}
			mu.Lock()
			frames++
			bytesIn += len(p)
			if !lastIn.IsZero() {
				i2f = append(i2f, float64(time.Since(lastIn).Microseconds())/1000)
				lastIn = time.Time{}
			}
			mu.Unlock()
			flag := byte(0)
			if isKey {
				flag = 2
			}
			if isKey && codec == "h264" {
				p = append(append([]byte{}, cfg...), p...) // every key frame is self-contained (parameter sets first)
			}
			if err := wsend(websocket.MessageBinary, append([]byte{flag}, p...)); err != nil {
				errc <- err
				return
			}
		}
	}()
	// stats
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		var pf, pb int
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				mu.Lock()
				s := append([]float64(nil), i2f...)
				i2f = i2f[:0]
				f, b := frames-pf, bytesIn-pb
				pf, pb = frames, bytesIn
				mu.Unlock()
				sort.Float64s(s)
				med := 0.0
				if len(s) > 0 {
					med = s[len(s)/2]
				}
				m, _ := json.Marshal(map[string]any{"t": "stats", "fps": float64(f) / 2, "kbps": float64(b) * 8 / 2000, "i2f_ms": med, "i2f_n": len(s)})
				wsend(websocket.MessageText, m)
			}
		}
	}()
	// browser -> device
	go func() {
		for {
			typ, p, err := ws.Read(ctx)
			if err != nil {
				errc <- err
				return
			}
			if typ != websocket.MessageText {
				continue
			}
			var m struct {
				T      string  `json:"t"`
				A      int     `json:"a"`
				X      float64 `json:"x"`
				Y      float64 `json:"y"`
				Code   int     `json:"code"`
				S      string  `json:"s"`
				TS     float64 `json:"ts"`
				Button int     `json:"b"`
				DX     float64 `json:"dx"`
				DY     float64 `json:"dy"`
				Paste  bool    `json:"paste"`
				Cut    bool    `json:"cut"`
			}
			if json.Unmarshal(p, &m) != nil {
				continue
			}
			var msg []byte
			switch m.T {
			case "ping":
				r, _ := json.Marshal(map[string]any{"t": "pong", "ts": m.TS})
				wsend(websocket.MessageText, r)
			case "touch": // a: 0 down, 1 up, 2 move; x, y in 0..1
				mu.Lock()
				w, h := width, height
				if m.A != 1 {
					lastIn = time.Now()
				}
				mu.Unlock()
				msg = touchMsg(m.A, m.X, m.Y, w, h)
			case "scroll": // dx, dy in wheel steps (browser pixels / 100, sign as scrcpy: positive dy scrolls up)
				mu.Lock()
				w, h := width, height
				mu.Unlock()
				msg = scrollMsg(m.X, m.Y, m.DX, m.DY, w, h)
			case "clip_set":
				if len(m.S) < 1<<15 {
					msg = clipSetMsg(m.S, m.Paste)
				}
			case "clip_get":
				msg = []byte{8, 1}
				if m.Cut {
					msg[1] = 2
				}
			case "key": // a: 0 down, 1 up; code: Android keycode
				msg = keyMsg(m.A, m.Code)
			case "text":
				if len(m.S) > 0 && len(m.S) < 256 {
					msg = append([]byte{1, 0, 0, 0, byte(len(m.S))}, m.S...)
				}
			}
			if msg != nil {
				if _, err := ctl.Write(msg); err != nil {
					errc <- err
					return
				}
			}
		}
	}()
	select {
	case err := <-errc:
		if websocket.CloseStatus(err) != -1 || errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
			return nil
		}
		return err
	case <-ctx.Done():
		return nil
	}
}

func touchMsg(action int, x, y float64, w, h int) []byte {
	b := make([]byte, 32)
	b[0] = 2
	b[1] = byte(action)
	binary.BigEndian.PutUint64(b[2:], 0xfffffffffffffffe) // SC_POINTER_ID_GENERIC_FINGER
	binary.BigEndian.PutUint32(b[10:], uint32(clamp(x)*float64(w-1)))
	binary.BigEndian.PutUint32(b[14:], uint32(clamp(y)*float64(h-1)))
	binary.BigEndian.PutUint16(b[18:], uint16(w))
	binary.BigEndian.PutUint16(b[20:], uint16(h))
	if action != 1 {
		binary.BigEndian.PutUint16(b[22:], 0xffff)
	}
	if action == 0 || action == 1 {
		binary.BigEndian.PutUint32(b[24:], 1) // primary button
	}
	if action != 1 {
		binary.BigEndian.PutUint32(b[28:], 1)
	}
	return b
}

func keyMsg(action, code int) []byte {
	b := make([]byte, 14)
	b[0] = 0
	b[1] = byte(action)
	binary.BigEndian.PutUint32(b[2:], uint32(code))
	return b
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// codecString builds the WebCodecs codec string (avc1.PPCCLL) from the first SPS in the Annex B config packet.
func codecString(p []byte) string {
	for i := 0; i+7 < len(p); i++ {
		if p[i] == 0 && p[i+1] == 0 && (p[i+2] == 1 || (p[i+2] == 0 && p[i+3] == 1)) {
			j := i + 3
			if p[i+2] == 0 {
				j = i + 4
			}
			if j+3 < len(p) && p[j]&0x1f == 7 {
				return fmt.Sprintf("avc1.%02x%02x%02x", p[j+1], p[j+2], p[j+3])
			}
		}
	}
	return "avc1.42e01f"
}

func scrollMsg(x, y, dx, dy float64, w, h int) []byte {
	b := make([]byte, 21)
	b[0] = 3
	binary.BigEndian.PutUint32(b[1:], uint32(clamp(x)*float64(w-1)))
	binary.BigEndian.PutUint32(b[5:], uint32(clamp(y)*float64(h-1)))
	binary.BigEndian.PutUint16(b[9:], uint16(w))
	binary.BigEndian.PutUint16(b[11:], uint16(h))
	binary.BigEndian.PutUint16(b[13:], uint16(fixed16(dx)))
	binary.BigEndian.PutUint16(b[15:], uint16(fixed16(dy)))
	return b // buttons (b[17:21]) stay 0
}

// fixed16 encodes a float in [-16, 16] the way scrcpy does (i16: 0x7fff is 16, 0x8000 is -16).
func fixed16(v float64) int16 {
	if v > 16 {
		v = 16
	}
	if v < -16 {
		v = -16
	}
	f := v / 16
	if f >= 1 {
		return 0x7fff
	}
	return int16(f * 0x8000)
}

func clipSetMsg(text string, paste bool) []byte {
	b := make([]byte, 14, 14+len(text))
	b[0] = 9
	binary.BigEndian.PutUint64(b[1:], uint64(time.Now().UnixNano())) // sequence: only the ack uses it
	if paste {
		b[9] = 1
	}
	binary.BigEndian.PutUint32(b[10:], uint32(len(text)))
	return append(b, text...)
}

// Viewer sessions: the WebSocket hands the page a random session id; the HTTP actions (APK install) need it, so a ticket (two minutes) is not what keeps them open.
var sessions = struct {
	sync.Mutex
	m map[string]bool
}{m: map[string]bool{}}

func newSession() string {
	var b [16]byte
	rand.Read(b[:])
	id := hex.EncodeToString(b[:])
	sessions.Lock()
	sessions.m[id] = true
	sessions.Unlock()
	return id
}
func endSession(id string) { sessions.Lock(); delete(sessions.m, id); sessions.Unlock() }
func sessionOK(id string) bool {
	sessions.Lock()
	defer sessions.Unlock()
	return id != "" && sessions.m[id]
}
