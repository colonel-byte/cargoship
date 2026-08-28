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
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/internal/clustercfg"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// MiscVaultPasswordFile flag
const MiscVaultPasswordFile = "vault-password-file"

type vaultEncryptOptions struct {
	vaultPasswordFile string
}

func newVaultEncryptCommand() *cobra.Command {
	o := vaultEncryptOptions{}

	cmd := &cobra.Command{
		Use:   "vault-encrypt [VALUE]",
		Args:  cobra.MaximumNArgs(1),
		Short: lang.CmdVaultEncryptShort,
		Long:  lang.CmdVaultEncryptLong,
		RunE:  o.run,
	}

	cmd.Flags().StringVar(&o.vaultPasswordFile, MiscVaultPasswordFile, "", lang.CmdVaultEncryptFlagPasswordFile)
	cmd.MarkFlagRequired(MiscVaultPasswordFile)

	return cmd
}

func (o *vaultEncryptOptions) run(cmd *cobra.Command, args []string) error {
	password, err := clustercfg.ResolveVaultPassword(o.vaultPasswordFile)
	if err != nil {
		return err
	}
	if password == "" {
		return errors.New("no vault password found: set --vault-password-file or the CARGOSHIP_VAULT_PASSWORD/ANSIBLE_VAULT_PASSWORD environment variable")
	}

	value, err := readValue(cmd, args)
	if err != nil {
		return err
	}

	encrypted, err := clustercfg.EncryptValue(value, password)
	if err != nil {
		return err
	}

	fmt.Println(encrypted)
	return nil
}

func readValue(cmd *cobra.Command, args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}

	in := cmd.InOrStdin()
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return readValuePrompt(cmd, f)
	}

	b, err := io.ReadAll(in)
	if err != nil {
		return "", fmt.Errorf("reading value from stdin: %w", err)
	}
	value := strings.TrimRight(string(b), "\r\n")
	if value == "" {
		return "", errors.New("no value given: pass it as an argument or pipe it via stdin")
	}
	return value, nil
}

func readValuePrompt(cmd *cobra.Command, f *os.File) (string, error) {
	fmt.Fprint(cmd.ErrOrStderr(), "Value to encrypt: ")
	b, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", fmt.Errorf("reading value: %w", err)
	}
	value := string(b)
	if value == "" {
		return "", errors.New("no value given")
	}
	return value, nil
}
