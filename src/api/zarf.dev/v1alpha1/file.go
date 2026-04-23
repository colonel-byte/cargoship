// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha1

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

type ZarfFile struct {
	//keep-sorted start
	Data        string         `json:"data,omitempty"`
	Executable  bool           `json:"executable,omitempty"`
	ExtractPath string         `json:"extractPath,omitempty"`
	Group       string         `json:"group,omitempty"`
	Name        string         `json:"name,omitempty"`
	PermMode    any            `json:"perm,omitempty"`
	Selector    BinarySelector `json:"selector,omitempty"`
	Shasum      string         `json:"shasum,omitempty"`
	Source      string         `json:"source"`
	Symlinks    []string       `json:"symlinks,omitempty"`
	Target      string         `json:"target"`
	TargetIsDir bool           `json:"isDirectory,omitempty"`
	User        string         `json:"user,omitempty"`
	//keep-sorted end
	Base           string    `json:"-"`
	PermString     string    `json:"-"`
	DirPermString  string    `json:"-"`
	OriginalTarget string    `json:"-"`
	LocalSource    LocalFile `json:"-"`
}

type LocalFile struct {
	Path     string
	PermMode string
}

type BinarySelector struct {
	Roles   []string `json:"roles,omitempty"`
	Profile string   `json:"profile,omitempty" jsonschema:"enum=worker,enum=controller"`
	Package string   `json:"package,omitempty" jsonschema:"enum=rpm,enum=apt,enum=binary"`
}

// String returns the file bundle name or if it is empty, the source.
func (u *ZarfFile) String() string {
	if u.Name == "" {
		return u.Source
	}
	return u.Name
}

// Owner returns a chown compatible user:group string from User and Group, or empty when neither are set.
func (u *ZarfFile) Owner() string {
	return strings.TrimSuffix(fmt.Sprintf("%s:%s", u.User, u.Group), ":")
}

// UnmarshalYAML sets in some sane defaults when unmarshaling the data from yaml
func (u *ZarfFile) UnmarshalYAML(unmarshal func(any) error) error {
	type uploadFile ZarfFile
	yu := (*uploadFile)(u)

	if err := unmarshal(yu); err != nil {
		return err
	}

	fp, err := permToString(u.PermMode)
	if err != nil {
		return err
	}
	u.PermString = fp

	return nil
}

func (u *ZarfFile) HasData() bool {
	return strings.TrimSpace(u.Data) != ""
}

func (u *ZarfFile) TargetDirectory() string {
	if u.TargetIsDir {
		return u.Target
	}
	return filepath.Base(u.Target)
}

// converts string or integer value to octal string for chmod
func permToString(val any) (string, error) {
	var s string
	switch t := val.(type) {
	case int, float64:
		var num int
		if n, ok := t.(float64); ok {
			num = int(n)
		} else {
			num = t.(int)
		}

		if num < 0 {
			return s, fmt.Errorf("invalid permission: %d: must be a positive value", num)
		}
		if num == 0 {
			return s, fmt.Errorf("invalid nil permission")
		}
		s = fmt.Sprintf("%#o", num)
	case string:
		s = t
	default:
		return "", nil
	}

	for i, c := range s {
		n, err := strconv.Atoi(string(c))
		if err != nil {
			return s, fmt.Errorf("failed to parse permission %s: %w", s, err)
		}

		// These could catch some weird octal conversion mistakes
		if i == 1 && n < 4 {
			return s, fmt.Errorf("invalid permission %s: owner would have unconventional access", s)
		}
		if n > 7 {
			return s, fmt.Errorf("invalid permission %s: octal value can't have numbers over 7", s)
		}
	}

	return s, nil
}
