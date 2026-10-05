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

// This file holds tag selection: which tags a flavor renders examples for, gathered from
// pins.json plus whatever is already on disk, and the newest-first order they are rendered in.

package examples

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/engineconfig"
)

// exampleMinorPattern matches the minor line directories examples are grouped under
// (example/rke2-cilium/v1_35), so anything else at that level is left alone. The naming
// matches thirdparty-src/<distro>/ and pkg/engineconfig/gen/<distro>/.
var exampleMinorPattern = regexp.MustCompile(`^v[0-9]+_[0-9]+$`)

// exampleTags is every tag one flavor renders an example for: the pinned ones plus whatever
// that flavor already has on disk, newest first. Examples live at
// <flavor dir>/<minor>/<tag>/distro.yaml, and a tag directory is named for its tag with the
// "+" swapped for a "-", since "+" is awkward in a path.
func exampleTags(pinned []string, spec exampleDistroSpec, f exampleFlavor) ([]string, error) {
	tags := map[string]bool{}
	for _, tag := range pinned {
		tags[tag] = true
	}

	minors, err := os.ReadDir(f.dir)
	if os.IsNotExist(err) {
		// A flavor with no examples yet renders the pinned tags, and grows from there.
		return flavorTags(tags, f)
	}
	if err != nil {
		return nil, err
	}
	for _, minor := range minors {
		if !minor.IsDir() || !exampleMinorPattern.MatchString(minor.Name()) {
			continue
		}

		entries, err := os.ReadDir(filepath.Join(f.dir, minor.Name()))
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			tag := strings.Replace(e.Name(), "-"+spec.name, "+"+spec.name, 1)
			if _, err := engineconfig.TagVersion(tag); err != nil {
				// Not a version directory -- nothing to render.
				continue
			}
			tags[tag] = true
		}
	}

	return flavorTags(tags, f)
}

// flavorTags flattens a tag set newest first, dropping the minor lines the flavor does not
// render.
func flavorTags(tags map[string]bool, f exampleFlavor) ([]string, error) {
	out := slices.Collect(maps.Keys(tags))
	if err := sortTagsDesc(out); err != nil {
		return nil, err
	}
	return filterFlavorTags(out, f)
}

// sortTagsDesc orders tags newest first, so generated output and the lines it prints read
// the way a reader scanning for the current release expects.
func sortTagsDesc(tags []string) error {
	var sortErr error
	slices.SortFunc(tags, func(a, b string) int {
		va, err := engineconfig.TagVersion(a)
		if err != nil {
			sortErr = err
		}
		vb, err := engineconfig.TagVersion(b)
		if err != nil {
			sortErr = err
		}
		if c := slices.Compare(vb[:], va[:]); c != 0 {
			return c
		}
		return strings.Compare(b, a) // same version, order by the rke2rN/k3sN revision
	})
	return sortErr
}
