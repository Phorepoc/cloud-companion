// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Network is the board's network as NetworkManager (nmcli) sees it: whether it is online, the Wi-Fi networks around, and the
// setup hotspot ("Cloud setup", open, the setup page at 10.42.0.1) for a board that has no network yet.
type Network struct {
	Exec   func(ctx context.Context, name string, args ...string) ([]byte, error)
	Iface  string // "wlan0"
	SSID   string // the hotspot's name: "Cloud setup"
	mu     sync.Mutex
	nearby []WiFi // scanned before the hotspot starts: one radio cannot scan well while it is an access point
}

// WiFi is a network around the board.
type WiFi struct {
	SSID   string `json:"ssid"`
	Signal int    `json:"signal"` // 0-100
	Secure bool   `json:"secure"`
}

const hotspotCon = "cloud-setup"

func (n *Network) nmcli(ctx context.Context, args ...string) (string, error) {
	out, err := n.Exec(ctx, "nmcli", args...)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		msg = strings.TrimPrefix(msg, "Error: ")
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return string(out), nil
}

// Online: NetworkManager has a connection other than the hotspot (cable or Wi-Fi).
func (n *Network) Online(ctx context.Context) bool {
	out, err := n.nmcli(ctx, "-t", "-f", "NAME,TYPE,DEVICE", "connection", "show", "--active")
	if err != nil {
		return false
	}
	for _, l := range strings.Split(out, "\n") {
		f := splitTerse(l)
		if len(f) == 3 && f[0] != hotspotCon && f[2] != "lo" && (strings.Contains(f[1], "ethernet") || strings.Contains(f[1], "wireless")) {
			return true
		}
	}
	return false
}

// HasWiFi: the board has a Wi-Fi radio.
func (n *Network) HasWiFi(ctx context.Context) bool {
	out, err := n.nmcli(ctx, "-t", "-f", "DEVICE,TYPE", "device")
	if err != nil {
		return false
	}
	for _, l := range strings.Split(out, "\n") {
		if f := splitTerse(l); len(f) == 2 && f[0] == n.Iface && f[1] == "wifi" {
			return true
		}
	}
	return false
}

// Hotspot says whether the setup hotspot is up.
func (n *Network) Hotspot(ctx context.Context) bool {
	out, err := n.nmcli(ctx, "-t", "-f", "NAME", "connection", "show", "--active")
	return err == nil && strings.Contains("\n"+out, "\n"+hotspotCon+"\n")
}

// Scan lists the Wi-Fi networks around, strongest first, one entry per name. While the hotspot is up it gives the list
// from before.
func (n *Network) Scan(ctx context.Context) []WiFi {
	if n.Hotspot(ctx) {
		n.mu.Lock()
		defer n.mu.Unlock()
		return n.nearby
	}
	out, err := n.nmcli(ctx, "-t", "-f", "SSID,SIGNAL,SECURITY", "device", "wifi", "list", "ifname", n.Iface, "--rescan", "yes")
	if err != nil {
		return nil
	}
	best := map[string]WiFi{}
	for _, l := range strings.Split(out, "\n") {
		f := splitTerse(l)
		if len(f) != 3 || f[0] == "" || f[0] == n.SSID {
			continue
		}
		sig, _ := strconv.Atoi(f[1])
		w := WiFi{SSID: f[0], Signal: sig, Secure: f[2] != "" && f[2] != "--"}
		if b, ok := best[w.SSID]; !ok || w.Signal > b.Signal {
			best[w.SSID] = w
		}
	}
	list := make([]WiFi, 0, len(best))
	for _, w := range best {
		list = append(list, w)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Signal != list[j].Signal {
			return list[i].Signal > list[j].Signal
		}
		return list[i].SSID < list[j].SSID
	})
	n.mu.Lock()
	n.nearby = list
	n.mu.Unlock()
	return list
}

// StartHotspot scans (for the page's list), then opens the setup hotspot: open, shared (NetworkManager's DHCP and DNS on
// 10.42.0.1; the image's dnsmasq-shared.d answers every name with it, so phones show the page as a sign-in page).
func (n *Network) StartHotspot(ctx context.Context) error {
	n.Scan(ctx)
	n.nmcli(ctx, "connection", "delete", hotspotCon) // a left-over one, with old settings
	if _, err := n.nmcli(ctx, "connection", "add", "type", "wifi", "ifname", n.Iface, "con-name", hotspotCon, "autoconnect", "no",
		"ssid", n.SSID, "802-11-wireless.mode", "ap", "802-11-wireless.band", "bg", "ipv4.method", "shared", "ipv6.method", "disabled"); err != nil {
		return fmt.Errorf("hotspot: %w", err)
	}
	if _, err := n.nmcli(ctx, "connection", "up", hotspotCon); err != nil {
		return fmt.Errorf("hotspot: %w", err)
	}
	return nil
}

// StopHotspot closes the setup hotspot.
func (n *Network) StopHotspot(ctx context.Context) {
	n.nmcli(ctx, "connection", "down", hotspotCon)
	n.nmcli(ctx, "connection", "delete", hotspotCon)
}

// Join connects to a Wi-Fi network (closing the hotspot first: one radio). NetworkManager keeps it for the next boots.
func (n *Network) Join(ctx context.Context, ssid, password string) error {
	n.StopHotspot(ctx)
	args := []string{"device", "wifi", "connect", ssid, "ifname", n.Iface}
	if password != "" {
		args = append(args, "password", password)
	}
	if _, err := n.nmcli(ctx, args...); err != nil {
		return err
	}
	return nil
}

// splitTerse splits a line of nmcli -t output: fields by ':', with '\:' and '\\' escaped.
func splitTerse(l string) []string {
	l = strings.TrimRight(l, "\r")
	if l == "" {
		return nil
	}
	var out []string
	var b strings.Builder
	for i := 0; i < len(l); i++ {
		switch {
		case l[i] == '\\' && i+1 < len(l):
			i++
			b.WriteByte(l[i])
		case l[i] == ':':
			out = append(out, b.String())
			b.Reset()
		default:
			b.WriteByte(l[i])
		}
	}
	return append(out, b.String())
}
