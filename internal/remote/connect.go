// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Cfg is a files account's configuration (Connections): kind "webdav" or "seafile". The password, app password or token is the
// account's secret.
type Cfg struct {
	URL       string `json:"url"`
	User      string `json:"user"`
	Nextcloud bool   `json:"nextcloud,omitempty"` // a Nextcloud server: URL is its address, and its WebDAV folder is found
	Insecure  bool   `json:"insecure,omitempty"`  // a server of your own with a self-made certificate
}

// Kinds are the account kinds whose files this package reads.
func Kinds(kind string) bool { return kind == "webdav" || kind == "seafile" }

// Connect signs in to an account's files.
func Connect(ctx context.Context, kind, cfgJSON, secret string) (Source, error) {
	var c Cfg
	if err := json.Unmarshal([]byte(cfgJSON), &c); err != nil {
		return nil, errors.New("bad configuration")
	}
	switch {
	case kind == "seafile":
		return NewSeafile(ctx, c.URL, c.User, secret, c.Insecure)
	case kind == "webdav" && c.Nextcloud:
		return Nextcloud(ctx, c.URL, c.User, secret, c.Insecure)
	case kind == "webdav":
		return NewWebDAV(c.URL, c.User, secret, c.Insecure)
	}
	return nil, fmt.Errorf("not a files account: %s", kind)
}

// Test signs in and lists the top: the Connections page's Test button.
func Test(ctx context.Context, kind, cfgJSON, secret string) (string, error) {
	s, err := Connect(ctx, kind, cfgJSON, secret)
	if err != nil {
		return "", err
	}
	l, err := s.List(ctx, "")
	if err != nil {
		return "", err
	}
	if len(l) == 1 {
		return "signed in: 1 item at the top", nil
	}
	return fmt.Sprintf("signed in: %d items at the top", len(l)), nil
}
