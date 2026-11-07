// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package host is the host manager (docs/host-manager.md): the one program on the board that touches Docker and the system. It
// switches Cloud Companion's features on and off, keeps what was chosen, and answers Cloud Companion over a local socket.
package host

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Feature is one entry of the catalogue (deploy/features/*.yaml): compose profiles (and files) of Cloud Companion's compose
// project, what it needs from the board, and the settings asked when it is switched on.
type Feature struct {
	ID        string            `yaml:"id" json:"id"`
	Title     string            `yaml:"title" json:"title"`
	About     string            `yaml:"about" json:"about"`
	Core      bool              `yaml:"core" json:"core,omitempty"`              // always on
	Later     bool              `yaml:"after-setup" json:"afterSetup,omitempty"` // not offered in setup (needs the setup page's port 80)
	Files     []string          `yaml:"files" json:"-"`                          // compose files beside docker-compose.yml
	Profiles  []string          `yaml:"profiles" json:"-"`
	Requires  []string          `yaml:"requires" json:"requires,omitempty"`
	Conflicts []string          `yaml:"conflicts" json:"conflicts,omitempty"`
	Needs     Needs             `yaml:"needs" json:"needs"`
	Settings  []Setting         `yaml:"settings" json:"settings,omitempty"`
	Env       map[string]string `yaml:"env" json:"-"` // more variables, ${...} of the settings expanded
}

type Needs struct {
	Memory string   `yaml:"memory" json:"memory,omitempty"` // "300M", "4G"
	Arch   []string `yaml:"arch" json:"arch,omitempty"`     // Go names (amd64, arm64); empty = any
}

// Setting is a value the feature asks for; it reaches the compose project as the variable Env.
type Setting struct {
	Key      string `yaml:"key" json:"key"`
	Label    string `yaml:"label" json:"label"`
	Env      string `yaml:"env" json:"-"`
	Generate bool   `yaml:"generate" json:"generate,omitempty"` // made by the manager (passwords between containers): never asked
	Secret   bool   `yaml:"secret" json:"secret,omitempty"`     // never shown back
	Required bool   `yaml:"required" json:"required,omitempty"`
}

// Board is what the features are checked against.
type Board struct {
	Model   string `json:"model"`
	Arch    string `json:"arch"`
	Memory  int64  `json:"memory"`            // bytes
	Storage string `json:"storage,omitempty"` // what the system runs from: sd, nvme, usb (rootStorage)
}

type Catalogue struct {
	byID map[string]*Feature
	ids  []string // sorted
}

// LoadCatalogue reads every *.yaml of dir and checks that the catalogue holds together: unique ids, known requirements and
// conflicts, no requirement cycles.
func LoadCatalogue(fsys fs.FS, dir string) (*Catalogue, error) {
	names, err := fs.Glob(fsys, path.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	c := &Catalogue{byID: map[string]*Feature{}}
	for _, n := range names {
		b, err := fs.ReadFile(fsys, n)
		if err != nil {
			return nil, err
		}
		var f Feature
		if err := yaml.Unmarshal(b, &f); err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		if f.ID == "" || f.Title == "" {
			return nil, fmt.Errorf("%s: id and title are required", n)
		}
		if _, dup := c.byID[f.ID]; dup {
			return nil, fmt.Errorf("%s: feature %q twice", n, f.ID)
		}
		if _, err := ParseSize(f.Needs.Memory); err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		for _, s := range f.Settings {
			if s.Key == "" || s.Env == "" {
				return nil, fmt.Errorf("%s: a setting needs key and env", n)
			}
		}
		c.byID[f.ID] = &f
		c.ids = append(c.ids, f.ID)
	}
	sort.Strings(c.ids)
	for _, f := range c.byID {
		for _, r := range append(slices.Clone(f.Requires), f.Conflicts...) {
			if c.byID[r] == nil {
				return nil, fmt.Errorf("feature %q names unknown feature %q", f.ID, r)
			}
		}
		if _, err := c.Closure([]string{f.ID}); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (c *Catalogue) Get(id string) *Feature { return c.byID[id] }

// IDs lists the catalogue in order of id.
func (c *Catalogue) IDs() []string { return c.ids }

// Closure is the features plus the core ones and everything they require, required ones first.
func (c *Catalogue) Closure(ids []string) ([]string, error) {
	var out []string
	state := map[string]int{} // 1 visiting, 2 done
	var visit func(id string, from string) error
	visit = func(id, from string) error {
		f := c.byID[id]
		if f == nil {
			return fmt.Errorf("unknown feature %q", id)
		}
		switch state[id] {
		case 1:
			return fmt.Errorf("features %q and %q require each other", from, id)
		case 2:
			return nil
		}
		state[id] = 1
		for _, r := range f.Requires {
			if err := visit(r, id); err != nil {
				return err
			}
		}
		state[id] = 2
		out = append(out, id)
		return nil
	}
	for _, id := range c.ids {
		if c.byID[id].Core {
			if err := visit(id, id); err != nil {
				return nil, err
			}
		}
	}
	for _, id := range ids {
		if err := visit(id, id); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Fits says whether the board can run the feature, and if not, why (shown on the Features page).
func (f *Feature) Fits(b Board) (bool, string) {
	if len(f.Needs.Arch) > 0 && !slices.Contains(f.Needs.Arch, b.Arch) {
		return false, "Not available for this board's processor"
	}
	need, _ := ParseSize(f.Needs.Memory)
	if b.Memory > 0 && need > b.Memory {
		return false, "Needs " + f.Needs.Memory + "B of memory"
	}
	return true, ""
}

// ParseSize reads "300M", "4G", "512K" or a number of bytes; "" is 0.
func ParseSize(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	mul := int64(1)
	switch strings.ToUpper(s[len(s)-1:]) {
	case "K":
		mul = 1 << 10
	case "M":
		mul = 1 << 20
	case "G":
		mul = 1 << 30
	}
	if mul > 1 {
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("bad size %q", s)
	}
	return n * mul, nil
}
