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

type vaultDecryptFileOptions struct {
	vaultPasswordFile string
	dryRun            bool
}

func newVaultDecryptFileCommand() *cobra.Command {
	o := vaultDecryptFileOptions{}

	cmd := &cobra.Command{
		Use:     "decrypt-file FILE",
		Args:    cobra.ExactArgs(1),
		Short:   lang.CmdVaultDecryptFileShort,
		Long:    lang.CmdVaultDecryptFileLong,
		Example: lang.CmdVaultDecryptFileExample,
		RunE:    o.run,
	}

	// Not marked required, for the same reason as on encrypt: the environment variables
	// ResolveVaultPassword falls back to are unreachable if cobra rejects the command first.
	cmd.Flags().StringVar(&o.vaultPasswordFile, MiscVaultPasswordFile, "", lang.CmdVaultEncryptFlagPasswordFile)
	cmd.Flags().BoolVar(&o.dryRun, InstallDryRun, false, lang.CmdVaultDecryptFileFlagDryRun)

	return cmd
}

func (o *vaultDecryptFileOptions) run(cmd *cobra.Command, args []string) error {
	file := args[0]

	password, err := requireVaultPassword(o.vaultPasswordFile)
	if err != nil {
		return err
	}

	src, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("reading %s: %w", file, err)
	}

	decrypted, changed, err := clustercfg.DecryptConfig(src, password)
	if err != nil {
		return err
	}

	if len(changed) > 0 && !o.dryRun {
		// Worth saying plainly: the point of vaulting these values is that the file around them is
		// safe to commit, and this command has just taken that away.
		logger.From(cmd.Context()).Warn("the file now holds these values in plaintext", "file", file, "count", len(changed))
	}

	return finishVaultFile(cmd, file, decrypted, changed, o.dryRun, "decrypted", "nothing to decrypt: no registry credential in this file is encrypted")
}
