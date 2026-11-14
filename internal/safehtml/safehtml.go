// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package safehtml makes HTML from outside (mail bodies, feed posts) safe to show in the web app.
package safehtml

import (
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// policy: formatting and structure only. No images, no styles, no scripts, no forms, no frames, no remote loads of any kind;
// links stay as links (nothing is fetched until the person follows one) and open in a new context without a referrer.
var policy = func() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("p", "br", "div", "span", "b", "i", "u", "strong", "em", "ul", "ol", "li", "blockquote", "pre", "code", "hr",
		"h1", "h2", "h3", "h4", "h5", "h6", "table", "thead", "tbody", "tr", "td", "th", "sub", "sup", "small", "s")
	p.AllowAttrs("href").OnElements("a")
	p.AllowURLSchemes("http", "https", "mailto")
	p.RequireNoFollowOnLinks(true)
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	return p
}()

// Sanitize returns a safe fragment of s.
func Sanitize(s string) string { return strings.TrimSpace(policy.Sanitize(s)) }
