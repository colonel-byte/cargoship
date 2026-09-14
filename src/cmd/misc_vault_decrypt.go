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

type vaultDecryptOptions struct {
	vaultPasswordFile string
}

func newVaultDecryptCommand() *cobra.Command {
	o := vaultDecryptOptions{}

	cmd := &cobra.Command{
		Use:     "decrypt [VALUE]",
		Args:    cobra.MaximumNArgs(1),
		Short:   lang.CmdVaultDecryptShort,
		Long:    lang.CmdVaultDecryptLong,
		Example: lang.CmdVaultDecryptExample,
		RunE:    o.run,
	}

	// Not marked required, for the same reason as on encrypt: the environment variables
	// ResolveVaultPassword falls back to are unreachable if cobra rejects the command first.
	cmd.Flags().StringVar(&o.vaultPasswordFile, MiscVaultPasswordFile, "", lang.CmdVaultEncryptFlagPasswordFile)

	return cmd
}

func (o *vaultDecryptOptions) run(cmd *cobra.Command, args []string) error {
	password, err := requireVaultPassword(o.vaultPasswordFile)
	if err != nil {
		return err
	}

	value, err := readCiphertext(cmd, args)
	if err != nil {
		return err
	}

	plain, err := clustercfg.DecryptValue(value, password)
	if err != nil {
		return err
	}

	// The plaintext is written as it is, with a newline added only when it does not end in one, so
	// that a multi-line value such as a PEM certificate can be redirected straight into a file.
	if !strings.HasSuffix(plain, "\n") {
		plain += "\n"
	}
	_, err = io.WriteString(cmd.OutOrStdout(), plain)
	return err
}

// readCiphertext returns the vaulted value to decrypt, from the command line or from stdin.
//
// Unlike the plaintext encrypt takes, this is neither secret nor a single line, so there is no
// hidden prompt to fall back on: a terminal with nothing piped into it is a mistake worth naming.
func readCiphertext(cmd *cobra.Command, args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}

	in := cmd.InOrStdin()
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return "", errors.New("no value given: pass the encrypted value as an argument or pipe it via stdin")
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
