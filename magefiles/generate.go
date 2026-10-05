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
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/completion"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/docs"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/engineconfig"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/examples"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/schema"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/zarfflags"
	"github.com/magefile/mage/mg"
)

const completionDir = "hack/completion"

type (
	Generate mg.Namespace
)

// Document creates the docs for this repo
func (Generate) Document() error {
	return docs.GenerateDocument()
}

// Schema creates the jsonschema files for a number of the yaml files
func (Generate) Schema() error {
	return schema.GenerateSchemas()
}

// ZarfFlags records the flag surface of the zarf commands the Ansible modules wrap, read from the
// installed zarf. Set CARGOSHIP_ZARF_BINARY to name one that is not on PATH.
func (Generate) ZarfFlags() error {
	return zarfflags.Generate()
}

// Completion generates shell completion scripts for cargoship into hack/completion
func (Generate) Completion() error {
	return completion.Generate(completionDir)
}

// EngineConfig generates config.yaml structs from thirdparty-src/
func (Generate) EngineConfig() error {
	return engineconfig.GenerateEngineConfig()
}

// LatestTag pins the newest non-RC tag for a distro ("k3s" or "rke2") matching a
// major.minor prefix (e.g. "v1.35")
func (Generate) LatestTag(distro, prefix string) error {
	pins, err := engineconfig.ReadEnginePins()
	if err != nil {
		return err
	}
	_, err = engineconfig.PinTag(&pins, distro, prefix)
	return err
}

// PullEngineSource fetches pinned k3s/RKE2 source files
func (Generate) PullEngineSource() error {
	return engineconfig.PullAllEngineSources()
}

// UpdatePins refreshes every pinned minor line to its newest non-RC patch release
func (Generate) UpdatePins() error {
	return engineconfig.UpdateAllPins()
}

// Examples renders every example distro.yaml from the shared templates
func (Generate) Examples() error {
	return examples.RenderAll()
}

// ExampleLine renders an example for every release on one minor line of a distro
func (Generate) ExampleLine(distro, prefix string) error {
	return examples.RenderLine(distro, prefix)
}
