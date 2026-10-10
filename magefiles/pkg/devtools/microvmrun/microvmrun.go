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
	"text/tabwriter"

	"github.com/colonel-byte/cargoship/internal/microvm"
)

// Up brings up a fleet and prints what to do with it.
func Up(ctx context.Context, controllers, workers int, distro string) error {
	fmt.Printf("Bringing up %d controller(s) and %d worker(s) for %s\n", controllers, workers, distro)

	fleet, err := microvm.Up(ctx, microvm.Spec{
		Controllers: controllers,
		Workers:     workers,
		Distro:      distro,
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

// Down tears the fleet down.
func Down() error {
	fmt.Println("Tearing down the dev fleet")
	if err := microvm.Down("", ""); err != nil {
		return err
	}
	fmt.Println("Done; nothing of it is left on this machine")
	return nil
}

// List prints the fleet's nodes and whether each is running.
func List() error {
	return printStatus("", "")
}

// SSH runs ssh against one node with the caller's terminal attached.
func SSH(ctx context.Context, node string, command ...string) error {
	cmd, err := microvm.SSHCommand(ctx, "", "", node, command...)
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

// printStatus writes the status table.
func printStatus(fleet, runRoot string) error {
	status, err := microvm.List(fleet, runRoot)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NODE\tROLE\tSTATE\tSSH\tPRIVATE ADDRESS")
	for _, s := range status {
		state := "stopped"
		if s.Running {
			state = "running (pid " + strconv.Itoa(s.PID) + ")"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t127.0.0.1:%d\t%s\n",
			s.Name, s.Role, state, s.SSHPort, s.PrivateAddress)
	}
	return w.Flush()
}
