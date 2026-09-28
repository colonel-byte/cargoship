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

// Package layout is where generated examples live on disk and how their directories are named.
//
// It is its own package because both magefiles/pkg/example and magefiles/pkg/example/upstream
// have to read the same tree back: an example already on disk is re-rendered whether or not its
// tag is still pinned, so an edit to a template reaches the older examples rather than leaving
// them to drift.
package layout

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/colonel-byte/cargoship/magefiles/pkg/engine/pins"
)

// minorPattern matches the minor line directories examples are grouped under
// (example/rke2-cilium/v1_35), so anything else at that level is left alone. The naming
// matches thirdparty-src/<distro>/ and pkg/engineconfig/gen/<distro>/.
var minorPattern = regexp.MustCompile(`^v[0-9]+_[0-9]+$`)

// Tags is every tag that should be rendered under dir: the pinned ones plus whatever is already
// there, newest first.
//
// Examples live at <dir>/<minor>/<tag>/distro.yaml, with the tag's "+" written as a "-" since
// "+" is awkward in a path; distro is the name that separator precedes ("rke2" in
// "v1.36.4-rke2r1"), which is what makes the swap reversible.
func Tags(dir, distro string, pinned []string) ([]string, error) {
	tags := map[string]bool{}
	for _, tag := range pinned {
		tags[tag] = true
	}

	minors, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		// A flavor with no examples yet renders the pinned tags, and grows from there.
		return sorted(tags)
	}
	if err != nil {
		return nil, err
	}

	for _, minor := range minors {
		if !minor.IsDir() || !minorPattern.MatchString(minor.Name()) {
			continue
		}

		entries, err := os.ReadDir(filepath.Join(dir, minor.Name()))
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			tag := strings.Replace(e.Name(), "-"+distro, "+"+distro, 1)
			if _, err := pins.TagVersion(tag); err != nil {
				// Not a version directory -- nothing to render.
				continue
			}
			tags[tag] = true
		}
	}

	return sorted(tags)
}

// sorted flattens a tag set newest first.
func sorted(tags map[string]bool) ([]string, error) {
	out := slices.Collect(maps.Keys(tags))
	if err := pins.SortDesc(out); err != nil {
		return nil, err
	}
	return out, nil
}
