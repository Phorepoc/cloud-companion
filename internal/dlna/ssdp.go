// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package dlna

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"time"
)

// SSDP, the discovery half of UPnP: players ask the multicast group 239.255.255.250:1900 who is there (M-SEARCH), and the
// server answers each asker directly; it also announces itself (NOTIFY) when it starts, every 15 minutes, and says goodbye.

var ssdpGroup = &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}

// targets are the search targets the server answers to.
func (s *Server) targets() []string {
	return []string{"upnp:rootdevice", s.uuid, "urn:schemas-upnp-org:device:MediaServer:1",
		"urn:schemas-upnp-org:service:ContentDirectory:1", "urn:schemas-upnp-org:service:ConnectionManager:1"}
}

func (s *Server) usn(st string) string {
	if st == s.uuid {
		return s.uuid
	}
	return s.uuid + "::" + st
}

func (s *Server) location(ip net.IP) string { return fmt.Sprintf("http://%s:%d/desc.xml", ip, s.port) }

const serverHeader = "Linux/1 UPnP/1.0 CloudCompanion/1"

// answer is the reply to an M-SEARCH, one per matching target; nil when the request is not one for this server.
func (s *Server) answer(req []byte, local net.IP) [][]byte {
	r, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(req)))
	if err != nil || r.Method != "M-SEARCH" || strings.Trim(r.Header.Get("MAN"), `"`) != "ssdp:discover" {
		return nil
	}
	st := r.Header.Get("ST")
	var sts []string
	if st == "ssdp:all" {
		sts = s.targets()
	} else {
		for _, t := range s.targets() {
			if t == st {
				sts = []string{t}
			}
		}
	}
	var out [][]byte
	for _, t := range sts {
		out = append(out, []byte("HTTP/1.1 200 OK\r\nCACHE-CONTROL: max-age=1800\r\nDATE: "+time.Now().UTC().Format(http.TimeFormat)+
			"\r\nEXT:\r\nLOCATION: "+s.location(local)+"\r\nSERVER: "+serverHeader+"\r\nST: "+t+"\r\nUSN: "+s.usn(t)+"\r\nContent-Length: 0\r\n\r\n"))
	}
	return out
}

func (s *Server) notify(nts string, ip net.IP) [][]byte {
	var out [][]byte
	for _, t := range s.targets() {
		m := "NOTIFY * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nNT: " + t + "\r\nNTS: " + nts + "\r\nUSN: " + s.usn(t) + "\r\n"
		if nts == "ssdp:alive" {
			m += "CACHE-CONTROL: max-age=1800\r\nLOCATION: " + s.location(ip) + "\r\nSERVER: " + serverHeader + "\r\n"
		}
		out = append(out, []byte(m+"\r\n"))
	}
	return out
}

// localIPv4 are this machine's addresses on networks that carry multicast.
func localIPv4() []net.IP {
	var out []net.IP
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagMulticast == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
				out = append(out, n.IP.To4())
			}
		}
	}
	return out
}

// localFor is the address of this machine the asker reaches it on.
func localFor(to *net.UDPAddr) net.IP {
	c, err := net.DialUDP("udp4", nil, to)
	if err != nil {
		return nil
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP
}

func (s *Server) announce(nts string) {
	for _, ip := range localIPv4() {
		c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip})
		if err != nil {
			continue
		}
		for _, m := range s.notify(nts, ip) {
			c.WriteToUDP(m, ssdpGroup)
		}
		c.Close()
	}
}

func (s *Server) ssdp(ctx context.Context) {
	c, err := net.ListenMulticastUDP("udp4", nil, ssdpGroup)
	if err != nil {
		s.Log.Error("DLNA: players cannot find this server (SSDP)", "err", err)
		return
	}
	go func() { <-ctx.Done(); s.announce("ssdp:byebye"); c.Close() }()
	go func() {
		for t := time.NewTicker(15 * time.Minute); ; {
			s.announce("ssdp:alive")
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
		}
	}()
	buf := make([]byte, 4096)
	for {
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			return
		}
		local := localFor(from)
		if local == nil {
			continue
		}
		replies := s.answer(buf[:n], local)
		if len(replies) == 0 {
			continue
		}
		go func(to *net.UDPAddr) { // answers are spread over a moment, as the MX header asks
			time.Sleep(time.Duration(rand.IntN(400)) * time.Millisecond)
			out, err := net.DialUDP("udp4", nil, to)
			if err != nil {
				return
			}
			defer out.Close()
			for _, m := range replies {
				out.Write(m)
			}
		}(from)
	}
}
