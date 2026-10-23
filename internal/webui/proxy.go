// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

// Behind a reverse proxy. The web port speaks plain HTTP and the proxy terminates TLS. The proxy's headers are believed ONLY when the TCP
// peer is in PHECLOUD_TRUSTED_PROXIES (comma separated IPs or CIDRs, for example the proxy's docker network 172.18.0.0/16); from anyone else they
// are ignored, so a client that reaches the port directly cannot choose the origin, the host or its own address.
//
// Read, in this order: Forwarded (RFC 7239: for, host, proto), X-Forwarded-Proto / -Host / -For, Cloudflare's CF-Visitor (scheme) and
// CF-Connecting-IP. The client address is the first entry of X-Forwarded-For that is not itself a trusted proxy, walking from the right.
// What is derived: the public origin and host (WebAuthn relying party id and origin, the WebSocket origin check), the client address (rate
// limiter, logs). The app sets no cookies and builds no absolute URLs, and the browser picks ws or wss from the page's own scheme.

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Trust is the set of proxies whose forwarding headers are believed.
type Trust struct{ nets []netip.Prefix }

// ParseTrust reads a comma separated list of IPs and CIDRs; an empty list trusts nobody.
func ParseTrust(list string) (*Trust, error) {
	t := &Trust{}
	for _, s := range strings.Split(list, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		if p, err := netip.ParsePrefix(s); err == nil {
			t.nets = append(t.nets, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(s)
		if err != nil {
			return nil, err
		}
		t.nets = append(t.nets, netip.PrefixFrom(a, a.BitLen()))
	}
	return t, nil
}

func (t *Trust) trusted(ip netip.Addr) bool {
	if t == nil {
		return false
	}
	ip = ip.Unmap()
	for _, p := range t.nets {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// Info is what the request really was, from the client's point of view.
type Info struct {
	Scheme   string // http | https
	Host     string // the host (and port) the browser used
	ClientIP string
	Proxied  bool // the headers of a trusted proxy were used
}

type infoKey struct{}

func peerIP(remote string) netip.Addr {
	h, _, err := net.SplitHostPort(remote)
	if err != nil {
		h = remote
	}
	a, _ := netip.ParseAddr(strings.Trim(h, "[]"))
	return a
}

// Resolve works out the Info of a request.
func (t *Trust) Resolve(r *http.Request) Info {
	in := Info{Scheme: "http", Host: r.Host, ClientIP: peerIP(r.RemoteAddr).String()}
	if r.TLS != nil {
		in.Scheme = "https"
	}
	if in.ClientIP == "" || in.ClientIP == "invalid IP" {
		in.ClientIP = r.RemoteAddr
	}
	peer := peerIP(r.RemoteAddr)
	if !peer.IsValid() || !t.trusted(peer) {
		return in
	}
	in.Proxied = true
	// Forwarded (RFC 7239): the last element was added by the nearest proxy
	var fwdFor []string
	if v := r.Header.Get("Forwarded"); v != "" {
		els := strings.Split(v, ",")
		for _, el := range els {
			for _, kv := range strings.Split(el, ";") {
				k, val, ok := strings.Cut(strings.TrimSpace(kv), "=")
				if !ok {
					continue
				}
				val = strings.Trim(val, "\"")
				switch strings.ToLower(k) {
				case "for":
					fwdFor = append(fwdFor, strings.Trim(val, "[]"))
				}
			}
		}
		last := els[len(els)-1]
		for _, kv := range strings.Split(last, ";") {
			k, val, ok := strings.Cut(strings.TrimSpace(kv), "=")
			if !ok {
				continue
			}
			val = strings.Trim(val, "\"")
			switch strings.ToLower(k) {
			case "proto":
				if val == "http" || val == "https" {
					in.Scheme = val
				}
			case "host":
				in.Host = val
			}
		}
	}
	last := func(h string) string {
		v := r.Header.Get(h)
		if v == "" {
			return ""
		}
		p := strings.Split(v, ",")
		return strings.TrimSpace(p[len(p)-1])
	}
	if r.Header.Get("Forwarded") == "" || !strings.Contains(strings.ToLower(r.Header.Get("Forwarded")), "proto=") {
		if p := strings.ToLower(last("X-Forwarded-Proto")); p == "http" || p == "https" {
			in.Scheme = p
		} else if v := r.Header.Get("CF-Visitor"); v != "" {
			var cv struct{ Scheme string }
			if json.Unmarshal([]byte(v), &cv) == nil && (cv.Scheme == "http" || cv.Scheme == "https") {
				in.Scheme = cv.Scheme
			}
		}
	}
	if !strings.Contains(strings.ToLower(r.Header.Get("Forwarded")), "host=") {
		if h := last("X-Forwarded-Host"); h != "" {
			in.Host = h
		}
	}
	// the client: walk the chain from the right, skipping trusted proxies
	chain := fwdFor
	if len(chain) == 0 {
		for _, s := range strings.Split(r.Header.Get("X-Forwarded-For"), ",") {
			if s = strings.TrimSpace(s); s != "" {
				chain = append(chain, strings.Trim(s, "[]"))
			}
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(chain[i])
		if err != nil {
			break // not an address (for example an obfuscated identifier): stop believing the chain
		}
		in.ClientIP = a.String()
		if !t.trusted(a) {
			return in
		}
	}
	if len(chain) == 0 {
		if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cf != "" {
			if a, err := netip.ParseAddr(cf); err == nil {
				in.ClientIP = a.String()
			}
		}
	}
	return in
}

// withInfo stores the resolved Info in the request context.
func (t *Trust) withInfo(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), infoKey{}, t.Resolve(r))))
	})
}

func infoOf(r *http.Request) Info {
	if v, ok := r.Context().Value(infoKey{}).(Info); ok {
		return v
	}
	return (*Trust)(nil).Resolve(r)
}
