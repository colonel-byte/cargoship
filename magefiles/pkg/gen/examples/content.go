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

package examples

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/colonel-byte/cargoship/pkg/distrogen"
	"github.com/colonel-byte/cargoship/pkg/helmvalues"
)

// ArchByName is the architecture this version targets under name, for a template that writes
// one architecture's manifest. It returns nil when the version does not target it, so a
// template reaches it with `with` and writes nothing for an architecture that is not there.
func (v exampleVersion) ArchByName(name string) *exampleArch {
	for i := range v.Arches {
		if v.Arches[i].Arch == name {
			return &v.Arches[i]
		}
	}
	return nil
}

// ContentFiles is every file URL one architecture installs, deduplicated and in a stable order:
// what a content manifest has to collect for a build to need no network. An architecture this
// version does not target has none.
//
// The URLs come from the same fields the distro.yaml template writes its `source` values from,
// which is the point -- cargoship matches a file in a content store by the URL the collecting
// tool recorded it as having come from, so a list rebuilt here instead would be one upstream
// change away from matching nothing. A field a distro does not use is empty and skipped, so
// this covers k3s's single binary as well as rke2's RPMs and tarballs.
//
// Each URL is collected whole, with no notion of extracting from it. A distro definition that
// declares an extractPath expects the archive, and both store formats hold what they were
// given, so a manifest that extracted on the operator's behalf would hand cargoship bytes it
// would then try to extract a second time.
//
// The architecture-independent policy RPM is included for every architecture. Collecting one
// architecture has to be enough to build it, and leaving a file out of both is the failure
// nobody notices until an install.
func (v exampleVersion) ContentFiles(name string) []string {
	arch := v.ArchByName(name)
	if arch == nil {
		return nil
	}

	candidates := []string{
		v.SelinuxRPM,
		arch.CommonRPM,
		arch.ServerRPM,
		arch.AgentRPM,
		arch.TarballURL,
		arch.EtcdURL,
		arch.BinaryURL,
	}

	seen := map[string]bool{}
	files := make([]string, 0, len(candidates))
	for _, url := range candidates {
		if url == "" || seen[url] {
			continue
		}
		seen[url] = true
		files = append(files, url)
	}
	return files
}

// base is the template's base function: the last segment of a URL, which is the filename an
// artifact is collected under. Unlike haulerName it rewrites nothing -- a zarf package stores a
// file at a path rather than under an OCI tag, so the characters hauler cannot take are fine.
func base(url string) string {
	if i := strings.LastIndex(url, "/"); i >= 0 {
		return url[i+1:]
	}
	return url
}

// haulerName is the template's haulerName function: the name a hauler manifest has to give an
// artifact, or "" when the one hauler would derive itself is already usable.
//
// Hauler names a file artifact after the last segment of its URL and uses that as an OCI tag.
// Rancher's RPM filenames carry a `~` before the distro revision -- rke2-common-1.36.5~rke2r1-
// 0.el9.x86_64.rpm -- and `~` is not legal in a tag, so hauler cannot derive a reference and
// fails the whole sync with "could not parse reference", taking every other file in the
// manifest with it. Renaming is safe because the recorded origin stays the URL, which is what
// cargoship matches on.
//
// Returning "" for a name that needs no rewriting is what lets the template write the field
// only where it is doing something, the way it writes a shasum only where there is one.
func haulerName(url string) string {
	base := url
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	safe := strings.ReplaceAll(base, "~", "-")
	if safe == base {
		return ""
	}
	return safe
}

// trimRegistry is the template's trimRegistry function: an image reference with the manifest's
// own registry host dropped, so the document's hauler.dev/registry annotation is what supplies
// it. Hauler fills that annotation in for unqualified names only, so an image left qualified is
// an image the annotation cannot redirect -- and an operator collecting from a mirror would be
// editing every line rather than one.
//
// Only the manifest's own host is trimmed. A reference under any other registry is returned
// untouched and stays qualified, which is what hauler needs to leave it alone: the annotation
// would otherwise silently repoint an image that is not published there.
func trimRegistry(registry, image string) string {
	return strings.TrimPrefix(image, registry+"/")
}

// writeExampleContentManifests renders a flavor's content-collection manifests beside its
// distro.yaml, one per template, under the template's own name without the .tmpl -- the same
// rule the values templates follow. Two formats are rendered, for the two a build can read
// back through --content-store: hauler manifests, and zarf package definitions.
//
// There is a template per output, rather than one template emitting several, because each file
// is a thing an operator names on a command line. How far that splits differs by format, and
// the difference is not cosmetic:
//
//   - hauler manifests split as far as is useful, because `hauler store sync` merges any number
//     of them into one store: one architecture's images, one chart's images, one architecture's
//     files.
//   - a zarf definition does not split past the architecture, because each one builds a separate
//     package and --content-store takes one path. Images and files that were split across two
//     definitions would build two packages, only one of which a build could be pointed at.
//
// A file that has to be split before it can be used is not worth generating whole.
//
// A template that renders no manifest is not written. Each one guards its own body -- on the
// architecture being targeted, or on the chart declaring any images -- so a flavor that does
// not have what a template describes produces comments and nothing else, and comments are not
// a manifest. That is what keeps a single-architecture flavor from getting an arm64 file, and
// an rke2 flavor that is not cilium from getting a cilium one. The alternative, an empty
// `images:` list, is a manifest hauler accepts and collects nothing from.
func writeExampleContentManifests(dir string, spec exampleDistroSpec, f exampleFlavor, v exampleVersion, sums *exampleShasums) error {
	formats := []struct {
		name      string
		wanted    bool
		templates []string
	}{
		{name: "hauler manifests", wanted: f.haulerManifests, templates: spec.haulerTemplates},
		{name: "zarf package definitions", wanted: f.zarfPackages, templates: spec.zarfTemplates},
	}

	templates := make([]string, 0, len(spec.haulerTemplates)+len(spec.zarfTemplates))
	for _, format := range formats {
		if !format.wanted {
			continue
		}
		// A flavor asking a distro for a format the distro has no templates for is a wiring
		// mistake rather than a flavor with nothing to collect, so it fails rather than
		// quietly writing nothing.
		if len(format.templates) == 0 {
			return fmt.Errorf("%s asks for %s and %s declares no templates to render", f.dir, format.name, spec.name)
		}
		templates = append(templates, format.templates...)
	}
	if len(templates) == 0 {
		return nil
	}

	funcs := helmvalues.WithFuncs(template.FuncMap{
		"haulerName":   haulerName,
		"base":         base,
		"sha256":       sums.get,
		"trimRegistry": trimRegistry,
	})

	files := make([]distrogen.File, 0, len(templates))
	for _, path := range templates {
		files = append(files, distrogen.File{Template: path})
	}
	rendered, err := distrogen.Render(files, v, funcs)
	if err != nil {
		return err
	}

	keep := make([]distrogen.Rendered, 0, len(rendered))
	for _, r := range rendered {
		if declaresManifest(r.Content) {
			keep = append(keep, r)
		}
	}
	if _, err := distrogen.Write(dir, keep); err != nil {
		return err
	}
	return nil
}

// declaresManifest reports whether rendered output holds a manifest rather than only the
// comments a guarded template leaves behind. An apiVersion at the start of a line is the one
// thing every output has and no comment does -- a hauler manifest and a zarf definition both
// declare one.
func declaresManifest(rendered []byte) bool {
	return bytes.HasPrefix(rendered, []byte("apiVersion:")) ||
		bytes.Contains(rendered, []byte("\napiVersion:"))
}
