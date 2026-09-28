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

// Package shell generates cargoship's shell tab completion scripts.
package shell

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/colonel-byte/cargoship/cmd"
	"github.com/spf13/cobra"
)

// completionTarget describes one shell's completion script: where it is written and how it is
// generated from the root command.
type completionTarget struct {
	name string
	path string
	gen  func(c *cobra.Command, w io.Writer) error
}

// completionTargets is the complete set of shells cargoship generates completion scripts for.
var completionTargets = []completionTarget{
	{
		name: "bash",
		path: filepath.Join("hack", "cargoship.bash"),
		gen: func(c *cobra.Command, w io.Writer) error {
			return c.GenBashCompletion(w)
		},
	},
	{
		name: "zsh",
		path: filepath.Join("hack", "cargoship.zsh"),
		gen: func(c *cobra.Command, w io.Writer) error {
			return c.GenZshCompletion(w)
		},
	},
	{
		name: "fish",
		path: filepath.Join("hack", "cargoship.fish"),
		gen: func(c *cobra.Command, w io.Writer) error {
			return c.GenFishCompletion(w, true)
		},
	},
	{
		name: "powershell",
		path: filepath.Join("hack", "cargoship.ps1"),
		gen: func(c *cobra.Command, w io.Writer) error {
			return c.GenPowerShellCompletionWithDesc(w)
		},
	},
}

// GenerateShell writes cargoship's bash, zsh, fish, and PowerShell tab completion scripts to
// their respective paths under hack/.
func GenerateShell() error {
	cargo := cmd.NewCargoshipCommand()

	for _, t := range completionTargets {
		if err := generate(cargo, t); err != nil {
			return err
		}
	}

	return nil
}

// generate creates t.path and writes the completion script t.gen produces for cargo into it.
func generate(cargo *cobra.Command, t completionTarget) error {
	f, err := os.Create(t.path)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", t.path, err)
	}
	defer f.Close()

	if err := t.gen(cargo, f); err != nil {
		return fmt.Errorf("failed to generate %s completion: %w", t.name, err)
	}

	return nil
}
