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
	"github.com/colonel-byte/cargoship/magefiles/pkg/engine/config"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/docs"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/schema"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/shell"
)

// Document creates the docs for this repo
func (Generate) Document() error {
	return docs.Generate()
}

// EngineConfig generates config.yaml structs from thirdparty-src/
//
// Statically parses the k3s/RKE2/upstream source committed under thirdparty-src/ (see
// Generate.PullEngineSource) into a Go struct per distro/version/target. Offline: it
// never touches the network or imports k3s/RKE2/upstream as a module -- see
// docs/agent/design-config-codegen.md.
func (Generate) EngineConfig() error {
	return config.Generate()
}

// ShellCompletion writes cargoship's bash, zsh, fish and PowerShell tab completion scripts
// under hack/.
func (Generate) ShellCompletion() error {
	return shell.GenerateShell()
}

// Schema creates the jsonschema files for a number of the yaml files
func (Generate) Schema() error {
	return schema.Generate()
}
