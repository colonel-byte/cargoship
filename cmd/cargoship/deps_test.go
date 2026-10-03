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
	"os/exec"
	"strings"
	"testing"
)

// forbiddenDeps are packages the CLI must not reach.
//
// The OpenTofu provider is a package of this module rather than a module of its own, and what
// makes that free for the CLI is that the linker never loads a package the import graph does not
// reach: terraform-plugin-framework and its dependencies are vendored for the provider and end up
// in no cargoship binary. That is a property of the import graph, not a boundary anything
// enforces, so one import from a package the CLI already uses would quietly undo it -- and the
// only symptom would be a larger binary, which no check reads. See
// docs/agent/choice-tofu-provider-layout.md.
//
// Prefixes rather than exact paths, so a package added under either tree is covered without this
// list being updated. One entry per line, because keep-sorted sorts lines: a multi-line struct
// literal between its markers is reordered into something that does not compile.
var forbiddenDeps = map[string]string{
	// keep-sorted start
	"github.com/colonel-byte/cargoship/cmd/terraform-provider-cargoship": "the provider's own main package",
	"github.com/colonel-byte/cargoship/internal/tofuprovider":            "the provider's implementation",
	"github.com/hashicorp/terraform-plugin":                              "the provider's plugin framework and its protocol libraries",
	// keep-sorted end
}

// TestCargoshipDoesNotDependOnTheProvider pins the import graph the size of the CLI binary rests
// on. It shells out to go list rather than walking imports itself, because the transitive graph is
// the question and go list already answers it the way the linker sees it.
func TestCargoshipDoesNotDependOnTheProvider(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list -deps .: %v", err)
	}

	deps := strings.Fields(string(out))
	if len(deps) == 0 {
		t.Fatal("go list -deps . returned no packages, so this test proved nothing")
	}

	for _, dep := range deps {
		for prefix, why := range forbiddenDeps {
			if strings.HasPrefix(dep, prefix) {
				t.Errorf(
					"cmd/cargoship depends on %s (%s): the provider's dependencies would ship in the CLI binary",
					dep, why,
				)
			}
		}
	}
}
