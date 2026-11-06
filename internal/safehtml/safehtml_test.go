// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package safehtml

import (
	"strings"
	"testing"
)

func TestSanitize(t *testing.T) {
	got := Sanitize(`<p onclick="x()">Hi <a href="javascript:alert(1)">bad</a> <a href="https://example.org">ok</a><img src="https://t.example/p.gif"><script>alert(1)</script></p>`)
	for _, no := range []string{"onclick", "javascript:", "<img", "<script", "alert(1)"} {
		if strings.Contains(got, no) {
			t.Errorf("%q kept in %s", no, got)
		}
	}
	if !strings.Contains(got, `href="https://example.org"`) || !strings.Contains(got, `rel="nofollow noreferrer noopener"`) {
		t.Errorf("the safe link is lost: %s", got)
	}
}
