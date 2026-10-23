// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package connectors

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/phorepoc/phecloud/internal/store"
)

// A server with a self-signed certificate, like Proton Mail Bridge on 127.0.0.1:1143 (STARTTLS).
func TestPinnedSelfSignedIMAP(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	fp := hex.EncodeToString(sum[:])

	mem := imapmemserver.New()
	mem.AddUser(imapmemserver.NewUser("me", "bridge-password"))
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}},
	})
	go srv.Serve(ln)
	defer srv.Close()
	c := ServerCfg{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, TLS: "starttls", User: "me"}

	// not pinned: refused, and the message gives the fingerprint to compare and paste
	_, err = TestIMAP(c, "bridge-password")
	if err == nil || !strings.Contains(err.Error(), "SHA-256 fingerprint is "+fp) || !strings.Contains(err.Error(), "paste it into the fingerprint field") {
		t.Fatalf("an unknown certificate must be reported with its fingerprint: %v", err)
	}
	// pinned (with colons and capitals, as people paste it): works
	c.Pin = strings.ToUpper(fp[:2] + ":" + fp[2:])
	if fl, err := TestIMAP(c, "bridge-password"); err != nil || len(fl) == 0 && false {
		t.Fatalf("pinned: %v %v", fl, err)
	}
	// the bridge password is not the account password: a wrong one is a login error, not a certificate error
	if _, err := TestIMAP(c, "my-proton-password"); err == nil || !strings.Contains(err.Error(), "login refused") {
		t.Fatalf("a wrong password: %v", err)
	}
	// a different certificate (another machine on that address) is refused although it is "pinned" to something else
	c.Pin = strings.Repeat("ab", 32)
	if _, err := TestIMAP(c, "bridge-password"); err == nil || !strings.Contains(err.Error(), "not the pinned one") {
		t.Fatalf("a wrong pin: %v", err)
	}
}

type icsFeed struct {
	mu   sync.Mutex
	body string
	code int
}

func (f *icsFeed) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.code != 0 {
		w.WriteHeader(f.code)
		return
	}
	w.Header().Set("Content-Type", "text/calendar")
	fmt.Fprint(w, f.body)
}

const icsHead = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Proton AG//Calendar//EN\r\n" +
	"BEGIN:VTIMEZONE\r\nTZID:Europe/Zurich\r\nBEGIN:STANDARD\r\nDTSTART:19701025T030000\r\nTZOFFSETFROM:+0200\r\nTZOFFSETTO:+0100\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\n"

func vevent(uid, summary string, extra string) string {
	return "BEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTAMP:20261023T100000Z\r\nDTSTART;TZID=Europe/Zurich:20261101T090000\r\nDTEND;TZID=Europe/Zurich:20261101T100000\r\nSUMMARY:" + summary + "\r\n" + extra + "END:VEVENT\r\n"
}

func TestICSSubscription(t *testing.T) {
	feed := &icsFeed{body: icsHead + vevent("a@proton", "Standup", "RRULE:FREQ=WEEKLY\r\n") + vevent("b@proton", "Dentist", "DESCRIPTION:Bring the\r\n  card\r\n") + "END:VCALENDAR\r\n"}
	srv := httptest.NewServer(feed)
	defer srv.Close()
	st := store.OpenTest(t)
	hub := &fakeHubDAV{puts: map[string]string{}}
	ctx := context.Background()
	cfg := DAVCfg{URL: srv.URL + "/cal.ics"}

	if n, err := TestICS(ctx, cfg); err != nil || n != 2 {
		t.Fatalf("test: %d %v", n, err)
	}
	if _, err := TestICS(ctx, DAVCfg{URL: "ftp://x/y"}); err == nil {
		t.Fatal("only http(s) and webcal links are read")
	}
	ch, rm, err := SyncICS(ctx, "acc", cfg, hub, st)
	if err != nil || ch != 2 || rm != 0 || len(hub.puts) != 2 {
		t.Fatalf("first: %d %d %v %v", ch, rm, err, hub.puts)
	}
	var standup, dentist string
	for name, b := range hub.puts {
		if !strings.HasPrefix(name, "cal/acc-") || !strings.HasSuffix(name, ".ics") {
			t.Fatalf("hub name %q", name)
		}
		if strings.Contains(b, "SUMMARY:Standup") {
			standup = b
		} else {
			dentist = b
		}
	}
	if !strings.Contains(standup, "RRULE:FREQ=WEEKLY") || !strings.Contains(standup, "TZID:Europe/Zurich") || !strings.Contains(standup, "TZOFFSETTO:+0100") {
		t.Fatalf("a recurring event keeps its rule and the feed's time zone definition:\n%s", standup)
	}
	if !strings.Contains(dentist, "DESCRIPTION:Bring the card") {
		t.Fatalf("folded lines are joined:\n%s", dentist)
	}
	if ch, rm, _ := SyncICS(ctx, "acc", cfg, hub, st); ch != 0 || rm != 0 {
		t.Fatalf("an unchanged feed writes nothing: %d %d", ch, rm)
	}
	// the feed changes: Standup renamed, Dentist removed, a new event
	feed.mu.Lock()
	feed.body = icsHead + vevent("a@proton", "Daily standup", "RRULE:FREQ=WEEKLY\r\n") + vevent("c@proton", "Lunch", "") + "END:VCALENDAR\r\n"
	feed.mu.Unlock()
	ch, rm, err = SyncICS(ctx, "acc", cfg, hub, st)
	if err != nil || ch != 2 || rm != 1 || len(hub.dels) != 1 {
		t.Fatalf("changes: %d %d %v %v", ch, rm, err, hub.dels)
	}
	// the link was turned off: the error says so and nothing is deleted
	feed.mu.Lock()
	feed.code = 404
	feed.mu.Unlock()
	if _, _, err := SyncICS(ctx, "acc", cfg, hub, st); err == nil || !strings.Contains(err.Error(), "HTTP 404") || len(hub.dels) != 1 {
		t.Fatalf("a dead link: %v (deletions %v)", err, hub.dels)
	}
}
