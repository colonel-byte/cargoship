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
)

// Examples renders every rke2 example distro.yaml from the shared template
//
// Renders every flavor in exampleFlavors -- one CNI each, into its own directory. Per
// flavor it covers each rke2 tag in thirdparty-src/pins.json, so new examples appear as
// Generate.UpdatePins moves the pins, plus every example directory that flavor already has
// on disk, so an edit to magefiles/templates/rke2-distro.yaml.tmpl reaches the older
// examples too rather than leaving them to drift. Examples are grouped by minor line --
// creating <flavor dir>/<minor>/ as needed -- so one release line's examples stay together.
// Everything that varies between versions is derived from the tag, except the image lists,
// which are fetched from that release's published airgap manifests. Touches the network.
func (Generate) Examples() error {
	pins, err := readEnginePins()
	if err != nil {
		return err
	}

	d, err := pins.distro(exampleDistro)
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

	tmpl, err := parseExampleTemplate(sums)
	if err != nil {
		return err
	}

	for _, f := range exampleFlavors {
		tags, err := exampleTags(d.Tags, f)
		if err != nil {
			return err
		}

		for _, tag := range tags {
			path, err := writeExample(tmpl, d.Repo, tag, f, sums)
			if err != nil {
				return fmt.Errorf("generating %s example for %s: %w", f.cni, tag, err)
			}
			if path == "" {
				fmt.Printf("Skipped %s %s: rpm.rancher.io no longer publishes its RPMs\n", f.cni, tag)
				continue
			}
			fmt.Println("Generated " + path)
		}
	}
	return nil
}
