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

// Command zarf_package_deploy answers Ansible as the zarf_package_deploy module.
//
// It is the second entry point the colonel_byte.zarf collection invokes, and it is the same shape
// as cmd/zarf/init: take over standard output, read the module's JSON parameters, render the zarf
// command line, run the installed zarf, follow the deployment, and write one JSON object.
// Everything it does is in internal/zarfmod.
//
// Deploy exists beside init because neither is enough on its own. A cluster whose init package
// disables k3s local-storage has no StorageClass for zarf's registry to claim, so the registry
// cannot be initialised until a storage provider has been deployed, and the provider cannot be
// deployed until the cluster has been initialised far enough to hold it. The playbook in
// test/e2e/zarf/testdata walks that around: a seed-only init, the provider, then the rest of init.
// It is modeled on the uds-bundle at
// https://github.com/colonel-byte/uds-bundles/blob/main/upstream/init-local-path/uds-bundle.yaml.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/colonel-byte/cargoship/internal/zarfmod"
)

func main() {
	// Cancel on the first signal and leave on the second, as cmd/zarf/init does and for the same
	// reason: a deployment still mutating a cluster nobody is watching is worse than a partial one
	// a later run reconciles.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		first := true
		for {
			<-signalCh
			if first {
				first = false
				cancel()
				continue
			}
			os.Exit(1)
		}
	}()

	name, ok := zarfmod.ModuleName(os.Args[0])
	if !ok {
		// Invoked under a name that is not a module. Say what the binary is for on stderr rather
		// than emitting a JSON object a human did not ask for.
		fmt.Fprintf(os.Stderr,
			"%s is an Ansible module file for zarf. Invoke it as %spackage_deploy, or set %s.\n",
			os.Args[0], zarfmod.Prefix, zarfmod.EnvModule)
		os.Exit(2)
	}

	os.Exit(zarfmod.Run(ctx, name, os.Args, zarfmod.ExecRunner{}))
}
