// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package tlsutil

import (
	"crypto/tls"
	"net"
	"testing"
)

func handshake(srv *tls.Config, cli *tls.Config) error {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	errs := make(chan error, 1)
	go func() { errs <- tls.Server(a, srv).Handshake() }()
	err := tls.Client(b, cli).Handshake()
	if serr := <-errs; err == nil {
		err = serr
	}
	return err
}

// The legacy certificate takes what the N9 (TLS 1.0, ECDHE-RSA AES-CBC) and Symbian (TLS 1.0, plain RSA AES-CBC) offer; the phone
// port's stays TLS 1.3 only. Both are made once and kept.
func TestLegacyTakesOldPhones(t *testing.T) {
	dir := t.TempDir()
	legacy, fp, err := LoadOrCreateLegacy(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, fp2, _ := LoadOrCreateLegacy(dir); fp2 != fp {
		t.Fatal("the legacy certificate was made again")
	}
	modern, _, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	for name, suite := range map[string]uint16{"N9": tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA, "Symbian": tls.TLS_RSA_WITH_AES_128_CBC_SHA} {
		old := &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS10, CipherSuites: []uint16{suite}}
		if err := handshake(legacy, old); err != nil {
			t.Fatalf("%s refused by the legacy certificate: %v", name, err)
		}
		if err := handshake(modern, old); err == nil {
			t.Fatalf("%s taken by the phone port", name)
		}
	}
	if err := handshake(legacy, &tls.Config{InsecureSkipVerify: true}); err != nil {
		t.Fatalf("a modern client: %v", err)
	}
}
