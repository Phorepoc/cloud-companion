// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func req(remote string, hdr map[string]string) *http.Request {
	r := httptest.NewRequest("GET", "http://phecloud:8080/", nil)
	r.RemoteAddr = remote
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

func TestResolve(t *testing.T) {
	tr, err := ParseTrust("172.18.0.0/16, 10.1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		remote string
		hdr    map[string]string
		scheme string
		host   string
		ip     string
	}{
		{"direct, no headers", "203.0.113.9:5000", nil, "http", "phecloud:8080", "203.0.113.9"},
		{"untrusted peer: every header ignored", "203.0.113.9:5000", map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "evil.example", "X-Forwarded-For": "1.2.3.4", "Forwarded": "for=5.6.7.8;host=evil.example;proto=https", "CF-Connecting-IP": "9.9.9.9"}, "http", "phecloud:8080", "203.0.113.9"},
		{"trusted proxy, X-Forwarded-*", "172.18.0.5:4000", map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "cloud.example.org", "X-Forwarded-For": "198.51.100.7"}, "https", "cloud.example.org", "198.51.100.7"},
		{"trusted proxy chain: the first untrusted address from the right", "172.18.0.5:4000", map[string]string{"X-Forwarded-For": "6.6.6.6, 198.51.100.7, 172.18.0.9"}, "http", "phecloud:8080", "198.51.100.7"},
		{"a forged leftmost address is not believed", "172.18.0.5:4000", map[string]string{"X-Forwarded-For": "1.1.1.1, 198.51.100.7"}, "http", "phecloud:8080", "198.51.100.7"},
		{"Forwarded (RFC 7239)", "10.1.2.3:1", map[string]string{"Forwarded": "for=\"[2001:db8::1]\";host=cloud.example.org;proto=https"}, "https", "cloud.example.org", "2001:db8::1"},
		{"Cloudflare", "172.18.0.2:1", map[string]string{"CF-Connecting-IP": "198.51.100.77", "CF-Visitor": "{\"scheme\":\"https\"}"}, "https", "phecloud:8080", "198.51.100.77"},
		{"junk scheme is ignored", "172.18.0.5:4000", map[string]string{"X-Forwarded-Proto": "javascript"}, "http", "phecloud:8080", "172.18.0.5"},
	}
	for _, c := range cases {
		in := tr.Resolve(req(c.remote, c.hdr))
		if in.Scheme != c.scheme || in.Host != c.host || in.ClientIP != c.ip {
			t.Errorf("%s: got %+v, want %s %s %s", c.name, in, c.scheme, c.host, c.ip)
		}
	}
	// nobody trusted (the default)
	none, _ := ParseTrust("")
	if in := none.Resolve(req("172.18.0.5:1", map[string]string{"X-Forwarded-Proto": "https"})); in.Scheme != "http" || in.Proxied {
		t.Errorf("an empty trust list must trust nobody: %+v", in)
	}
	if _, err := ParseTrust("not-an-ip"); err == nil {
		t.Error("a bad entry must be an error")
	}
}
