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

// Command terraform-provider-cargoship will be the OpenTofu provider that exposes cargoship as a
// cargoship_cluster resource: the configuration describes the fleet, cargoship owns the phase
// ordering, and a tofu apply converges the cluster.
//
// It is a package of this module rather than a module of its own, which costs the CLI nothing in
// the binary it ships: the linker never loads a package the import graph does not reach, so
// nothing here reaches cmd/cargoship. What it does cost is the vendor tree, once this package
// takes a dependency on terraform-plugin-framework. See
// docs/agent/choice-tofu-provider-layout.md, and docs/agent/choice-tofu-secrets.md for the
// decisions the schema has to honour before any attribute name is published.
//
// Nothing is implemented yet. The main function exists because the release plumbing needs
// something to build: a provider is distributed as a binary, so the packaging and publishing path
// is testable only against one. It refuses to serve rather than serving an empty provider, because
// a provider that starts and then describes no resources is harder to diagnose than one that says
// it is not finished.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr,
		"terraform-provider-cargoship is not implemented yet: see https://github.com/colonel-byte/cargoship/issues/302")
	os.Exit(1)
}
