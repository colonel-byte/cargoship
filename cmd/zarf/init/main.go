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

// Command zarf_init answers Ansible as the zarf_init module.
//
// It is the entry point the colonel_byte.zarf collection invokes. It is deliberately this small:
// it takes over standard output, reads the module's JSON parameters, renders the zarf command line,
// runs the installed zarf, follows the init package's deployment, and writes one JSON object.
// Everything it does is in internal/zarfmod.
//
// This file is a wrapper rather than part of zarf because ZEP-0072's design -- dispatch inside the
// zarf binary itself -- is not something this repository can build. See
// docs/agent/choice-zarf-ansible-module.md for what that costs and what it does not.
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
	// Cancel on the first signal and leave on the second. A cancelled context kills the zarf
	// child, which is the behaviour ZEP-0072 leans toward for a controller that gives up mid-run:
	// an initialisation still mutating a cluster nobody is watching is worse than a partial one a
	// later run reconciles.
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
			"%s is an Ansible module file for zarf. Invoke it as %s%s, or set %s.\n",
			os.Args[0], zarfmod.Prefix, zarfmod.Modules()[0], zarfmod.EnvModule)
		os.Exit(2)
	}

	os.Exit(zarfmod.Run(ctx, name, os.Args, zarfmod.ExecRunner{}))
}
