// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package connectors

// Merging two duplicate contacts that live on the SAME device or service (a CardDAV account): the one case where the source's own list
// is changed, so it is explicit. Preview shows exactly what will change on that device (which entry is rewritten, which is deleted, the
// resulting fields); Apply writes it through the connector (PUT the merged card over entry A, DELETE entry B, both guarded by their ETags
// so a change made meanwhile on the device is never overwritten); Undo (10 minutes) puts the two original cards back.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/phorepoc/cloud-companion/internal/davhub"
	"github.com/phorepoc/cloud-companion/internal/proto"
	"github.com/phorepoc/cloud-companion/internal/store"
)

const UndoWindow = 10 * time.Minute

// DevMergeStore is the part of store.Store used here.
type DevMergeStore interface {
	DevMergeSave(m store.DevMerge) store.DevMerge
	DevMergeGet(id string) (store.DevMerge, bool)
}

// DevMergePreview is what the page shows before the user confirms.
type DevMergePreview struct {
	ID      string        `json:"id"`
	Account string        `json:"account"`
	Keep    proto.Contact `json:"keep"`    // the card as it is now (entry A)
	Drop    proto.Contact `json:"drop"`    // the card that will be deleted (entry B)
	Merged  proto.Contact `json:"merged"`  // what entry A will contain
	Changes []string      `json:"changes"` // plain sentences: what happens on the device
}

func davURL(c DAVCfg, href string) string {
	if strings.HasPrefix(href, "http") {
		return href
	}
	b, _ := url.Parse(c.URL)
	return b.Scheme + "://" + b.Host + href
}

func fetchCard(ctx context.Context, c DAVCfg, pass, href string) (body, etag string, err error) {
	code, b, hdr, err := davDoH(ctx, c, pass, "GET", davURL(c, href), "", nil)
	if err != nil {
		return "", "", err
	}
	if code != 200 {
		return "", "", fmt.Errorf("the device answered HTTP %d for %s", code, href)
	}
	return string(b), strings.TrimSpace(hdr.Get("ETag")), nil
}

// PreviewDeviceMerge reads both cards from the device and prepares the merge; nothing is written.
func PreviewDeviceMerge(ctx context.Context, st DevMergeStore, account string, c DAVCfg, pass, hrefA, hrefB string) (DevMergePreview, error) {
	if hrefA == hrefB {
		return DevMergePreview{}, errors.New("choose two different entries")
	}
	ba, ea, err := fetchCard(ctx, c, pass, hrefA)
	if err != nil {
		return DevMergePreview{}, err
	}
	bb, eb, err := fetchCard(ctx, c, pass, hrefB)
	if err != nil {
		return DevMergePreview{}, err
	}
	ca, uidA, extraA := davhub.ParseVCard(ba)
	cb, _, _ := davhub.ParseVCard(bb)
	merged := Merge(ca, cb)
	mbody := davhub.WriteVCard(merged, uidA, extraA)
	m := st.DevMergeSave(store.DevMerge{Account: account, AHref: hrefA, ABody: ba, AETag: ea, BHref: hrefB, BBody: bb, BETag: eb, MergedBody: mbody, State: "preview", Expires: time.Now().Add(UndoWindow).Unix()})
	p := DevMergePreview{ID: m.ID, Account: account, Keep: ca, Drop: cb, Merged: merged}
	p.Changes = append(p.Changes, fmt.Sprintf("On the device, %q is rewritten with the combined details.", ca.Name))
	for _, ph := range merged.Phones[len(ca.Phones):] {
		p.Changes = append(p.Changes, "It gains the phone number "+strings.TrimSpace(ph.Value)+".")
	}
	for _, em := range merged.Emails[len(ca.Emails):] {
		p.Changes = append(p.Changes, "It gains the email address "+strings.TrimSpace(em.Value)+".")
	}
	if merged.Note != ca.Note {
		p.Changes = append(p.Changes, "Its note is extended with the other entry's note.")
	}
	p.Changes = append(p.Changes, fmt.Sprintf("On the device, %q is deleted.", cb.Name), "You can undo this for 10 minutes.")
	return p, nil
}

// ApplyDeviceMerge writes a previewed merge to the device.
func ApplyDeviceMerge(ctx context.Context, st DevMergeStore, id string, c DAVCfg, pass string) error {
	m, ok := st.DevMergeGet(id)
	if !ok || m.State != "preview" || time.Now().Unix() > m.Expires {
		return errors.New("this merge is no longer available; preview it again")
	}
	code, _, hdr, err := davDoH(ctx, c, pass, "PUT", davURL(c, m.AHref), m.MergedBody, map[string]string{"Content-Type": "text/vcard; charset=utf-8", "If-Match": m.AETag})
	if err != nil {
		return err
	}
	if code == 412 {
		return errors.New("the entry was changed on the device in the meantime; preview again")
	}
	if code/100 != 2 {
		return fmt.Errorf("the device refused the change (HTTP %d)", code)
	}
	m.MergedETag = strings.TrimSpace(hdr.Get("ETag"))
	code, _, _, err = davDoH(ctx, c, pass, "DELETE", davURL(c, m.BHref), "", map[string]string{"If-Match": m.BETag})
	if err != nil || code/100 != 2 {
		// put A back: a half-done merge would lose nothing but must not stay half-done
		davDoH(ctx, c, pass, "PUT", davURL(c, m.AHref), m.ABody, map[string]string{"Content-Type": "text/vcard; charset=utf-8"})
		if err == nil {
			err = fmt.Errorf("the device refused to delete the second entry (HTTP %d); nothing was changed", code)
		}
		return err
	}
	m.State, m.Expires = "applied", time.Now().Add(UndoWindow).Unix()
	st.DevMergeSave(m)
	return nil
}

// UndoDeviceMerge puts the two original cards back (within the undo window).
func UndoDeviceMerge(ctx context.Context, st DevMergeStore, id string, c DAVCfg, pass string) error {
	m, ok := st.DevMergeGet(id)
	if !ok || m.State != "applied" || time.Now().Unix() > m.Expires {
		return errors.New("the undo window has passed")
	}
	hdr := map[string]string{"Content-Type": "text/vcard; charset=utf-8"}
	h1 := map[string]string{"Content-Type": hdr["Content-Type"]}
	if m.MergedETag != "" {
		h1["If-Match"] = m.MergedETag
	}
	if code, _, _, err := davDoH(ctx, c, pass, "PUT", davURL(c, m.AHref), m.ABody, h1); err != nil || code/100 != 2 {
		return fmt.Errorf("could not restore the first entry (the device may have changed it since)")
	}
	if code, _, _, err := davDoH(ctx, c, pass, "PUT", davURL(c, m.BHref), m.BBody, map[string]string{"Content-Type": hdr["Content-Type"], "If-None-Match": "*"}); err != nil || code/100 != 2 {
		return fmt.Errorf("restored the first entry but could not re-create the second (HTTP %d)", code)
	}
	m.State = "undone"
	st.DevMergeSave(m)
	return nil
}
