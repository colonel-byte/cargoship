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

package main

import (
	"fmt"
	"text/template"
)

// Examples renders every example distro.yaml from the shared templates
//
// Renders every flavor of every distro in exampleDistros -- one CNI each, into its own
// directory. Per flavor it covers each of that distro's tags in thirdparty-src/pins.json,
// so new examples appear as Generate.UpdatePins moves the pins, plus every example directory
// that flavor already has on disk, so an edit to a template reaches the older examples too
// rather than leaving them to drift. Examples are grouped by minor line -- creating
// <flavor dir>/<minor>/ as needed -- so one release line's examples stay together.
// Everything that varies between versions is derived from the tag, except the image lists
// and digests, which come from that release's published assets. Touches the network.
func (Generate) Examples() error {
	pins, err := readEnginePins()
	if err != nil {
		return err
	}

	sums, err := loadExampleShasums()
	if err != nil {
		return err
	}
	// Save whatever was hashed even if a later version fails, so a long first run is not
	// thrown away.
	defer func() {
		if err := sums.save(); err != nil {
			fmt.Println("warning: " + err.Error())
		}
	}()

	for _, spec := range exampleDistros {
		d, err := pins.distro(spec.name)
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
	return nil
}

// renderExample writes one example and reports what it did, since a build upstream has
// dropped is skipped rather than written.
func renderExample(tmpl *template.Template, repoURL, tag string, spec exampleDistroSpec, f exampleFlavor, sums *exampleShasums) error {
	path, err := writeExample(tmpl, repoURL, tag, spec, f, sums)
	if err != nil {
		return fmt.Errorf("generating %s %s example for %s: %w", spec.name, f.cni, tag, err)
	}
	if path == "" {
		fmt.Printf("Skipped %s %s: upstream no longer publishes what it installs\n", f.cni, tag)
		return nil
	}
	fmt.Println("Generated " + path)
	return nil
}
