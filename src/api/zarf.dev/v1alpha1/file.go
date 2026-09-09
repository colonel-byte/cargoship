// Copyright 2023 k0sctl authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from k0sctl:
// https://github.com/k0sproject/k0sctl
//
// Modifications Copyright 2026 colonel-byte.
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

// Package v1alpha1 defines file types shared by the cluster and distro APIs.
package v1alpha1

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/colonel-byte/cargoship/src/api"
)

// ZarfFile defines a file shared by the distro and cluster APIs.
type ZarfFile struct {
	// Data holds the inline file content.
	Data string `json:"data,omitempty"`
	// Executable sets executable permissions on the file when true.
	Executable bool `json:"executable,omitempty"`
	// ExtractPath is the path cargoship extracts from a tarball.
	ExtractPath string `json:"extractPath,omitempty"`
	// Group is the group that owns the file.
	Group string `json:"group,omitempty"`
	// Name identifies the file entry. It is not the file name on disk.
	Name string `json:"name,omitempty"`
	// PermMode sets the permissions cargoship applies to the file. It accepts an octal string or an integer, for example "0644" or 644.
	PermMode any `json:"perm,omitempty"`
	// Selector controls whether cargoship uploads this file to a given node.
	Selector BinarySelector `json:"selector,omitempty"`
	// Shasum verifies the file when cargoship sources the distro package.
	Shasum string `json:"shasum,omitempty"`
	// Source is the path where cargoship finds the file when it creates the package.
	Source string `json:"source"`
	// Symlinks lists the symlinks cargoship creates that point to Target.
	Symlinks []string `json:"symlinks,omitempty"`
	// Target is the path on the remote host where cargoship writes the file.
	Target string `json:"target"`
	// TargetIsDir indicates that Target is a directory. Cargoship uses this mainly for image uploads.
	TargetIsDir bool `json:"isDirectory,omitempty"`
	// User is the user that owns the file.
	User string `json:"user,omitempty"`
	// Base is the local directory cargoship joins with LocalSource.Path to find the file. Cargoship resolves it at runtime; it is not read from the config.
	Base string `json:"-"`
	// Category labels why cargoship is uploading this file, e.g. "engine", "image", "file", or "data". Cargoship records it in the remote upload manifest; it is not read from the config.
	Category string `json:"-"`
	// PermString is the octal permission string cargoship applies to the file. Cargoship derives it from PermMode at runtime.
	PermString string `json:"-"`
	// DirPermString is the octal permission string cargoship applies to the file's parent directory.
	DirPermString string `json:"-"`
	// OriginalTarget is the target path before cargoship renames it during install. Cargoship uses it to move the file back into place.
	OriginalTarget string `json:"-"`
	// LocalSource holds the resolved local path and permission cargoship uses to upload the file.
	LocalSource LocalFile `json:"-"`
}

// LocalFile holds the local path and permission cargoship resolves for uploading a file.
type LocalFile struct {
	// Path is the local path to the file cargoship uploads.
	Path string
	// PermMode is the octal permission string cargoship applies to the file.
	PermMode string
}

// BinarySelector selects which files to upload to a host, based on host role and install method.
type BinarySelector struct {
	// Roles lists the host roles this file applies to.
	Roles []string `json:"roles,omitempty"`
	// Profile selects which host role receives this file: worker or controller.
	Profile string `json:"profile,omitempty" jsonschema:"enum=worker,enum=controller"`
	// Package selects which install method receives this file: rpm, apt, or binary.
	Package string `json:"package,omitempty" jsonschema:"enum=rpm,enum=apt,enum=binary"`
	// Arch lists the CPU architectures this file applies to. An empty list means every architecture the package targets.
	Arch api.Arches `json:"arch,omitempty"`
}

// MatchesArch reports whether this selector applies to arch. An empty Arch list matches every
// architecture, mirroring how an empty Profile matches every profile.
func (s BinarySelector) MatchesArch(arch api.Arch) bool {
	if len(s.Arch) == 0 {
		return true
	}
	return slices.Contains(s.Arch, arch)
}

// String returns Name. If Name is empty, it returns Source instead.
func (u *ZarfFile) String() string {
	if u.Name == "" {
		return u.Source
	}
	return u.Name
}

// Owner returns a chown-compatible "user:group" string built from User and Group. It returns an empty string when both are empty.
func (u *ZarfFile) Owner() string {
	return strings.TrimSuffix(fmt.Sprintf("%s:%s", u.User, u.Group), ":")
}

// UnmarshalYAML unmarshals the YAML data, then converts PermMode into PermString.
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

// HasData reports whether Data holds non-blank content.
func (u *ZarfFile) HasData() bool {
	return strings.TrimSpace(u.Data) != ""
}

// TargetDirectory returns Target if TargetIsDir is true. Otherwise it returns the base name of Target.
func (u *ZarfFile) TargetDirectory() string {
	if u.TargetIsDir {
		return u.Target
	}
	return filepath.Base(u.Target)
}

// permToString converts an int, float64, or string permission value to an octal string for chmod.
// It returns an empty string and no error for any other type.
func permToString(val any) (string, error) {
	var s string
	switch t := val.(type) {
	case int, float64:
		var num int
		if n, ok := t.(float64); ok {
			num = int(n)
		} else {
			num = t.(int) //nolint:errcheck
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
