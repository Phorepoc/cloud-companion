// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

func TestOwnerRecovery(t *testing.T) {
	st := OpenTest(t)
	if st.OwnerExists() {
		t.Fatal("owner before any passkey")
	}
	admin, _ := st.NewPairCode(time.Minute, true)
	plain, _ := st.NewPairCode(time.Minute, false)
	if st.CheckAdminCode(plain) || !st.CheckAdminCode(admin) {
		t.Fatal("only an admin code may register a passkey")
	}
	if err := st.ConsumeAdminCode(admin); err != nil {
		t.Fatal(err)
	}
	if st.ConsumeAdminCode(admin) == nil {
		t.Fatal("an admin code works once")
	}
	h := NewHandle()
	rec, err := st.AddPasskey(h, &webauthn.Credential{ID: []byte("c1")}, "k1")
	if err != nil || rec == "" {
		t.Fatalf("first passkey creates the owner and returns a recovery code: %q %v", rec, err)
	}
	if rec2, err := st.AddPasskey(h, &webauthn.Credential{ID: []byte("c2")}, "k2"); err != nil || rec2 != "" {
		t.Fatalf("second passkey: %q %v", rec2, err)
	}
	if !st.OwnerExists() || len(st.Passkeys()) != 2 {
		t.Fatal("owner and two passkeys")
	}
	if _, err := st.UseRecoveryCode("AAAA-BBBB-CCCC-DDDD"); err == nil {
		t.Fatal("a wrong recovery code was accepted")
	}
	n, err := st.UseRecoveryCode(rec[:4] + " " + rec[5:9] + "-" + lower(rec[10:])) // spaces, case and dashes do not matter
	if err != nil || n == "" || n == rec {
		t.Fatalf("recovery: %q %v", n, err)
	}
	if _, err := st.UseRecoveryCode(rec); err == nil {
		t.Fatal("a recovery code works once")
	}
	if _, err := st.UseRecoveryCode(n); err != nil {
		t.Fatal("the replacement code works", err)
	}
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
