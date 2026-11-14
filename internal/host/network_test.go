// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package host

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeNM is NetworkManager on a board with Wi-Fi and no cable: nmcli answers from its state.
type fakeNM struct {
	mu       sync.Mutex
	active   []string // "name:type:device"
	hotspot  bool
	password string // the home Wi-Fi's
	calls    []string
}

func (f *fakeNM) exec(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := strings.Join(args, " ")
	f.calls = append(f.calls, cmd)
	switch {
	case strings.HasPrefix(cmd, "-t -f NAME,TYPE,DEVICE connection show --active"):
		out := strings.Join(f.active, "\n")
		if f.hotspot {
			out += "\ncloud-setup:802-11-wireless:wlan0"
		}
		return []byte(out + "\n"), nil
	case strings.HasPrefix(cmd, "-t -f NAME connection show --active"):
		out := ""
		for _, a := range f.active {
			out += strings.SplitN(a, ":", 2)[0] + "\n"
		}
		if f.hotspot {
			out += "cloud-setup\n"
		}
		return []byte(out), nil
	case strings.HasPrefix(cmd, "-t -f DEVICE,TYPE device"):
		return []byte("eth0:ethernet\nwlan0:wifi\nlo:loopback\n"), nil
	case strings.HasPrefix(cmd, "-t -f SSID,SIGNAL,SECURITY device wifi list"):
		if f.hotspot {
			return nil, errors.New("busy") // the radio is an access point
		}
		return []byte("Home\\: 5G:80:WPA2\nCafe::\nHome\\: 5G:40:WPA2\n:30:WPA2\nCloud setup:99:\n"), nil
	case strings.HasPrefix(cmd, "connection up cloud-setup"):
		f.hotspot = true
	case strings.HasPrefix(cmd, "connection down cloud-setup"):
		f.hotspot = false
	case strings.HasPrefix(cmd, "device wifi connect "):
		if !strings.Contains(cmd, "password "+f.password) {
			return []byte("Error: Secrets were required, but not provided.\n"), errors.New("exit status 4")
		}
		f.active = append(f.active, `Home\: 5G:802-11-wireless:wlan0`) // nmcli -t escapes the colon
	}
	return nil, nil
}

func (f *fakeNM) did(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func TestSplitTerse(t *testing.T) {
	if got := splitTerse(`Home\: 5G:80:WPA2 WPA3`); !reflect.DeepEqual(got, []string{"Home: 5G", "80", "WPA2 WPA3"}) {
		t.Errorf("%q", got)
	}
	if got := splitTerse(`a\\b::`); !reflect.DeepEqual(got, []string{`a\b`, "", ""}) {
		t.Errorf("%q", got)
	}
}

func TestScanOnePerNameStrongestFirst(t *testing.T) {
	nm := &fakeNM{}
	n := &Network{Exec: nm.exec, Iface: "wlan0", SSID: "Cloud setup"}
	got := n.Scan(context.Background())
	want := []WiFi{{"Home: 5G", 80, true}, {"Cafe", 0, false}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%+v", got)
	}
}

func TestSetupHotspotAndJoin(t *testing.T) {
	s, _, ck, srv := newSetup(t, "PINE-APPLE-42")
	nm := &fakeNM{password: "letmein123"}
	s.Net, s.HotspotAt = &Network{Exec: nm.exec, Iface: "wlan0", SSID: "Cloud setup"}, 90*time.Second
	c := newClient()
	ctx := context.Background()

	s.Tick(ctx) // a minute after boot: the network may still come
	if nm.did("connection up cloud-setup") {
		t.Fatal("hotspot before 90 s")
	}
	ck.add(time.Minute)
	s.Tick(ctx)
	if !nm.did("connection up cloud-setup") {
		t.Fatal("no hotspot after 90 s offline")
	}
	nf := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if res, err := nf.Get(srv.URL + "/generate_204"); err != nil || res.StatusCode != http.StatusFound || res.Header.Get("Location") != "http://10.42.0.1/" {
		t.Errorf("a phone's sign-in check is not sent to the page: %v %v", res.StatusCode, err)
	}

	if getState(t, c, srv)["network"] != nil {
		t.Error("the network is shown before unlocking")
	}
	post(t, c, srv, "/api/unlock", `{"secret":"PINE-APPLE-42"}`)
	net := getState(t, c, srv)["network"].(map[string]any)
	if net["online"] != false || net["hotspot"] != true || len(net["wifi"].([]any)) != 2 {
		t.Fatalf("network: %v", net)
	}
	if post(t, c, srv, "/api/setup", `{"passphrase":"correct horse battery","features":[]}`) != 409 {
		t.Error("setup started without the internet")
	}

	wait := func() map[string]any {
		for i := 0; i < 100; i++ {
			if n := getState(t, c, srv)["network"].(map[string]any); n["joining"] == nil {
				return n
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("still joining")
		return nil
	}
	if post(t, c, srv, "/api/wifi", `{"ssid":"Home: 5G","password":"wrong"}`) != 202 {
		t.Fatal("join refused")
	}
	if n := getState(t, c, srv)["network"].(map[string]any); n["joining"] != "Home: 5G" {
		t.Errorf("joining not shown: %v", n)
	}
	n := wait()
	if n["online"] != false || n["hotspot"] != true || !strings.Contains(n["error"].(string), "Secrets were required") {
		t.Fatalf("after a wrong password: %v", n)
	}
	post(t, c, srv, "/api/wifi", `{"ssid":"Home: 5G","password":"letmein123"}`)
	n = wait()
	if n["online"] != true || n["hotspot"] != false || n["error"] != nil {
		t.Fatalf("after joining: %v", n)
	}
	if post(t, c, srv, "/api/setup", `{"passphrase":"correct horse battery","features":[]}`) != 202 {
		t.Error("setup refused once online")
	}
	if res, _ := nf.Get(srv.URL + "/generate_204"); res.StatusCode != http.StatusNotFound {
		t.Errorf("sign-in redirect without the hotspot: %d", res.StatusCode)
	}
}
