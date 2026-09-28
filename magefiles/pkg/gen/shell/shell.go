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
		path: filepath.Join("hack", "completion", "cargoship.bash"),
		gen: func(c *cobra.Command, w io.Writer) error {
			return c.GenBashCompletion(w)
		},
	},
	{
		name: "zsh",
		path: filepath.Join("hack", "completion", "cargoship.zsh"),
		gen: func(c *cobra.Command, w io.Writer) error {
			return c.GenZshCompletion(w)
		},
	},
	{
		name: "fish",
		path: filepath.Join("hack", "completion", "cargoship.fish"),
		gen: func(c *cobra.Command, w io.Writer) error {
			return c.GenFishCompletion(w, true)
		},
	},
	{
		name: "powershell",
		path: filepath.Join("hack", "completion", "cargoship.ps1"),
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
		fmt.Println(t.path)
		if err := generate(cargo, t); err != nil {
			return err
		}
	}

	return nil
}

// generate creates t.path and writes the completion script t.gen produces for cargo into it.
func generate(cargo *cobra.Command, t completionTarget) (err error) {
	// The scripts live in a directory of their own, which a fresh checkout has but a tree that
	// has never run this target may not.
	if err := os.MkdirAll(filepath.Dir(t.path), 0o775); err != nil {
		return fmt.Errorf("failed to create the directory for %s: %w", t.path, err)
	}

	f, err := os.Create(t.path)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", t.path, err)
	}
	// A close failure is the flush failing, which means the script on disk is truncated. Report
	// it through the named return rather than dropping it, but never over a generation error.
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close %s: %w", t.path, cerr)
		}
	}()

	if err := t.gen(cargo, f); err != nil {
		return fmt.Errorf("failed to generate %s completion: %w", t.name, err)
	}

	return nil
}
