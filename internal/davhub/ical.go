// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package davhub

import (
	"strings"
	"time"

	"github.com/phorepoc/phecloud/internal/proto"
)

// ical is a parsed calendar object: the first VEVENT as an Event, plus what must be written back around and inside it.
type ical struct {
	ev       proto.Event
	uid      string
	head     []string // lines before the VEVENT (VCALENDAR header, VTIMEZONE blocks), verbatim
	tail     []string // lines after it (END:VCALENDAR, other components), verbatim
	extra    []string // VEVENT lines the model does not cover (RRULE, VALARM blocks, ...), verbatim
	startTZ  string   // the TZID parameter of the original DTSTART/DTEND, kept on edit
	endTZ    string
	hasEvent bool
}

// isoFromICal converts 20261024T090000Z / 20261024T090000 / 20261024 to ISO 8601.
func isoFromICal(v string) (iso string, allDay bool) {
	v = strings.TrimSpace(v)
	switch {
	case len(v) == 8:
		return v[0:4] + "-" + v[4:6] + "-" + v[6:8], true
	case len(v) >= 15:
		s := v[0:4] + "-" + v[4:6] + "-" + v[6:8] + "T" + v[9:11] + ":" + v[11:13] + ":" + v[13:15]
		if strings.HasSuffix(v, "Z") {
			s += "Z"
		}
		return s, false
	}
	return v, false
}

// icalFromISO is the inverse (floating local time unless the value ends with Z).
func icalFromISO(s string, allDay bool) string {
	s = strings.TrimSpace(s)
	if allDay || len(s) == 10 {
		return strings.ReplaceAll(s[:min(10, len(s))], "-", "")
	}
	z := strings.HasSuffix(s, "Z")
	s = strings.TrimSuffix(s, "Z")
	s = strings.ReplaceAll(strings.ReplaceAll(s, "-", ""), ":", "")
	if len(s) == 13 { // 20261024T0900
		s += "00"
	}
	if z {
		s += "Z"
	}
	return s
}

func tzParam(params string) string {
	for _, p := range strings.Split(params, ";") {
		if k, v, ok := strings.Cut(p, "="); ok && strings.EqualFold(k, "TZID") {
			return v
		}
	}
	return ""
}

func parseICal(data string) ical {
	var o ical
	lines := unfold(data)
	state := 0 // 0 before VEVENT, 1 inside, 2 after
	depth := 0
	for _, l := range lines {
		name, params, val := splitProp(l)
		switch state {
		case 0:
			if name == "BEGIN" && strings.EqualFold(val, "VEVENT") {
				state, o.hasEvent = 1, true
				continue
			}
			o.head = append(o.head, l)
		case 1:
			if depth > 0 { // inside a nested block (VALARM): kept verbatim
				o.extra = append(o.extra, l)
				if name == "END" {
					depth--
				}
				continue
			}
			switch name {
			case "END":
				state = 2
				continue
			case "BEGIN":
				depth++
				o.extra = append(o.extra, l)
			case "UID":
				o.uid = val
			case "SUMMARY":
				o.ev.Title = unescape(val)
			case "LOCATION":
				o.ev.Location = unescape(val)
			case "DESCRIPTION":
				o.ev.Notes = unescape(val)
			case "DTSTART":
				o.ev.Start, o.ev.AllDay = isoFromICal(val)
				o.startTZ = tzParam(params)
			case "DTEND":
				o.ev.End, _ = isoFromICal(val)
				o.endTZ = tzParam(params)
			case "DTSTAMP", "LAST-MODIFIED":
			case "RRULE", "RDATE":
				o.ev.Recurs = true
				o.extra = append(o.extra, l)
			default:
				o.extra = append(o.extra, l)
			}
		default:
			o.tail = append(o.tail, l)
		}
	}
	return o
}

func writeICal(o ical, e proto.Event) string {
	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\r\n") }
	if len(o.head) == 0 {
		o.head = []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//phecloud//web//EN"}
		o.tail = []string{"END:VCALENDAR"}
	}
	for _, l := range o.head {
		line(l)
	}
	line("BEGIN:VEVENT")
	line("UID:" + o.uid)
	line("DTSTAMP:" + time.Now().UTC().Format("20060102T150405Z"))
	prop := func(name, tz string) string {
		if e.AllDay {
			return name + ";VALUE=DATE"
		}
		if tz != "" {
			return name + ";TZID=" + tz
		}
		return name
	}
	line(prop("DTSTART", o.startTZ) + ":" + icalFromISO(e.Start, e.AllDay))
	if e.End != "" {
		line(prop("DTEND", o.endTZ) + ":" + icalFromISO(e.End, e.AllDay))
	}
	line("SUMMARY:" + escape(e.Title))
	if e.Location != "" {
		line("LOCATION:" + escape(e.Location))
	}
	if e.Notes != "" {
		line("DESCRIPTION:" + escape(e.Notes))
	}
	for _, x := range o.extra {
		line(x)
	}
	line("END:VEVENT")
	for _, l := range o.tail {
		line(l)
	}
	return b.String()
}
