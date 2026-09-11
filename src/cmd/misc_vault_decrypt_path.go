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
	"fmt"
	"os"

	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/internal/clustercfg"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type vaultDecryptPathOptions struct {
	vaultPasswordFile string
	dryRun            bool
}

func newVaultDecryptPathCommand() *cobra.Command {
	o := vaultDecryptPathOptions{}

	cmd := &cobra.Command{
		Use:     "decrypt-path FILE PATH",
		Args:    cobra.ExactArgs(2),
		Short:   lang.CmdVaultDecryptPathShort,
		Long:    lang.CmdVaultDecryptPathLong,
		Example: lang.CmdVaultDecryptPathExample,
		RunE:    o.run,
	}

	// Not marked required, for the same reason as on encrypt: the environment variables
	// ResolveVaultPassword falls back to are unreachable if cobra rejects the command first.
	cmd.Flags().StringVar(&o.vaultPasswordFile, MiscVaultPasswordFile, "", lang.CmdVaultEncryptFlagPasswordFile)
	cmd.Flags().BoolVar(&o.dryRun, InstallDryRun, false, lang.CmdVaultDecryptPathFlagDryRun)

	return cmd
}

func (o *vaultDecryptPathOptions) run(cmd *cobra.Command, args []string) error {
	file, path := args[0], args[1]

	password, err := requireVaultPassword(o.vaultPasswordFile)
	if err != nil {
		return err
	}

	src, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("reading %s: %w", file, err)
	}

	decrypted, err := clustercfg.DecryptAtPath(src, path, password)
	if err != nil {
		return err
	}

	if o.dryRun {
		_, err := cmd.OutOrStdout().Write(decrypted)
		return err
	}

	if err := writeFileInPlace(file, decrypted); err != nil {
		return err
	}
	// Worth saying plainly: the point of vaulting a value is that the file around it is safe to
	// commit, and this command has just taken that away.
	logger.From(cmd.Context()).Warn("the file now holds this value in plaintext", "file", file, "path", path)
	logger.From(cmd.Context()).Info("decrypted value in place", "file", file, "path", path)
	return nil
}
