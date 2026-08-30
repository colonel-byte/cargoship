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

// ExampleLine renders an example for every release on one minor line of a distro
//
// Where Generate.Examples re-renders what is already pinned or already on disk, this
// backfills a whole line: it lists every non-RC tag of that distro on the given minor line
// and renders each one, so "rke2 v1.36" covers v1.36.0+rke2r1 through the newest v1.36
// release. The examples land in each of that distro's flavor directories, and
// Generate.Examples keeps them current from then on, since it covers every example directory
// on disk. Touches the network.
//
//	mage generate:exampleLine rke2 v1.36
//	mage generate:exampleLine k3s 1.36
func (Generate) ExampleLine(distro, prefix string) error {
	// Accept "1.36" as well as "v1.36" -- upstream tags carry the v, but the minor line is
	// as often written without it.
	if !strings.HasPrefix(prefix, "v") {
		prefix = "v" + prefix
	}

	spec, err := exampleDistroByName(distro)
	if err != nil {
		return err
	}

	pins, err := readEnginePins()
	if err != nil {
		return err
	}

	d, err := pins.distro(spec.name)
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

	tmpl, err := parseExampleTemplate(spec, sums)
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

	for _, f := range spec.flavors {
		for _, tag := range tags {
			if err := renderExample(tmpl, d.Repo, tag, spec, f, sums); err != nil {
				return err
			}
		}
	}
	return nil
}

// exampleDistroByName finds the spec a distro name asks for, listing the names that do work
// when it is not one of them.
func exampleDistroByName(name string) (exampleDistroSpec, error) {
	var names []string
	for _, spec := range exampleDistros {
		if spec.name == name {
			return spec, nil
		}
		names = append(names, spec.name)
	}
	return exampleDistroSpec{}, fmt.Errorf("unknown distro %q, want one of: %s", name, strings.Join(names, ", "))
}
