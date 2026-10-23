// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package connectors

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

// Vault seals the accounts' passwords and tokens (AES-256-GCM) before they are stored. The key is PHECLOUD_SECRET_KEY (64 hex digits)
// when set, else a random key kept in <dir>/connectors.key (mode 0600, next to the database: back both up together, see docs/hub.md).
type Vault struct{ aead cipher.AEAD }

func OpenVault(dir string) (*Vault, error) {
	var key []byte
	if h := os.Getenv("PHECLOUD_SECRET_KEY"); h != "" {
		k, err := hex.DecodeString(h)
		if err != nil || len(k) != 32 {
			return nil, errors.New("PHECLOUD_SECRET_KEY must be 64 hex digits")
		}
		key = k
	} else {
		p := filepath.Join(dir, "connectors.key")
		b, err := os.ReadFile(p)
		if err != nil {
			key = make([]byte, 32)
			rand.Read(key)
			if err := os.WriteFile(p, []byte(hex.EncodeToString(key)), 0o600); err != nil {
				return nil, err
			}
		} else if key, err = hex.DecodeString(string(b)); err != nil || len(key) != 32 {
			return nil, errors.New("connectors.key is damaged")
		}
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	return &Vault{g}, nil
}

func (v *Vault) Seal(plain []byte) []byte {
	n := make([]byte, v.aead.NonceSize())
	rand.Read(n)
	return v.aead.Seal(n, n, plain, nil)
}

func (v *Vault) Open(b []byte) ([]byte, error) {
	if len(b) < v.aead.NonceSize() {
		return nil, errors.New("sealed data too short")
	}
	return v.aead.Open(nil, b[:v.aead.NonceSize()], b[v.aead.NonceSize():], nil)
}
