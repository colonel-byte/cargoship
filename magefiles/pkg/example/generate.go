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
	"sort"
	"strings"
	"text/template"

	"github.com/colonel-byte/cargoship/magefiles/pkg/engine/pins"
	"github.com/colonel-byte/cargoship/magefiles/pkg/example/shasums"
	"github.com/colonel-byte/cargoship/magefiles/pkg/example/upstream"
)

// GenerateAll renders every flavor of every distro in distros -- one CNI each, into its own
// directory. Per flavor it covers each of that distro's tags in thirdparty-src/pins.json,
// so new examples appear as the pins move, plus every example directory that flavor already
// has on disk, so an edit to a template reaches the older examples too rather than leaving
// them to drift. Examples are grouped by minor line -- creating <flavor dir>/<minor>/ as
// needed -- so one release line's examples stay together. Everything that varies between
// versions is derived from the tag, except the image lists and digests, which come from that
// release's published assets. Touches the network.
func GenerateAll() error {
	manifest, err := pins.Read()
	if err != nil {
		return err
	}

	sums, err := shasums.Load()
	if err != nil {
		return err
	}
	// Save whatever was hashed even if a later version fails, so a long first run is not
	// thrown away.
	defer func() {
		if err := sums.Save(); err != nil {
			fmt.Println("warning: " + err.Error())
		}
	}()

	for _, spec := range distros {
		d, err := manifest.Distro(spec.name)
		if err != nil {
			return err
		}

		tmpl, err := parseTemplate(spec, sums)
		if err != nil {
			return err
		}

		for _, f := range spec.flavors {
			tags, err := renderableTags(d.Tags, spec, f)
			if err != nil {
				return err
			}

			for _, tag := range tags {
				if err := render(tmpl, d.Repo, tag, spec, f, sums); err != nil {
					return err
				}
			}
		}
	}

	up, err := manifest.Distro("upstream")
	if err != nil {
		return err
	}
	return upstream.RenderExamples(up.Tags, sums)
}

// GenerateLine renders an example for every release on one minor line of a distro.
//
// Where GenerateAll re-renders what is already pinned or already on disk, this backfills a
// whole line: it lists every non-RC tag of that distro on the given minor line and renders
// each one, so "rke2 v1.36" covers v1.36.0+rke2r1 through the newest v1.36 release. The
// examples land in each of that distro's flavor directories, and GenerateAll keeps them
// current from then on, since it covers every example directory on disk. Touches the network.
func GenerateLine(distro, prefix string) error {
	// Accept "1.36" as well as "v1.36" -- upstream tags carry the v, but the minor line is
	// as often written without it.
	if !strings.HasPrefix(prefix, "v") {
		prefix = "v" + prefix
	}

	manifest, err := pins.Read()
	if err != nil {
		return err
	}

	sums, err := shasums.Load()
	if err != nil {
		return err
	}
	// Save whatever was hashed even if a later version fails, so a long first run is not
	// thrown away.
	defer func() {
		if err := sums.Save(); err != nil {
			fmt.Println("warning: " + err.Error())
		}
	}()

	// Upstream (kubeadm) does not fit distroSpec -- see magefiles/pkg/example/upstream -- so
	// it gets its own backfill rather than one more distros entry.
	if distro == "upstream" {
		d, err := manifest.Distro("upstream")
		if err != nil {
			return err
		}
		return upstream.RenderLine(d.Repo, prefix, sums)
	}

	spec, err := distroByName(distro)
	if err != nil {
		return err
	}

	d, err := manifest.Distro(spec.name)
	if err != nil {
		return err
	}

	tmpl, err := parseTemplate(spec, sums)
	if err != nil {
		return err
	}

	tags, err := pins.RemoteTags(d.Repo, prefix)
	if err != nil {
		return err
	}
	if err := pins.SortDesc(tags); err != nil {
		return err
	}

	for _, f := range spec.flavors {
		rendered, err := filterFlavorTags(tags, f)
		if err != nil {
			return err
		}
		for _, tag := range rendered {
			if err := render(tmpl, d.Repo, tag, spec, f, sums); err != nil {
				return err
			}
		}
	}
	return nil
}

// render writes one example and reports what it did, since a build upstream has dropped is
// skipped rather than written.
func render(tmpl *template.Template, repoURL, tag string, spec distroSpec, f flavor, sums *shasums.Cache) error {
	path, err := write(tmpl, repoURL, tag, spec, f, sums)
	if err != nil {
		return fmt.Errorf("generating %s %s example for %s: %w", spec.name, f.flavorName(), tag, err)
	}
	if path == "" {
		fmt.Printf("Skipped %s %s: upstream no longer publishes what it installs\n", f.flavorName(), tag)
		return nil
	}
	fmt.Println("Generated " + path)
	return nil
}

// distroByName finds the spec a distro name asks for, listing the names that do work when it
// is not one of them.
func distroByName(name string) (distroSpec, error) {
	var names []string
	for _, spec := range distros {
		if spec.name == name {
			return spec, nil
		}
		names = append(names, spec.name)
	}
	return distroSpec{}, fmt.Errorf("unknown distro %q, want one of: %s", name, strings.Join(names, ", "))
}

// Definitions returns every directory under example/ holding a distro.yaml, sorted so a run
// covers the flavors in a stable order. Examples live at
// example/<flavor>/<minor>/<version>/distro.yaml, so the glob is fixed at that depth rather
// than walking the tree, which keeps a stray distro.yaml elsewhere under example/ out of it.
func Definitions() ([]string, error) {
	matches, err := filepath.Glob(filepath.Join("example", "*", "*", "*", "distro.yaml"))
	if err != nil {
		return nil, err
	}

	dirs := make([]string, 0, len(matches))
	for _, m := range matches {
		dirs = append(dirs, filepath.Dir(m))
	}
	sort.Strings(dirs)
	return dirs, nil
}
