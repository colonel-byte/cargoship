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
	"fmt"
	"strings"
	"text/template"

	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/engineconfig"
)

// RenderAll renders every example distro.yaml from the shared templates.
func RenderAll() error {
	pins, err := engineconfig.ReadEnginePins()
	if err != nil {
		return err
	}

	sums, err := loadExampleShasums()
	if err != nil {
		return err
	}
	defer func() {
		if err := sums.save(); err != nil {
			fmt.Println("warning: " + err.Error())
		}
	}()

	for _, spec := range exampleDistros {
		d, err := pins.Distro(spec.name)
		if err != nil {
			return err
		}

		tmpl, err := parseExampleTemplate(spec, sums)
		if err != nil {
			return err
		}

		for _, f := range spec.flavors {
			tags, err := exampleTags(d.Tags, spec, f)
			if err != nil {
				return err
			}

			for _, tag := range tags {
				if err := renderExample(tmpl, d.Repo, tag, spec, f, sums); err != nil {
					return err
				}
			}
		}
	}

	upstream, err := pins.Distro("upstream")
	if err != nil {
		return err
	}
	return renderUpstreamExamples(upstream.Tags, sums)
}

// RenderLine backfills an example for every release on one minor line of a distro.
func RenderLine(distro, prefix string) error {
	if !strings.HasPrefix(prefix, "v") {
		prefix = "v" + prefix
	}

	pins, err := engineconfig.ReadEnginePins()
	if err != nil {
		return err
	}

	sums, err := loadExampleShasums()
	if err != nil {
		return err
	}
	defer func() {
		if err := sums.save(); err != nil {
			fmt.Println("warning: " + err.Error())
		}
	}()

	if distro == "upstream" {
		d, err := pins.Distro("upstream")
		if err != nil {
			return err
		}
		return renderUpstreamLine(d.Repo, prefix, sums)
	}

	spec, err := exampleDistroByName(distro)
	if err != nil {
		return err
	}

	d, err := pins.Distro(spec.name)
	if err != nil {
		return err
	}

	tmpl, err := parseExampleTemplate(spec, sums)
	if err != nil {
		return err
	}

	tags, err := engineconfig.RemoteTags(d.Repo, prefix)
	if err != nil {
		return err
	}
	if err := sortTagsDesc(tags); err != nil {
		return err
	}
	// A backfill names its own line, so asking for one that has aged out is worth saying
	// rather than quietly rendering nothing.
	tags, err = aboveFloor(tags)
	if err != nil {
		return err
	}
	if len(tags) == 0 {
		return fmt.Errorf("no %s releases on %s at or above the %s floor", distro, prefix, exampleMinorFloor)
	}

	for _, f := range spec.flavors {
		flavorTags, err := filterFlavorTags(tags, f)
		if err != nil {
			return err
		}

		for _, tag := range flavorTags {
			if err := renderExample(tmpl, d.Repo, tag, spec, f, sums); err != nil {
				return err
			}
		}
	}
	return nil
}

func renderExample(tmpl *template.Template, repoURL, tag string, spec exampleDistroSpec, f exampleFlavor, sums *exampleShasums) error {
	path, err := writeExample(tmpl, repoURL, tag, spec, f, sums)
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

func exampleDistroByName(name string) (exampleDistroSpec, error) {
	for _, spec := range exampleDistros {
		if spec.name == name {
			return spec, nil
		}
	}
	return exampleDistroSpec{}, fmt.Errorf("unknown distro %q, expected one of: k3s, rke2, upstream", name)
}
