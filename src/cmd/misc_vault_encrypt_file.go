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

type vaultEncryptFileOptions struct {
	vaultPasswordFile string
	dryRun            bool
	force             bool
}

func newVaultEncryptFileCommand() *cobra.Command {
	o := vaultEncryptFileOptions{}

	cmd := &cobra.Command{
		Use:     "encrypt-file FILE",
		Args:    cobra.ExactArgs(1),
		Short:   lang.CmdVaultEncryptFileShort,
		Long:    lang.CmdVaultEncryptFileLong,
		Example: lang.CmdVaultEncryptFileExample,
		RunE:    o.run,
	}

	// Not marked required, for the same reason as on encrypt: the environment variables
	// ResolveVaultPassword falls back to are unreachable if cobra rejects the command first.
	cmd.Flags().StringVar(&o.vaultPasswordFile, MiscVaultPasswordFile, "", lang.CmdVaultEncryptFlagPasswordFile)
	cmd.Flags().BoolVar(&o.dryRun, InstallDryRun, false, lang.CmdVaultEncryptFileFlagDryRun)
	cmd.Flags().BoolVar(&o.force, MiscVaultForce, false, lang.CmdVaultEncryptFileFlagForce)

	return cmd
}

func (o *vaultEncryptFileOptions) run(cmd *cobra.Command, args []string) error {
	file := args[0]

	password, err := requireVaultPassword(o.vaultPasswordFile)
	if err != nil {
		return err
	}

	src, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("reading %s: %w", file, err)
	}

	encrypted, changed, err := clustercfg.EncryptConfig(src, password, o.force)
	if err != nil {
		return err
	}

	return finishVaultFile(cmd, file, encrypted, changed, o.dryRun, "encrypted", "nothing to encrypt: every registry credential is encrypted already, or there are none to encrypt")
}

// finishVaultFile reports what a whole-file rewrite did and writes the result, which encrypt-file
// and decrypt-file do the same way.
//
// A run that changed nothing still prints the document under --dry-run, so that piping the output
// somewhere does not depend on whether there happened to be anything to do.
func finishVaultFile(cmd *cobra.Command, file string, doc []byte, changed []string, dryRun bool, verb, nothingToDo string) error {
	l := logger.From(cmd.Context())

	if dryRun {
		_, err := cmd.OutOrStdout().Write(doc)
		return err
	}

	if len(changed) == 0 {
		l.Info(nothingToDo, "file", file)
		return nil
	}

	if err := writeFileInPlace(file, doc); err != nil {
		return err
	}
	for _, path := range changed {
		l.Info(verb+" value in place", "file", file, "path", path)
	}
	l.Info(verb+" registry credentials", "file", file, "count", len(changed))
	return nil
}
