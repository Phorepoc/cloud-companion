// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package connectors

import (
	"errors"
	"path"
	"sort"
	"strings"
	"unicode"

	"github.com/phorepoc/phecloud/internal/proto"
)

// HubID is the id of a hub contact: the name of its resource without the extension.
func HubID(href string) string { return strings.TrimSuffix(path.Base(href), ".vcf") }

// Suggestion proposes that two hub contacts are the same person.
type Suggestion struct {
	A      string `json:"a"` // hub ids
	B      string `json:"b"`
	NameA  string `json:"name_a"`
	NameB  string `json:"name_b"`
	Reason string `json:"reason"` // "same phone number 0171 ...", "same email x@y", "same name"
}

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// phoneKey compares numbers by their last 9 digits (so +49 171 ... and 0171 ... match); shorter numbers must be equal.
func phoneKey(s string) string {
	d := digits(s)
	if len(d) < 6 {
		return ""
	}
	if len(d) > 9 {
		d = d[len(d)-9:]
	}
	return d
}

func nameKey(c proto.Contact) string {
	n := strings.ToLower(strings.TrimSpace(c.Name))
	if n == "" {
		n = strings.ToLower(strings.TrimSpace(c.Given + " " + c.Family))
	}
	parts := strings.FieldsFunc(n, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	sort.Strings(parts) // "Müller, Anna" == "Anna Müller"
	k := strings.Join(parts, " ")
	if len(k) < 4 {
		return ""
	}
	return k
}

// Duplicates finds likely duplicates among contacts: same phone number, same e-mail address, or the same name (words in any order).
// A pair is reported once, with the strongest reason.
func Duplicates(cs []proto.Contact) []Suggestion {
	type ent struct{ i int }
	idx := map[string][]int{}
	reason := map[string]string{}
	add := func(kind, key, label string, i int) {
		if key == "" {
			return
		}
		k := kind + "|" + key
		idx[k] = append(idx[k], i)
		reason[k] = label
	}
	for i, c := range cs {
		for _, p := range c.Phones {
			add("p", phoneKey(p.Value), "same phone number "+strings.TrimSpace(p.Value), i)
		}
		for _, e := range c.Emails {
			em := strings.ToLower(strings.TrimSpace(e.Value))
			add("e", em, "same email address "+em, i)
		}
		add("n", nameKey(c), "same name", i)
	}
	seen := map[[2]int]bool{}
	var out []Suggestion
	for _, kind := range []string{"p", "e", "n"} { // strongest reason first
		keys := make([]string, 0, len(idx))
		for k := range idx {
			if strings.HasPrefix(k, kind+"|") {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			l := idx[k]
			for x := 0; x < len(l); x++ {
				for y := x + 1; y < len(l); y++ {
					a, b := l[x], l[y]
					if a == b || seen[[2]int{a, b}] {
						continue
					}
					seen[[2]int{a, b}] = true
					out = append(out, Suggestion{A: HubID(cs[a].Href), B: HubID(cs[b].Href), NameA: cs[a].Name, NameB: cs[b].Name, Reason: reason[k]})
				}
			}
		}
	}
	return out
}

// Merge folds b into a: a keeps its name; phones and emails are united (no repeats), empty fields are filled from b, notes are joined.
func Merge(a, b proto.Contact) proto.Contact {
	if a.Name == "" {
		a.Name = b.Name
	}
	if a.Given == "" {
		a.Given = b.Given
	}
	if a.Family == "" {
		a.Family = b.Family
	}
	if a.Org == "" {
		a.Org = b.Org
	}
	if b.Note != "" && !strings.Contains(a.Note, b.Note) {
		if a.Note != "" {
			a.Note += "\n"
		}
		a.Note += b.Note
	}
	have := map[string]bool{}
	for _, p := range a.Phones {
		have[phoneKey(p.Value)+"|"+strings.TrimSpace(p.Value)] = true
	}
	for _, p := range b.Phones {
		k := phoneKey(p.Value)
		dup := false
		for _, q := range a.Phones {
			if k != "" && phoneKey(q.Value) == k || strings.TrimSpace(q.Value) == strings.TrimSpace(p.Value) {
				dup = true
			}
		}
		if !dup {
			a.Phones = append(a.Phones, p)
		}
	}
	for _, e := range b.Emails {
		dup := false
		for _, q := range a.Emails {
			if strings.EqualFold(strings.TrimSpace(q.Value), strings.TrimSpace(e.Value)) {
				dup = true
			}
		}
		if !dup {
			a.Emails = append(a.Emails, e)
		}
	}
	return a
}

// HubContacts is the hub side of a merge (davhub.Client).
type HubContacts interface {
	Contacts() ([]proto.Contact, error)
	SaveContact(proto.Contact) (proto.Contact, error)
	DeleteRaw(kind, name string) error
}

// LinkMover re-points links (store.Store).
type LinkMover interface{ MoveLinks(from, to string) }

var ErrGone = errors.New("one of the contacts no longer exists")

// MergeHub folds the hub contact `drop` into `keep`. Only the HUB's copy is touched: the data goes into `keep`, `drop` is deleted from the
// hub, and every link of `drop` (device or service entry) is re-pointed to `keep`. The entries in the sources are never written or deleted:
// every device and service keeps its own list (hub-design.md); a later sync does not bring `drop` back.
func MergeHub(hub HubContacts, links LinkMover, keep, drop string) (proto.Contact, error) {
	cs, err := hub.Contacts()
	if err != nil {
		return proto.Contact{}, err
	}
	var a, b *proto.Contact
	for i := range cs {
		switch HubID(cs[i].Href) {
		case keep:
			a = &cs[i]
		case drop:
			b = &cs[i]
		}
	}
	if a == nil || b == nil || keep == drop {
		return proto.Contact{}, ErrGone
	}
	merged := Merge(*a, *b)
	if _, err := hub.SaveContact(merged); err != nil {
		return merged, err
	}
	if err := hub.DeleteRaw("card", path.Base(b.Href)); err != nil {
		return merged, err
	}
	links.MoveLinks(drop, keep)
	return merged, nil
}
