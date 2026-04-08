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

package cluster

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/k0sproject/rig/log"
)

type LocalFile struct {
	Path     string
	PermMode string
}

// UploadFile describes a file to be uploaded for the host
type UploadFile struct {
	Name            string       `yaml:"name,omitempty"`
	Source          string       `yaml:"src,omitempty"`
	Data            string       `yaml:"data,omitempty"`
	DestinationDir  string       `yaml:"dstDir,omitempty"`
	DestinationFile string       `yaml:"dst,omitempty"`
	PermMode        any          `yaml:"perm,omitempty"`
	DirPermMode     any          `yaml:"dirPerm,omitempty"`
	User            string       `yaml:"user,omitempty"`
	Group           string       `yaml:"group,omitempty"`
	PermString      string       `yaml:"-"`
	DirPermString   string       `yaml:"-"`
	Sources         []*LocalFile `yaml:"-"`
	Base            string       `yaml:"-"`
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

// UnmarshalYAML sets in some sane defaults when unmarshaling the data from yaml
func (u *UploadFile) UnmarshalYAML(unmarshal func(any) error) error {
	type uploadFile UploadFile
	yu := (*uploadFile)(u)

	if err := unmarshal(yu); err != nil {
		return err
	}

	fp, err := permToString(u.PermMode)
	if err != nil {
		return err
	}
	u.PermString = fp

	dp, err := permToString(u.DirPermMode)
	if err != nil {
		return err
	}
	u.DirPermString = dp

	return nil
}

// String returns the file bundle name or if it is empty, the source.
func (u *UploadFile) String() string {
	if u.Name == "" {
		return u.Source
	}
	return u.Name
}

// Owner returns a chown compatible user:group string from User and Group, or empty when neither are set.
func (u *UploadFile) Owner() string {
	return strings.TrimSuffix(fmt.Sprintf("%s:%s", u.User, u.Group), ":")
}

// returns true if the string contains any glob characters
func isGlob(s string) bool {
	return strings.ContainsAny(s, "*%?[]{}")
}

// ResolveRelativeTo sets the destination and resolves globs/local paths relative to baseDir.
func (u *UploadFile) ResolveRelativeTo(baseDir string) error {
	if u.IsURL() {
		if u.DestinationFile == "" {
			if u.DestinationDir != "" {
				u.DestinationFile = path.Join(u.DestinationDir, path.Base(u.Source))
			} else {
				u.DestinationFile = path.Base(u.Source)
			}
		}
		return nil
	}

	if u.HasData() {
		return nil
	}

	u.Base = ""
	u.Sources = nil

	src := filepath.ToSlash(u.Source)
	if src == "" {
		return fmt.Errorf("failed to resolve local path for %s: empty source", u)
	}
	if !path.IsAbs(src) {
		if baseDir != "" {
			src = path.Join(baseDir, src)
		}
	}
	src = path.Clean(src)

	if isGlob(u.Source) {
		return u.glob(src)
	}

	fsPath := filepath.FromSlash(src)
	stat, err := os.Stat(fsPath)
	if err != nil {
		return fmt.Errorf("failed to stat local path for %s: %w", u, err)
	}

	if stat.IsDir() {
		log.Tracef("source %s is a directory, assuming %s/**/*", src, src)
		return u.glob(path.Join(src, "**/*"))
	}

	perm := u.PermString
	if perm == "" {
		perm = fmt.Sprintf("%o", stat.Mode())
	}
	u.Base = path.Dir(src)
	u.Sources = []*LocalFile{
		{Path: path.Base(src), PermMode: perm},
	}

	return nil
}

// finds files based on a glob pattern
func (u *UploadFile) glob(src string) error {
	base, pattern := doublestar.SplitPattern(src)
	u.Base = base
	fsys := os.DirFS(filepath.FromSlash(base))
	sources, err := doublestar.Glob(fsys, pattern)
	if err != nil {
		return err
	}

	for _, s := range sources {
		abs := path.Join(base, s)
		log.Tracef("glob %s found: %s", abs, s)
		stat, err := os.Stat(filepath.FromSlash(abs))
		if err != nil {
			return fmt.Errorf("failed to stat file %s: %w", u, err)
		}

		if stat.IsDir() {
			log.Tracef("%s is a directory", abs)
			continue
		}

		perm := u.PermString
		if perm == "" {
			perm = fmt.Sprintf("%o", stat.Mode())
		}

		u.Sources = append(u.Sources, &LocalFile{Path: s, PermMode: perm})
	}

	if len(u.Sources) == 0 {
		return fmt.Errorf("no files found for %s", u)
	}

	if u.DestinationFile != "" && len(u.Sources) > 1 {
		return fmt.Errorf("found multiple files for %s but single file dst %s defined", u, u.DestinationFile)
	}

	return nil
}

// IsURL returns true if the source is a URL
func (u *UploadFile) IsURL() bool {
	return strings.Contains(u.Source, "://")
}

func (u *UploadFile) HasData() bool {
	return strings.TrimSpace(u.Data) != ""
}
