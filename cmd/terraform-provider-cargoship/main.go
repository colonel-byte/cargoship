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

// Command terraform-provider-cargoship is the OpenTofu provider that drives cargoship: the
// configuration describes the fleet, cargoship owns the phase ordering, and an apply converges the
// cluster.
//
// The provider itself is internal/tofuprovider. This file is the plugin entry point and nothing
// else, which is also what keeps it out of the CLI: it is a package of this module, and the linker
// never loads a package the import graph does not reach -- see
// docs/agent/choice-tofu-provider-layout.md, and cmd/cargoship/deps_test.go, which holds that
// nothing here is reachable from the CLI.
//
// What it serves so far is read-only: one data source, cargoship_cluster_facts. The resource that
// converges a cluster comes next; see docs/agent/design-tofu-provider-install.md and
// docs/dev/tofu-provider.md for how to run what exists.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/colonel-byte/cargoship/internal/tofuprovider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

// version is overwritten at build time with the release the binary was cut from.
var version = "dev"

// address is how a configuration names this provider. OpenTofu resolves a provider by source
// address, and an oci_mirror maps that address onto a repository rather than replacing it, so the
// registry hostname is part of the name even for a provider that is never published to a registry.
// See docs/agent/choice-tofu-provider-layout.md.
const address = "registry.opentofu.org/colonel-byte/cargoship"

func main() {
	// --debug runs the provider so a debugger can attach to it, which is the only way to step
	// through a provider: OpenTofu launches it as a child process over a plugin protocol, so
	// there is no `go run` path into it.
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for a debugger to attach")
	flag.Parse()

	err := providerserver.Serve(context.Background(), tofuprovider.New(version), providerserver.ServeOpts{
		Address: address,
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
