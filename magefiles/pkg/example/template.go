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

package example

import (
	"fmt"
	"path/filepath"
	"text/template"

	"github.com/colonel-byte/cargoship/magefiles/pkg/example/layout"
	"github.com/colonel-byte/cargoship/magefiles/pkg/example/shasums"
)

// parseTemplate parses a distro's example template, wiring in the sha256 function its
// remote file entries are hashed with.
func parseTemplate(spec distroSpec, sums *shasums.Cache) (*template.Template, error) {
	tmpl, err := template.New(filepath.Base(spec.template)).
		Funcs(template.FuncMap{"sha256": sums.Get}).
		ParseFiles(spec.template)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", spec.template, err)
	}
	return tmpl, nil
}

// renderableTags is every tag one flavor renders an example for: the pinned ones plus whatever
// that flavor already has on disk, newest first, less the minor lines the flavor does not cover.
func renderableTags(pinned []string, spec distroSpec, f flavor) ([]string, error) {
	tags, err := layout.Tags(f.dir, spec.name, pinned)
	if err != nil {
		return nil, err
	}
	return filterFlavorTags(tags, f)
}

// write renders one tag into <flavor dir>/<minor>/<tag>/distro.yaml, creating the
// minor line directory if this is its first example, and returns the path it wrote.
//
// It returns "" for a build upstream no longer publishes, having removed any example
// already on disk for it. Rancher supersedes an rke2rN with the next revision and drops the
// old RPMs -- v1.35.0+rke2r2 and v1.35.3+rke2r2 both gave way to an rke2r3 -- and an example
// pointing at RPM URLs that 404 cannot be built, so keeping it only invites someone to try.
// The git tag surviving is what makes these renderable in the first place, so it is the
