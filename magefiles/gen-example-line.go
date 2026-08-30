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
	"strings"
)

// ExampleLine renders an example for every release on one rke2 minor line
//
// Where Generate.Examples re-renders what is already pinned or already on disk, this
// backfills a whole line: it lists every non-RC rke2 tag on the given minor line and
// renders each one, so "v1.36" covers v1.36.0+rke2r1 through the newest v1.36 release. The
// examples land in each flavor's <flavor dir>/<minor>/, and Generate.Examples keeps them
// current from then on, since it covers every example directory on disk. Touches the network.
//
//	mage generate:exampleLine v1.36
func (Generate) ExampleLine(prefix string) error {
	// Accept "1.36" as well as "v1.36" -- upstream tags carry the v, but the minor line is
	// as often written without it.
	if !strings.HasPrefix(prefix, "v") {
		prefix = "v" + prefix
	}

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

	tags, err := remoteTags(d.Repo, prefix)
	if err != nil {
		return err
	}
	if err := sortTagsDesc(tags); err != nil {
		return err
	}

	for _, f := range exampleFlavors {
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
