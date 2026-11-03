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
	"os"
	"path/filepath"
	"strings"
	"text/template"
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

// HaulerFiles is every file URL one architecture installs, deduplicated and in a stable order:
// what a hauler manifest has to collect for a build to need no network. An architecture this
// version does not target has none.
//
// The URLs come from the same fields the distro.yaml template writes its `source` values from,
// which is the point -- cargoship matches a file in a store by the URL hauler recorded it as
// having come from, so a list rebuilt here instead would be one upstream change away from
// matching nothing. A field a distro does not use is empty and skipped, so this covers k3s's
// single binary as well as rke2's RPMs and tarballs.
//
// The architecture-independent policy RPM is included for every architecture. Collecting one
// architecture has to be enough to build it, and leaving a file out of both is the failure
// nobody notices until an install.
func (v exampleVersion) HaulerFiles(name string) []string {
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

// writeExampleHaulerManifests renders a flavor's hauler manifests beside its distro.yaml, one
// per template, under the template's own name without the .tmpl -- the same rule the values
// templates follow.
//
// There is a template per manifest, rather than one template emitting several, because each
// file is a thing an operator names on a `hauler store sync` command line: one architecture's
// images, one chart's images, one architecture's files. A file that has to be split before it
// can be used is not worth generating whole.
//
// A template that renders no manifest is not written. Each one guards its own body -- on the
// architecture being targeted, or on the chart declaring any images -- so a flavor that does
// not have what a template describes produces comments and nothing else, and comments are not
// a manifest. That is what keeps a single-architecture flavor from getting an arm64 file, and
// an rke2 flavor that is not cilium from getting a cilium one. The alternative, an empty
// `images:` list, is a manifest hauler accepts and collects nothing from.
func writeExampleHaulerManifests(dir string, spec exampleDistroSpec, f exampleFlavor, v exampleVersion) error {
	if !f.haulerManifests {
		return nil
	}
	if len(spec.haulerTemplates) == 0 {
		return fmt.Errorf("%s asks for hauler manifests and %s declares no templates to render", f.dir, spec.name)
	}

	for _, path := range spec.haulerTemplates {
		tmpl, err := template.New(filepath.Base(path)).
			Funcs(template.FuncMap{"haulerName": haulerName}).
			ParseFiles(path)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, v); err != nil {
			return fmt.Errorf("rendering %s: %w", path, err)
		}
		if !declaresManifest(buf.Bytes()) {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(path), ".tmpl")
		if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// declaresManifest reports whether rendered output holds a manifest rather than only the
// comments a guarded template leaves behind. An apiVersion at the start of a line is the one
// thing every manifest has and no comment does.
func declaresManifest(rendered []byte) bool {
	return bytes.HasPrefix(rendered, []byte("apiVersion:")) ||
		bytes.Contains(rendered, []byte("\napiVersion:"))
}
