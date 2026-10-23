// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package server

import "testing"

func TestAuthFailsBlockAfterTen(t *testing.T) {
	var f authFails
	for i := 0; i < authFailLimit-1; i++ {
		f.add("203.0.113.9:5000" + string(rune('0'+i)))
	}
	if f.blocked("203.0.113.9:1") {
		t.Fatal("nine failures must not block")
	}
	f.add("203.0.113.9:777")
	if !f.blocked("203.0.113.9:2") {
		t.Fatal("ten failures from one host (any port) must block it")
	}
	if f.blocked("203.0.113.10:1") {
		t.Fatal("another host is not blocked")
	}
}
