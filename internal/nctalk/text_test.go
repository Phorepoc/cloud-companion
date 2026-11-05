// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package nctalk

import "testing"

// Talk sends mentions and files as placeholders; they reach a phone written out.
func TestText(t *testing.T) {
	m := message{Message: "{mention-user1} see {file} {unknown}", RawParameters: []byte(`{"mention-user1":{"type":"user","id":"bob","name":"Bob B"},"file":{"type":"file","id":"7","name":"plan.pdf"}}`)}
	if got := m.text(); got != "@Bob B see [file: plan.pdf] {unknown}" {
		t.Fatal(got)
	}
	if got := (message{Message: "plain {x}", RawParameters: []byte(`[]`)}).text(); got != "plain {x}" {
		t.Fatal(got)
	}
}
