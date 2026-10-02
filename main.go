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

// Package main is the development entry point for the cargoship CLI, so that `go run . <command>`
// keeps working from a checkout.
//
// The released binary is built from ./cmd/cargoship, and that is the one every artifact comes
// from: .goreleaser.yaml, magefiles/pkg/build.Binary, the container images, and the e2e suites all
// name it. The repository builds more than one binary now -- the cargoship CLI and the zarf Ansible
// module files under ./cmd/zarf -- and Go allows one main package per directory, so the released
// entry point moved into a directory of its own rather than staying at the root.
//
// This file is a convenience, not a second product. It runs the same command tree against the same
// configurers; what it deliberately leaves out is the Ansible module dispatch that
// ./cmd/cargoship/main.go runs before Cobra:
//
//   - A module is selected by the name the binary was invoked under -- a cargoship_<action>
//     symlink to the installed binary. `go run .` compiles to a temporary file named after this
//     package, so it can never carry such a name, and the branch would be unreachable here.
//   - The CARGOSHIP_ANSIBLE_MODULE environment variable does select a module without a symlink,
//     and that is the path to reproduce a module run by hand. It has no effect through this entry
//     point. Use `go run ./cmd/cargoship` for that, or the built binary.
//
// See the package documentation of internal/ansiblemod and docs/agent/choice-ansible-module.md for
// what the module path does, and cmd/ansible.go for where it is entered.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/colonel-byte/cargoship/cmd"

	// anonymous import is needed to load the os configurers
	_ "github.com/colonel-byte/cargoship/types/os"
	// anonymous import is needed to load the os configurers
	_ "github.com/colonel-byte/cargoship/types/os/linux"
	// anonymous import is needed to load the os configurers
	_ "github.com/colonel-byte/cargoship/types/os/linux/enterpriselinux"
	// anonymous import is needed to load the distro configurers
	_ "github.com/colonel-byte/cargoship/types/distrocfg"
)

func main() {
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

	if err := cmd.Execute(ctx); err != nil {
		os.Exit(1)
	}
}
