// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package dlna

import (
	"context"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/phorepoc/cloud-companion/internal/files"
)

type dirLib struct{ fs *files.Store }

func (d dirLib) List(_ context.Context, p string) ([]files.Entry, error) { return d.fs.List(p) }
func (d dirLib) Serve(w http.ResponseWriter, r *http.Request, p string) {
	fh, e, err := d.fs.Open(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer fh.Close()
	w.Header().Set("Content-Type", e.Type)
	http.ServeContent(w, r, e.Name, e.Mod, fh)
}

func call(t *testing.T, url, service, action, args string) (int, string) {
	t.Helper()
	body := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:` + action +
		` xmlns:u="urn:schemas-upnp-org:service:` + service + `:1">` + args + `</u:` + action + `></s:Body></s:Envelope>`
	r, _ := http.NewRequest("POST", url, strings.NewReader(body))
	r.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:`+service+`:1#`+action+`"`)
	r.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func browse(id, flag string, start, n int) string {
	return "<ObjectID>" + id + "</ObjectID><BrowseFlag>" + flag + "</BrowseFlag><Filter>*</Filter><StartingIndex>" +
		itoa(start) + "</StartingIndex><RequestedCount>" + itoa(n) + "</RequestedCount><SortCriteria></SortCriteria>"
}

func itoa(n int) string { return strconv.Itoa(n) }

// Description, Browse (children in pages, metadata), playing a song with a range, other files never shared, and discovery.
func TestMediaServer(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "Music", "Album"), 0o755)
	os.WriteFile(filepath.Join(dir, "Music", "Album", "01 One & Two.mp3"), []byte("ID3-0123456789"), 0o644)
	os.WriteFile(filepath.Join(dir, "Music", "Album", "02 Three.flac"), []byte("fLaC"), 0o644)
	os.WriteFile(filepath.Join(dir, "Music", "Album", "cover.jpg"), []byte("JPEG"), 0o644)
	os.WriteFile(filepath.Join(dir, "Music", "Album", "lyrics.txt"), []byte("words"), 0o644)
	os.WriteFile(filepath.Join(dir, "taxes.pdf"), []byte("%PDF"), 0o644)
	s := New("Test Media", dirLib{files.New(dir)}, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	res, _ := http.Get(srv.URL + "/desc.xml")
	b, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(b), "<friendlyName>Test Media</friendlyName>") || !strings.Contains(string(b), "MediaServer:1") || !strings.Contains(string(b), s.uuid) {
		t.Fatalf("the description: %s", b)
	}
	for _, p := range []string{"/cd.xml", "/cm.xml"} {
		if res, _ := http.Get(srv.URL + p); res.StatusCode != 200 {
			t.Fatalf("%s: %d", p, res.StatusCode)
		}
	}

	_, top := call(t, srv.URL+"/ctl/cd", "ContentDirectory", "Browse", browse("0", "BrowseDirectChildren", 0, 0))
	if !strings.Contains(top, "&gt;Music&lt;/dc:title&gt;") || strings.Contains(top, "taxes") || !strings.Contains(top, "<TotalMatches>1</TotalMatches>") {
		t.Fatalf("the top: %s", top)
	}
	album := idOf("Music/Album")
	_, l := call(t, srv.URL+"/ctl/cd", "ContentDirectory", "Browse", browse(album, "BrowseDirectChildren", 0, 2))
	if !strings.Contains(l, "<NumberReturned>2</NumberReturned>") || !strings.Contains(l, "<TotalMatches>3</TotalMatches>") ||
		!strings.Contains(l, "01 One &amp;amp; Two.mp3") || !strings.Contains(l, "object.item.audioItem.musicTrack") || strings.Contains(l, "lyrics") {
		t.Fatalf("the album, first page: %s", l)
	}
	_, l2 := call(t, srv.URL+"/ctl/cd", "ContentDirectory", "Browse", browse(album, "BrowseDirectChildren", 2, 2))
	if !strings.Contains(l2, "<NumberReturned>1</NumberReturned>") || !strings.Contains(l2, "cover.jpg") || !strings.Contains(l2, "imageItem.photo") {
		t.Fatalf("the album, second page: %s", l2)
	}
	_, meta := call(t, srv.URL+"/ctl/cd", "ContentDirectory", "Browse", browse(idOf("Music/Album/02 Three.flac"), "BrowseMetadata", 0, 0))
	if !strings.Contains(meta, "02 Three.flac") || !strings.Contains(meta, "parentID=&#34;"+album) {
		t.Fatalf("one song's metadata: %s", meta)
	}
	if code, _ := call(t, srv.URL+"/ctl/cd", "ContentDirectory", "Browse", browse("pnope!", "BrowseDirectChildren", 0, 0)); code != 500 {
		t.Fatalf("a bad ID: %d", code)
	}

	m := regexp.MustCompile(`http://[^<]+?\.mp3`).FindString(html.UnescapeString(html.UnescapeString(l)))
	if m == "" {
		t.Fatalf("no address for the song in %s", l)
	}
	r, _ := http.NewRequest("GET", m, nil)
	r.Header.Set("Range", "bytes=3-6")
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(res.Body)
	if res.StatusCode != 206 || string(b) != "-012" || res.Header.Get("Content-Type") != "audio/mpeg" || res.Header.Get("transferMode.dlna.org") != "Streaming" {
		t.Fatalf("playing the song: %d %q %v", res.StatusCode, b, res.Header)
	}
	for _, p := range []string{"taxes.pdf", "Music/Album/lyrics.txt", "../../etc/passwd"} {
		if res, _ := http.Get(srv.URL + "/media/" + idOf(p) + "/x"); res.StatusCode != 404 {
			t.Fatalf("%s was served: %d", p, res.StatusCode)
		}
	}
	if res, _ := http.Post(srv.URL+"/media/"+idOf("Music/Album/02 Three.flac")+"/x", "audio/flac", strings.NewReader("x")); res.StatusCode != 405 {
		t.Fatal("a POST to a song")
	}
	if code, body := call(t, srv.URL+"/ctl/cm", "ConnectionManager", "GetProtocolInfo", ""); code != 200 || !strings.Contains(body, "http-get:*:audio/*:*") {
		t.Fatalf("GetProtocolInfo: %d %s", code, body)
	}

	s.port = 8200
	ip := net.IPv4(192, 168, 1, 5)
	search := func(st string) [][]byte {
		return s.answer([]byte("M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 1\r\nST: "+st+"\r\n\r\n"), ip)
	}
	if a := search("ssdp:all"); len(a) != 5 || !strings.Contains(string(a[0]), "LOCATION: http://192.168.1.5:8200/desc.xml") {
		t.Fatalf("ssdp:all: %q", a)
	}
	if a := search("urn:schemas-upnp-org:device:MediaServer:1"); len(a) != 1 || !strings.Contains(string(a[0]), "USN: "+s.uuid+"::urn:schemas-upnp-org:device:MediaServer:1") {
		t.Fatalf("a media server search: %q", a)
	}
	if a := search("urn:schemas-upnp-org:device:MediaRenderer:1"); len(a) != 0 {
		t.Fatal("answered a search for something else")
	}
	if a := s.answer([]byte("NOTIFY * HTTP/1.1\r\nHOST: x\r\n\r\n"), ip); a != nil {
		t.Fatal("answered a NOTIFY")
	}
}
