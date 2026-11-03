// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package pkginfo reads the metadata a software package carries about itself, with the Go standard library only: from a Symbian SIS/SISX the
// names, vendor, UID, version, target platforms, languages and the application icon (an MBM bitmap, decoded to PNG); from an Android APK the
// package name, version, label, icon and the processors it ships code for (binary AndroidManifest.xml and resources.arsc). Nothing is installed or
// executed; damaged files give an error, never a panic. docs/catalogue.md ("What the scanner reads") lists what is and is not extracted.
package pkginfo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Info is what could be read from one package. Any field may be empty.
type Info struct {
	Kind      string   `json:"kind"`                // sis | apk
	Platform  string   `json:"platform"`            // symbian | android
	AppID     string   `json:"app_id"`              // derived: uid-xxxxxxxx for Symbian, the package name for Android
	Name      string   `json:"name"`                //
	Version   string   `json:"version"`             //
	Vendor    string   `json:"vendor,omitempty"`    //
	UID       string   `json:"uid,omitempty"`       // Symbian package UID, 8 hex digits
	Arch      []string `json:"arch,omitempty"`      // arm | x32 for Symbian, aarch64 / armv7 / x86_64 / x86 for Android; empty when the package does not say
	Targets   []string `json:"targets,omitempty"`   // the devices / platform versions it is meant for
	Languages []string `json:"languages,omitempty"` //
	Files     int      `json:"files,omitempty"`     // number of files the package installs (SIS)
	Icon      []byte   `json:"-"`                   // PNG
	IconNote  string   `json:"icon_note,omitempty"` // why there is no icon, when the package has one we cannot show
}

// ErrUnsupported is returned for a file type this package does not read (the entry is still listed, without metadata).
var ErrUnsupported = errors.New("no metadata is read from this kind of file")

// MaxInspect is the largest SIS file read into memory.
const MaxInspect = 512 << 20

// Inspect reads the metadata of the package at path; the extension picks the reader (sis, sisx, apk).
func Inspect(path string) (info *Info, err error) {
	defer func() {
		if r := recover(); r != nil { // a damaged file must not take the server down
			info, err = nil, fmt.Errorf("the file is damaged (%v)", r)
		}
	}()
	switch ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), ".")); ext {
	case "sis", "sisx":
		fi, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if fi.Size() > MaxInspect {
			return nil, errors.New("the file is too large to be read for metadata")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return ParseSIS(b)
	case "apk":
		return ParseAPK(path)
	}
	return nil, ErrUnsupported
}
