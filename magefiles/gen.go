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

//go:build mage
// +build mage

package main

import (
	"fmt"

	"github.com/colonel-byte/cargoship/magefiles/pkg/engine/source"
	"github.com/colonel-byte/cargoship/magefiles/pkg/example"
	"github.com/magefile/mage/mg"
)

type (
	Generate mg.Namespace
)

// Examples renders every example distro.yaml from the shared templates
//
// Renders every flavor of every distro -- one CNI each, into its own directory. Per flavor it
// covers each of that distro's tags in thirdparty-src/pins.json, so new examples appear as
// Generate.UpdatePins moves the pins, plus every example directory that flavor already has on
// disk, so an edit to a template reaches the older examples too rather than leaving them to
// drift. Touches the network.
func (Generate) Examples() error {
	return example.GenerateAll()
}

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
//	mage generate:exampleLine upstream v1.37
func (Generate) ExampleLine(distro, prefix string) error {
	return example.GenerateLine(distro, prefix)
}

// LatestTag pins the newest non-RC tag for a distro ("k3s" or "rke2") matching a
// major.minor prefix (e.g. "v1.35")
//
// Resolves the tag, prints it, and writes it into thirdparty-src/pins.json -- replacing
// whatever that minor line held, or adding the line if it is new. When the pin moves it
// re-pulls that version's source so pins.json and thirdparty-src/ never disagree. Touches
// the network.
//
//	mage generate:latestTag rke2 v1.35
func (Generate) LatestTag(distro, prefix string) error {
	tag, err := source.LatestTag(distro, prefix)
	if err != nil {
		return err
	}

	fmt.Println(tag)
	return nil
}

// PullEngineSource fetches pinned k3s/RKE2 source files
//
// Fetches the raw files pinned in thirdparty-src/pins.json at their exact tags and
// commits them under thirdparty-src/ as plain text -- never as a Go module dependency
// (their go.mod replace directives make that unsafe). This is the only step in the
// engine-config codegen pipeline that touches the network; Generate.EngineConfig runs
// fully offline against what this writes.
func (Generate) PullEngineSource() error {
	return source.PullAll()
}
