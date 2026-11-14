// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package dlna makes Cloud Companion a UPnP/DLNA media server on the home network: TVs, consoles, receivers and players (VLC,
// Kodi, BubbleUPnP, the N9's and Symbian's media apps) find it on their own and play the music, videos and photos in Files.
// It answers SSDP discovery (multicast, so the container needs the host's network, or one bridged to the LAN) and the
// ContentDirectory service's Browse; the files themselves are served over plain HTTP.
//
// DLNA has no sign-in: everyone on the network can browse and play what it shares. So it is off unless PHECLOUD_DLNA_LISTEN
// is set, and it shares only media (audio, video, pictures) and their folders, never other files.
package dlna

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/phorepoc/cloud-companion/internal/files"
)

// Library is what is shared: folders and files with paths as in the Files section ("" is the top).
type Library interface {
	List(ctx context.Context, p string) ([]files.Entry, error)
	Serve(w http.ResponseWriter, r *http.Request, p string)
}

// Server is the media server.
type Server struct {
	Name string // shown on TVs and players
	Lib  Library
	Log  *slog.Logger

	uuid   string
	port   int
	update atomic.Uint32
}

// New makes a server named name; its UPnP identity comes from the name and the machine, so players keep recognising it.
func New(name string, lib Library, log *slog.Logger) *Server {
	host, _ := net.LookupAddr("127.0.0.1")
	h := sha1.Sum([]byte(name + "|" + strings.Join(host, ",") + "|phecloud-dlna"))
	u := fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], append([]byte{0x50 | h[6]&0x0f}, h[7]), append([]byte{0x80 | h[8]&0x3f}, h[9]), h[10:16])
	if log == nil {
		log = slog.Default()
	}
	return &Server{Name: name, Lib: lib, Log: log, uuid: "uuid:" + u}
}

// ListenAndServe serves HTTP on addr and answers discovery until ctx ends.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return err
	}
	s.port = ln.Addr().(*net.TCPAddr).Port
	hs := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { <-ctx.Done(); hs.Close() }()
	go s.ssdp(ctx)
	s.Log.Info("DLNA media server", "addr", ln.Addr().String(), "name", s.Name)
	err = hs.Serve(ln)
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// Handler serves the description, the services and the media.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /desc.xml", s.desc)
	mux.HandleFunc("GET /cd.xml", func(w http.ResponseWriter, r *http.Request) { writeXML(w, cdSCPD) })
	mux.HandleFunc("GET /cm.xml", func(w http.ResponseWriter, r *http.Request) { writeXML(w, cmSCPD) })
	mux.HandleFunc("POST /ctl/cd", s.contentDirectory)
	mux.HandleFunc("POST /ctl/cm", s.connectionManager)
	mux.HandleFunc("/media/{id}/{name}", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "read-only", http.StatusMethodNotAllowed)
			return
		}
		p, ok := pathOf(r.PathValue("id"))
		if !ok || p == "" || !media(files.TypeOf(p)) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("transferMode.dlna.org", "Streaming")
		w.Header().Set("contentFeatures.dlna.org", "DLNA.ORG_OP=01;DLNA.ORG_CI=0")
		s.Lib.Serve(w, r, p)
	})
	return mux
}

func writeXML(w http.ResponseWriter, b string) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	io.WriteString(w, xml.Header+b)
}

func media(t string) bool {
	return strings.HasPrefix(t, "audio/") || strings.HasPrefix(t, "video/") || strings.HasPrefix(t, "image/")
}

// Object IDs: "0" is the top, others are "p" and the path in base64.
func idOf(p string) string {
	if p == "" {
		return "0"
	}
	return "p" + base64.RawURLEncoding.EncodeToString([]byte(p))
}

func pathOf(id string) (string, bool) {
	if id == "0" {
		return "", true
	}
	if !strings.HasPrefix(id, "p") {
		return "", false
	}
	b, err := base64.RawURLEncoding.DecodeString(id[1:])
	if err != nil {
		return "", false
	}
	c, err := files.Clean(string(b))
	return c, err == nil
}

func parentOf(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return idOf(p[:i])
	}
	return "0"
}

func (s *Server) desc(w http.ResponseWriter, r *http.Request) {
	writeXML(w, `<root xmlns="urn:schemas-upnp-org:device-1-0" xmlns:dlna="urn:schemas-dlna-org:device-1-0">
<specVersion><major>1</major><minor>0</minor></specVersion>
<device>
<deviceType>urn:schemas-upnp-org:device:MediaServer:1</deviceType>
<friendlyName>`+html.EscapeString(s.Name)+`</friendlyName>
<manufacturer>Phorepoc</manufacturer>
<manufacturerURL>https://github.com/phorepoc/cloud-companion</manufacturerURL>
<modelName>Cloud Companion</modelName>
<modelDescription>Your music, videos and photos from Cloud Companion</modelDescription>
<UDN>`+s.uuid+`</UDN>
<dlna:X_DLNADOC>DMS-1.50</dlna:X_DLNADOC>
<serviceList>
<service><serviceType>urn:schemas-upnp-org:service:ContentDirectory:1</serviceType><serviceId>urn:upnp-org:serviceId:ContentDirectory</serviceId><SCPDURL>/cd.xml</SCPDURL><controlURL>/ctl/cd</controlURL><eventSubURL>/evt/cd</eventSubURL></service>
<service><serviceType>urn:schemas-upnp-org:service:ConnectionManager:1</serviceType><serviceId>urn:upnp-org:serviceId:ConnectionManager</serviceId><SCPDURL>/cm.xml</SCPDURL><controlURL>/ctl/cm</controlURL><eventSubURL>/evt/cm</eventSubURL></service>
</serviceList>
</device>
</root>`)
}

// soap reads a SOAP action's name and arguments.
func soap(r *http.Request) (string, map[string]string, error) {
	action := strings.Trim(r.Header.Get("SOAPACTION"), `"`)
	if i := strings.LastIndex(action, "#"); i >= 0 {
		action = action[i+1:]
	}
	args := map[string]string{}
	d := xml.NewDecoder(io.LimitReader(r.Body, 64<<10))
	depth := 0
	var name string
	for {
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, err
		}
		switch e := t.(type) {
		case xml.StartElement:
			depth++
			name = e.Name.Local
			if depth == 3 && action == "" {
				action = name
			}
		case xml.CharData:
			if depth == 4 {
				args[name] += string(e)
			}
		case xml.EndElement:
			depth--
		}
	}
	return action, args, nil
}

func soapReply(w http.ResponseWriter, service, action string, out [][2]string) {
	var b strings.Builder
	b.WriteString(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	fmt.Fprintf(&b, `<u:%sResponse xmlns:u="urn:schemas-upnp-org:service:%s:1">`, action, service)
	for _, kv := range out {
		fmt.Fprintf(&b, "<%s>%s</%s>", kv[0], html.EscapeString(kv[1]), kv[0])
	}
	fmt.Fprintf(&b, `</u:%sResponse></s:Body></s:Envelope>`, action)
	writeXML(w, b.String())
}

func soapFault(w http.ResponseWriter, code int, desc string) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.WriteHeader(500)
	fmt.Fprintf(w, `%s<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><s:Fault><faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring><detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0"><errorCode>%d</errorCode><errorDescription>%s</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`,
		xml.Header, code, html.EscapeString(desc))
}

func (s *Server) connectionManager(w http.ResponseWriter, r *http.Request) {
	action, _, err := soap(r)
	if err != nil {
		soapFault(w, 402, "bad request")
		return
	}
	switch action {
	case "GetProtocolInfo":
		soapReply(w, "ConnectionManager", action, [][2]string{{"Source", "http-get:*:audio/*:*,http-get:*:video/*:*,http-get:*:image/*:*"}, {"Sink", ""}})
	case "GetCurrentConnectionIDs":
		soapReply(w, "ConnectionManager", action, [][2]string{{"ConnectionIDs", "0"}})
	case "GetCurrentConnectionInfo":
		soapReply(w, "ConnectionManager", action, [][2]string{{"RcsID", "-1"}, {"AVTransportID", "-1"}, {"ProtocolInfo", ""},
			{"PeerConnectionManager", ""}, {"PeerConnectionID", "-1"}, {"Direction", "Output"}, {"Status", "OK"}})
	default:
		soapFault(w, 401, "invalid action")
	}
}

func (s *Server) contentDirectory(w http.ResponseWriter, r *http.Request) {
	action, args, err := soap(r)
	if err != nil {
		soapFault(w, 402, "bad request")
		return
	}
	switch action {
	case "GetSystemUpdateID":
		soapReply(w, "ContentDirectory", action, [][2]string{{"Id", strconv.Itoa(int(s.update.Load()))}})
	case "GetSearchCapabilities":
		soapReply(w, "ContentDirectory", action, [][2]string{{"SearchCaps", ""}})
	case "GetSortCapabilities":
		soapReply(w, "ContentDirectory", action, [][2]string{{"SortCaps", ""}})
	case "Browse":
		s.browse(w, r, args)
	default:
		soapFault(w, 401, "invalid action")
	}
}

func (s *Server) browse(w http.ResponseWriter, r *http.Request, args map[string]string) {
	p, ok := pathOf(args["ObjectID"])
	if !ok {
		soapFault(w, 701, "no such object")
		return
	}
	start, _ := strconv.Atoi(args["StartingIndex"])
	count, _ := strconv.Atoi(args["RequestedCount"])
	base := "http://" + r.Host
	var objs []string
	total := 0
	switch args["BrowseFlag"] {
	case "BrowseMetadata":
		if p == "" {
			objs = []string{fmt.Sprintf(`<container id="0" parentID="-1" restricted="1" searchable="0"><dc:title>%s</dc:title><upnp:class>object.container.storageFolder</upnp:class></container>`, html.EscapeString(s.Name))}
		} else {
			dir := ""
			if i := strings.LastIndex(p, "/"); i >= 0 {
				dir = p[:i]
			}
			l, err := s.Lib.List(r.Context(), dir)
			if err != nil {
				soapFault(w, 701, "no such object")
				return
			}
			for _, e := range l {
				if e.Path == p && (e.Dir || media(e.Type)) {
					objs = []string{s.didl(e, base)}
				}
			}
			if objs == nil {
				soapFault(w, 701, "no such object")
				return
			}
		}
		total = 1
	case "BrowseDirectChildren":
		l, err := s.Lib.List(r.Context(), p)
		if err != nil {
			soapFault(w, 701, "no such container")
			return
		}
		var keep []files.Entry
		for _, e := range l {
			if (e.Dir || media(e.Type)) && !strings.HasPrefix(e.Name, ".") {
				keep = append(keep, e)
			}
		}
		total = len(keep)
		if start > len(keep) {
			start = len(keep)
		}
		keep = keep[start:]
		if count > 0 && count < len(keep) {
			keep = keep[:count]
		}
		for _, e := range keep {
			objs = append(objs, s.didl(e, base))
		}
	default:
		soapFault(w, 402, "BrowseFlag must be BrowseDirectChildren or BrowseMetadata")
		return
	}
	result := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">` +
		strings.Join(objs, "") + `</DIDL-Lite>`
	soapReply(w, "ContentDirectory", "Browse", [][2]string{{"Result", result}, {"NumberReturned", strconv.Itoa(len(objs))},
		{"TotalMatches", strconv.Itoa(total)}, {"UpdateID", strconv.Itoa(int(s.update.Load()))}})
}

// didl is one folder or file as DIDL-Lite.
func (s *Server) didl(e files.Entry, base string) string {
	id, parent, title := idOf(e.Path), parentOf(e.Path), html.EscapeString(e.Name)
	if e.Dir {
		return fmt.Sprintf(`<container id="%s" parentID="%s" restricted="1" searchable="0"><dc:title>%s</dc:title><upnp:class>object.container.storageFolder</upnp:class></container>`, id, parent, title)
	}
	class := "object.item.imageItem.photo"
	switch {
	case strings.HasPrefix(e.Type, "audio/"):
		class = "object.item.audioItem.musicTrack"
	case strings.HasPrefix(e.Type, "video/"):
		class = "object.item.videoItem"
	}
	size := ""
	if e.Size > 0 {
		size = fmt.Sprintf(` size="%d"`, e.Size)
	}
	date := ""
	if !e.Mod.IsZero() {
		date = "<dc:date>" + e.Mod.UTC().Format("2006-01-02T15:04:05") + "</dc:date>"
	}
	u := base + "/media/" + id + "/" + url.PathEscape(path.Base(e.Path))
	res := fmt.Sprintf(`<res protocolInfo="http-get:*:%s:DLNA.ORG_OP=01;DLNA.ORG_CI=0"%s>%s</res>`, e.Type, size, html.EscapeString(u))
	art := ""
	if class == "object.item.imageItem.photo" {
		art = "<upnp:albumArtURI>" + html.EscapeString(u) + "</upnp:albumArtURI>"
	}
	return fmt.Sprintf(`<item id="%s" parentID="%s" restricted="1"><dc:title>%s</dc:title>%s<upnp:class>%s</upnp:class>%s%s</item>`, id, parent, title, date, class, art, res)
}
