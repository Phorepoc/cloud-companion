// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

import (
	"testing"
	"time"
)

func TestBrowserTicket(t *testing.T) {
	now := time.Unix(1000, 0)
	tk := browserTicket("s", now.Add(time.Minute))
	if !browserTicketOK("s", tk, now) {
		t.Fatal("a fresh ticket must pass")
	}
	if browserTicketOK("s", tk, now.Add(2*time.Minute)) {
		t.Fatal("an old ticket must fail")
	}
	if browserTicketOK("other", tk, now) || browserTicketOK("s", "junk", now) || browserTicketOK("s", "2000.00", now) {
		t.Fatal("a wrong ticket must fail")
	}
}
