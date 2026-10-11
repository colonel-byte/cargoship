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

// Package microvmrun is the presentation half of the Dev VM targets: it drives
// internal/microvm and formats what that package returns for a terminal.
//
// It is separate from internal/microvm because that package has a second consumer, the e2e
// cluster suite, which wants the fleet and not a status table. Keeping the printing here
// leaves the mage targets as thin as magefiles/AGENTS.md asks, without pushing terminal
// formatting into a package the test suite imports.
package microvmrun

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/colonel-byte/cargoship/internal/microvm"
)

// Options is what dev:vmUp was asked for, after its flag defaults have been applied.
type Options struct {
	// Controllers, Workers and Infra are how many of each group to create. Infra nodes join
	// as workers under a profile of their own.
	Controllers int
	Workers     int
	Infra       int
	// Distro is the engine the fleet is prepared for, k3s or rke2.
	Distro string
}

// Up brings up a fleet and prints what to do with it.
func Up(ctx context.Context, opts Options) error {
	fmt.Printf("Bringing up %s for %s\n", describe(opts), opts.Distro)

	fleet, err := microvm.Up(ctx, microvm.Spec{
		Controllers: opts.Controllers,
		Workers:     opts.Workers,
		Infra:       opts.Infra,
		Distro:      opts.Distro,
	})
	if err != nil {
		return err
	}

	if err := printStatus(fleet.Name, ""); err != nil {
		return err
	}
	fmt.Printf("\nInventory: %s\n", fleet.InventoryPath())
	fmt.Printf("Point cargoship at it with:\n  cargoship apply --config %s <package>\n", fleet.InventoryPath())
	fmt.Printf("Open a shell on a node with:\n  mage dev:vmShell %s\n", fleet.Nodes[0].Name)
	fmt.Printf("Take it away with:\n  mage dev:vmDown\n")
	return nil
}

// describe names the groups that were asked for, leaving out the ones that are empty so that
// the common case does not report "and 0 infra".
func describe(opts Options) string {
	parts := []string{
		fmt.Sprintf("%d control-plane", opts.Controllers),
		fmt.Sprintf("%d worker", opts.Workers),
	}
	if opts.Infra > 0 {
		parts = append(parts, fmt.Sprintf("%d infra", opts.Infra))
	}
	return strings.Join(parts, ", ") + " node(s)"
}

// Down tears a fleet down. An empty fleet name means the default one.
func Down(fleet string) error {
	fmt.Printf("Tearing down the %s fleet\n", name(fleet))
	if err := microvm.Down(fleet, ""); err != nil {
		return err
	}
	fmt.Println("Done; nothing of it is left on this machine")
	return nil
}

// List prints a fleet's nodes and whether each is running.
func List(fleet string) error {
	return printStatus(fleet, "")
}

// SSH runs ssh against one node with the caller's terminal attached.
func SSH(ctx context.Context, fleet, node string, command ...string) error {
	cmd, err := microvm.SSHCommand(ctx, fleet, "", node, command...)
	if err != nil {
		return err
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Image fetches and verifies the base image without starting anything.
func Image(ctx context.Context) error {
	path, err := microvm.Fetch(ctx)
	if err != nil {
		return err
	}
	fmt.Println("Base image ready at " + path)
	return nil
}

// name is a fleet name for reporting, standing in the default where none was given. The
// default itself belongs to internal/microvm, which is what resolves an empty name.
func name(fleet string) string {
	if fleet == "" {
		return "dev"
	}
	return fleet
}

// printStatus writes the status table.
func printStatus(fleet, runRoot string) error {
	status, err := microvm.List(fleet, runRoot)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NODE\tROLE\tPROFILE\tSTATE\tSSH\tPRIVATE ADDRESS")
	for _, s := range status {
		state := "stopped"
		if s.Running {
			state = "running (pid " + strconv.Itoa(s.PID) + ")"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t127.0.0.1:%d\t%s\n",
			s.Name, s.Role, s.Profile, state, s.SSHPort, s.PrivateAddress)
	}
	return w.Flush()
}
