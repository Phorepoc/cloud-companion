// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package tlsutil makes the server's self-signed certificate (once, at first start) and its pin fingerprint.
package tlsutil

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// Fingerprint is the SHA-256 of the DER certificate, hex.
func Fingerprint(der []byte) string {
	h := sha256.Sum256(der)
	return hex.EncodeToString(h[:])
}

// LoadOrCreate returns the TLS 1.3 server config using dir/tls.crt and dir/tls.key, creating a self-signed
// ECDSA P-256 certificate (10 years) on first use. The second result is the pin fingerprint.
func LoadOrCreate(dir string) (*tls.Config, string, error) {
	crt, key := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	if _, err := os.Stat(crt); err != nil {
		if err := create(crt, key); err != nil {
			return nil, "", err
		}
	}
	cert, err := tls.LoadX509KeyPair(crt, key)
	if err != nil {
		return nil, "", err
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}, Fingerprint(cert.Certificate[0]), nil
}

// LoadOrCreateLegacy is the config for the fronts that old phones use (the XMPP front: the Nokia N9, Symbian), using dir/legacy.crt
// and dir/legacy.key, a self-signed RSA 2048 certificate made on first use: those phones know neither TLS 1.3 nor ECDSA. It
// accepts TLS 1.0 and up, with the old phones' AES-CBC suites after the modern ones; the phone port keeps LoadOrCreate's TLS 1.3.
func LoadOrCreateLegacy(dir string) (*tls.Config, string, error) {
	crt, key := filepath.Join(dir, "legacy.crt"), filepath.Join(dir, "legacy.key")
	if _, err := os.Stat(crt); err != nil {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, "", err
		}
		if err := write(crt, key, k, &k.PublicKey, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(k), x509.KeyUsageDigitalSignature|x509.KeyUsageKeyEncipherment); err != nil {
			return nil, "", err
		}
	}
	cert, err := tls.LoadX509KeyPair(crt, key)
	if err != nil {
		return nil, "", err
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS10, CipherSuites: []uint16{
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384, tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA, // the N9 (OpenSSL 1.0, TLS 1.0)
		tls.TLS_RSA_WITH_AES_128_CBC_SHA, tls.TLS_RSA_WITH_AES_256_CBC_SHA, // Symbian (no ECDHE)
	}}, Fingerprint(cert.Certificate[0]), nil
}

// FingerprintFile reads the fingerprint of dir/tls.crt without a key.
func FingerprintFile(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "tls.crt"))
	if err != nil {
		return "", err
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return "", os.ErrInvalid
	}
	return Fingerprint(blk.Bytes), nil
}

func create(crt, key string) error {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	kb, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		return err
	}
	return write(crt, key, k, &k.PublicKey, "EC PRIVATE KEY", kb, x509.KeyUsageDigitalSignature)
}

func write(crt, key string, k crypto.Signer, pub any, keyType string, kb []byte, usage x509.KeyUsage) error {
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "phecloud"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     usage,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"phecloud"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, pub, k)
	if err != nil {
		return err
	}
	if err := os.WriteFile(crt, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return err
	}
	return os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: keyType, Bytes: kb}), 0o600)
}
