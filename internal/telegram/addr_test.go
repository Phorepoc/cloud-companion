// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package telegram

import "testing"

func TestParse(t *testing.T) {
	for in, want := range map[string]target{"bob_b": {kind: 'n', s: "bob_b"}, "@bob_b": {kind: 'n', s: "bob_b"}, "u123": {kind: 'u', id: 123},
		"g45": {kind: 'g', id: 45}, "c678": {kind: 'c', id: 678}, "+306912345678": {kind: 'p', s: "+306912345678"}, "uber_fan": {kind: 'n', s: "uber_fan"}} {
		if got, ok := parse(in); !ok || got != want {
			t.Fatalf("%q read as %+v (%v), want %+v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "a b", "abc", "+30"} {
		if _, ok := parse(bad); ok {
			t.Fatalf("%q taken", bad)
		}
	}
}
