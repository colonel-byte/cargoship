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

// Package v1alpha1 is for the shared File logic across both the cluster and distro api's
package v1alpha1

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// ZarfFile used by both Distro and Cluster logic
type ZarfFile struct {
	// Data file contents
	Data string `json:"data,omitempty"`
	// Executable if the file should have executable permissions
	Executable bool `json:"executable,omitempty"`
	// ExtractPath used to extract a file from a tar ball
	ExtractPath string `json:"extractPath,omitempty"`
	// Group to which the file will be owned by
	Group string `json:"group,omitempty"`
	// Name id, not the actual file name
	Name string `json:"name,omitempty"`
	// PermMode what permissions will be applied to the file
	PermMode any `json:"perm,omitempty"`
	// Selector used to determine if this file should be uploaded to the node
	Selector BinarySelector `json:"selector,omitempty"`
	// Shasum is used to check the file during sourcing of the distro package
	Shasum string `json:"shasum,omitempty"`
	// Source path the file should be found during package creation
	Source string `json:"source"`
	// Symlinks that will be created from the Target file
	Symlinks []string `json:"symlinks,omitempty"`
	// Target path on the remote host that will created
	Target string `json:"target"`
	// TargetIsDir if the target is a directory, normally used for image upload logic
	TargetIsDir bool `json:"isDirectory,omitempty"`
	// User to which the file will be owned by
	User string `json:"user,omitempty"`
	// Base is runtime option
	Base string `json:"-"`
	// PermString is runtime option
	PermString string `json:"-"`
	// DirPermString is runtime option
	DirPermString string `json:"-"`
	// OriginalTarget is runtime option
	OriginalTarget string `json:"-"`
	// LocalSource is runtime option
	LocalSource LocalFile `json:"-"`
}

// LocalFile runtime information that will be used for the local files
type LocalFile struct {
	// Path to the file that will be uploaded
	Path string
	// PermMode permission of the file
	PermMode string
}

// BinarySelector allows for filtering files based on certain criteria
type BinarySelector struct {
	// Roles arbitrary list of roles to upload files too
	Roles []string `json:"roles,omitempty"`
	// Profile what type of node to upload files too
	Profile string `json:"profile,omitempty" jsonschema:"enum=worker,enum=controller"`
	// Package what type of engine binary will be used to install
	Package string `json:"package,omitempty" jsonschema:"enum=rpm,enum=apt,enum=binary"`
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

// HasData if the Data is not empty
func (u *ZarfFile) HasData() bool {
	return strings.TrimSpace(u.Data) != ""
}

// TargetDirectory returns the directory for the target file
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
