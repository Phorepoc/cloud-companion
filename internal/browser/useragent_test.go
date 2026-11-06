// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package browser

import "testing"

func TestUserAgent(t *testing.T) {
	for _, c := range []struct {
		w, h int
		site string
		want string
	}{
		{360, 0, "", mobileUA},     // the phone layout
		{1440, 0, "", mobileUA},    // phone layout asked for by width alone
		{480, 800, "", mobileUA},   // a narrow window
		{1440, 798, "", desktopUA}, // a desktop window: no touch pages (Google's full-screen search)
		{600, 700, "", desktopUA},
		{1440, 798, "mobile", mobileUA}, // the person asked for the mobile site
		{360, 0, "desktop", desktopUA},
	} {
		if got := userAgent(c.w, c.h, c.site); got != c.want {
			t.Errorf("userAgent(%d, %d, %q) = %q, want %q", c.w, c.h, c.site, got, c.want)
		}
	}
}
