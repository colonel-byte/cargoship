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
// The OpenTofu provider used to be a package of this module, and this test is what kept its
// dependencies out of the binary: the linker never loads a package the import graph does not
// reach, so terraform-plugin-framework and its tree ended up in no cargoship binary. The provider
// now lives in colonel-byte/terraform-provider-cargoship and consumes this module as a library,
// so the two packages it owned are gone from the list.
//
// The plugin framework stays, and the test with it. What it protects did not leave with the
// provider: a provider-shaped dependency reaching the CLI costs an air-gapped operator megabytes
// they carry by hand, and the only symptom is a larger binary, which no check reads. See the
// size budget in AGENTS.md. A pattern matching nothing is the state this should be in.
//
// Prefixes rather than exact paths, so a package added under the tree is covered without this
// list being updated. One entry per line, because keep-sorted sorts lines: a multi-line struct
// literal between its markers is reordered into something that does not compile.
var forbiddenDeps = map[string]string{
	// keep-sorted start
	"github.com/hashicorp/terraform-plugin": "a plugin framework and its protocol libraries",
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
