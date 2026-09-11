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
)

// MiscVaultNewPasswordFile flag
const MiscVaultNewPasswordFile = "new-vault-password-file"

type vaultRekeyOptions struct {
	vaultPasswordFile    string
	newVaultPasswordFile string
	dryRun               bool
}

func newVaultRekeyCommand() *cobra.Command {
	o := vaultRekeyOptions{}

	cmd := &cobra.Command{
		Use:     "rekey FILE",
		Args:    cobra.ExactArgs(1),
		Short:   lang.CmdVaultRekeyShort,
		Long:    lang.CmdVaultRekeyLong,
		Example: lang.CmdVaultRekeyExample,
		RunE:    o.run,
	}

	// The old password is resolved the way every other vault command resolves its one password,
	// environment fallbacks included, because that is where the password a file is already vaulted
	// under is likely to be sitting.
	cmd.Flags().StringVar(&o.vaultPasswordFile, MiscVaultPasswordFile, "", lang.CmdVaultEncryptFlagPasswordFile)
	cmd.Flags().StringVar(&o.newVaultPasswordFile, MiscVaultNewPasswordFile, "", lang.CmdVaultRekeyFlagNewPasswordFile)
	cmd.Flags().BoolVar(&o.dryRun, InstallDryRun, false, lang.CmdVaultRekeyFlagDryRun)

	return cmd
}

func (o *vaultRekeyOptions) run(cmd *cobra.Command, args []string) error {
	file := args[0]

	oldPassword, err := requireVaultPassword(o.vaultPasswordFile)
	if err != nil {
		return err
	}

	newPassword, err := resolveNewVaultPassword(o.newVaultPasswordFile, oldPassword)
	if err != nil {
		return err
	}

	src, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("reading %s: %w", file, err)
	}

	rekeyed, changed, err := clustercfg.RekeyConfig(src, oldPassword, newPassword)
	if err != nil {
		return err
	}

	// Rekeying onto the password the file already carries re-salts every value and leaves it
	// readable with the same password as before. That is a supported thing to ask for, but it is
	// not a rotation, so it is reported as what it is rather than as a password change that did
	// not happen.
	verb, nothingToDo := "rekeyed", "nothing to rekey: no registry credential in this file is encrypted"
	if newPassword == oldPassword {
		verb, nothingToDo = "re-salted", "nothing to re-salt: no registry credential in this file is encrypted"
	}

	return finishVaultFile(cmd, file, rekeyed, changed, o.dryRun, verb, nothingToDo)
}

// resolveNewVaultPassword reads the password to rekey onto, which unlike every other vault password
// comes only from the file named. The environment variables ResolveVaultPassword falls back to hold
// the password a configuration is vaulted under now, so honouring them here would turn an omitted
// flag into a silent rotation onto the password the file started with.
//
// An omitted flag instead means the old password, which re-salts every value under the password the
// file already uses -- a fresh salt and fresh ciphertext for a configuration whose password is
// fine but whose ciphertext an operator would rather not keep, without the plaintext ever reaching
// disk. Naming a file holding the old password does the same thing, and is reported the same way.
func resolveNewVaultPassword(passwordFile, oldPassword string) (string, error) {
	if passwordFile == "" {
		return oldPassword, nil
	}

	// Going through ResolveVaultPassword is safe only because the empty case is handled above: given
	// a file it reads that file and never consults the environment. Reusing it keeps the trailing
	// newline handling identical to every other password cargoship reads.
	password, err := clustercfg.ResolveVaultPassword(passwordFile)
	if err != nil {
		return "", err
	}
	if password == "" {
		return "", fmt.Errorf("the file named by --%s holds no password", MiscVaultNewPasswordFile)
	}
	return password, nil
}
