// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package davhub talks CardDAV (contacts) and CalDAV (calendar) to the personal hub (Stalwart) over HTTP. It reads and writes
// vCard 3.0 and iCalendar 2.0 with just the fields the web app edits and keeps every other line of an item as it was.
package davhub

import (
	"fmt"
	"strings"

	"github.com/phorepoc/phecloud/internal/proto"
)

// unfold joins RFC 5545/6350 folded lines and splits into content lines.
func unfold(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(out) > 0 {
			out[len(out)-1] += l[1:]
			continue
		}
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// splitProp splits "NAME;PARAM=x:value" into the upper-case name, the raw parameter string and the value.
func splitProp(l string) (name, params, value string) {
	i := strings.Index(l, ":")
	if i < 0 {
		return strings.ToUpper(l), "", ""
	}
	head, value := l[:i], l[i+1:]
	if j := strings.Index(head, ";"); j >= 0 {
		return strings.ToUpper(head[:j]), head[j+1:], value
	}
	return strings.ToUpper(head), "", value
}

func unescape(s string) string {
	r := strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`)
	return r.Replace(s)
}

func escape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "\n", `\n`, ",", `\,`, ";", `\;`)
	return r.Replace(s)
}

// typeOf extracts a TYPE=... parameter (vCard 3.0: TYPE=CELL,VOICE; 4.0: TYPE="cell"), lower-cased and without noise.
func typeOf(params string) string {
	var ts []string
	for _, p := range strings.Split(params, ";") {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			ts = append(ts, strings.ToLower(k)) // vCard 2.1 style bare type
			continue
		}
		if strings.EqualFold(k, "TYPE") {
			for _, t := range strings.Split(strings.Trim(v, `"`), ",") {
				t = strings.ToLower(strings.TrimSpace(t))
				if t != "" && t != "voice" && t != "internet" && t != "pref" {
					ts = append(ts, t)
				}
			}
		}
	}
	return strings.Join(ts, ",")
}

// parseVCard reads one vCard. extra holds every line the model does not cover, to be written back unchanged.
func parseVCard(data string) (c proto.Contact, uid string, extra []string) {
	for _, l := range unfold(data) {
		name, params, val := splitProp(l)
		switch name {
		case "BEGIN", "END", "VERSION", "PRODID":
		case "UID":
			uid = val
		case "FN":
			c.Name = unescape(val)
		case "N":
			p := strings.Split(val, ";")
			if len(p) > 0 {
				c.Family = unescape(p[0])
			}
			if len(p) > 1 {
				c.Given = unescape(p[1])
			}
		case "ORG":
			c.Org = unescape(strings.Split(val, ";")[0])
		case "NOTE":
			c.Note = unescape(val)
		case "TEL":
			c.Phones = append(c.Phones, proto.Labeled{Type: typeOf(params), Value: val})
		case "EMAIL":
			c.Emails = append(c.Emails, proto.Labeled{Type: typeOf(params), Value: val})
		default:
			extra = append(extra, l)
		}
	}
	if c.Name == "" { // some producers send only N
		c.Name = strings.TrimSpace(c.Given + " " + c.Family)
	}
	return c, uid, extra
}

func vcardType(t string, def string) string {
	if t == "" {
		return def
	}
	return strings.ToUpper(t)
}

func writeVCard(c proto.Contact, uid string, extra []string) string {
	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\r\n") }
	line("BEGIN:VCARD")
	line("VERSION:3.0")
	line("PRODID:-//phecloud//web//EN")
	line("UID:" + uid)
	fn := c.Name
	if fn == "" {
		fn = strings.TrimSpace(c.Given + " " + c.Family)
	}
	line("FN:" + escape(fn))
	line(fmt.Sprintf("N:%s;%s;;;", escape(c.Family), escape(c.Given)))
	if c.Org != "" {
		line("ORG:" + escape(c.Org))
	}
	for _, p := range c.Phones {
		if strings.TrimSpace(p.Value) != "" {
			line("TEL;TYPE=" + vcardType(p.Type, "VOICE") + ":" + p.Value)
		}
	}
	for _, e := range c.Emails {
		if strings.TrimSpace(e.Value) != "" {
			line("EMAIL;TYPE=" + vcardType(e.Type, "INTERNET") + ":" + e.Value)
		}
	}
	if c.Note != "" {
		line("NOTE:" + escape(c.Note))
	}
	for _, x := range extra {
		line(x)
	}
	line("END:VCARD")
	return b.String()
}
