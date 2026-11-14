// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package webui

import (
	"context"
	"net/http"

	"github.com/phorepoc/cloud-companion/internal/files"
)

// dlnaLib is what the DLNA media server shares (internal/dlna, PHECLOUD_DLNA_LISTEN): the Files section with the folders of the
// other services at the top; the media server itself keeps to music, videos and pictures.
type dlnaLib struct{ f *filesAPI }

func (d dlnaLib) List(ctx context.Context, p string) ([]files.Entry, error) {
	l, err := d.f.list(ctx, p)
	if err != nil || p != "" {
		return l, err
	}
	for _, a := range d.f.mounts() {
		l = append(l, files.Entry{Name: a.Name, Path: "~" + a.ID, Dir: true})
	}
	return l, nil
}

func (d dlnaLib) Serve(w http.ResponseWriter, r *http.Request, p string) {
	if id, rest, ok := remotePath(p); ok {
		src, err := d.f.source(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		src.Serve(w, r, rest)
		return
	}
	serveFile(w, r, d.f.fs, p, false)
}
