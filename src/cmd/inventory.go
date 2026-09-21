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

package cmd

import (
	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/spf13/cobra"
)

// newInventoryCommand groups the commands that produce a cluster inventory rather than consume
// one. It runs nothing itself.
func newInventoryCommand() *cobra.Command {
	f := &keyFlags{}

	cmd := &cobra.Command{
		Use:   "inventory",
		Short: lang.CmdInventoryShort,
		Long:  lang.CmdInventoryLong,
	}

	cmd.PersistentFlags().StringVar(&f.vaultPasswordFile, MiscVaultPasswordFile, "", lang.CmdVaultEncryptFlagPasswordFile)
	addAgeFlags(cmd, f)

	cmd.AddCommand(newInventoryFromAnsibleCommand(f))

	return cmd
}
