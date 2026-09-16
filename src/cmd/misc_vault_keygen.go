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

	"filippo.io/age"
	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/internal/clustercfg"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	// MiscVaultKeygenOutput flag
	MiscVaultKeygenOutput = "output"
	// MiscVaultKeygenPublicKey flag
	MiscVaultKeygenPublicKey = "public-key"
)

type vaultKeygenOptions struct {
	output    string
	publicKey bool
}

// newVaultKeygenCommand makes the age key pair the rest of the age support needs, so that the age
// distribution is not a prerequisite for using a format cargoship already links the library for.
//
// It sits in the vault group with the commands it serves, for the same reason they kept the name:
// the group is called vault because renaming it would break every script that calls it. The Short
// string names age so that the group's help stays unambiguous.
func newVaultKeygenCommand() *cobra.Command {
	o := vaultKeygenOptions{}

	cmd := &cobra.Command{
		Use:     "keygen [IDENTITY_FILE]",
		Args:    cobra.MaximumNArgs(1),
		Short:   lang.CmdVaultKeygenShort,
		Long:    lang.CmdVaultKeygenLong,
		Example: lang.CmdVaultKeygenExample,
		RunE:    o.run,
	}

	cmd.Flags().StringVarP(&o.output, MiscVaultKeygenOutput, "o", "", lang.CmdVaultKeygenFlagOutput)
	cmd.Flags().BoolVarP(&o.publicKey, MiscVaultKeygenPublicKey, "y", false, lang.CmdVaultKeygenFlagPublicKey)

	return cmd
}

func (o *vaultKeygenOptions) run(cmd *cobra.Command, args []string) error {
	if o.publicKey {
		// --output is refused here rather than honored, so that it keeps one meaning: the file
		// holding a private key that was just generated, which is never overwritten. A public key
		// needs no such care and redirects perfectly well.
		if o.output != "" {
			return fmt.Errorf("--%s cannot be combined with --%s: public keys are not secret, so redirect them instead",
				MiscVaultKeygenOutput, MiscVaultKeygenPublicKey)
		}
		return o.printPublicKeys(cmd, args)
	}

	if len(args) == 1 {
		return fmt.Errorf("%q is only read with --%s; generating a key pair takes no argument, and writes to --%s or stdout",
			args[0], MiscVaultKeygenPublicKey, MiscVaultKeygenOutput)
	}

	return o.generate(cmd)
}

// generate makes a key pair and puts it somewhere the operator chose.
//
// The identity never passes through logger. Logging is wired to stderr and may be collected
// centrally, and a private key that reaches a log aggregator is a private key that has to be
// treated as compromised wherever it was used.
func (o *vaultKeygenOptions) generate(cmd *cobra.Command) error {
	id, err := clustercfg.GenerateAgeIdentity()
	if err != nil {
		return err
	}

	if o.output == "" {
		warnIfTerminal(cmd)
		return clustercfg.WriteAgeIdentity(cmd.OutOrStdout(), id)
	}

	if err := writeAgeIdentityFile(o.output, id); err != nil {
		return err
	}

	// To stderr, so that the file holds the private key and nothing else, and so this can be
	// copied straight into a recipients file.
	fmt.Fprintf(cmd.ErrOrStderr(), "Public key: %s\n", id.Recipient())
	return nil
}

// writeAgeIdentityFile creates path with mode 0600 and refuses to overwrite a file that is already
// there.
//
// Refusing is the whole point of writing the file here rather than through writeFileInPlace.
// Replacing an identity file destroys the only copy of a key, and any value in a committed
// configuration that was encrypted to it becomes unreadable by anyone, permanently. O_EXCL makes
// the check part of the create, so there is no window between asking and writing.
func writeAgeIdentityFile(path string, id *age.X25519Identity) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s exists already, and cargoship will not overwrite an age identity: anything encrypted to the key it holds would become unreadable. Choose another path, or move that file aside first", path)
		}
		return fmt.Errorf("creating %s: %w", path, err)
	}

	if err := clustercfg.WriteAgeIdentity(f, id); err != nil {
		_ = f.Close() //nolint:errcheck // the write error is the one worth reporting
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", path, err)
	}
	return nil
}

// printPublicKeys reads an identity file and prints the public key of every identity in it, which
// is how an operator recovers a recipient from a private key they still hold.
func (o *vaultKeygenOptions) printPublicKeys(cmd *cobra.Command, args []string) error {
	in := cmd.InOrStdin()
	if len(args) == 1 {
		f, err := os.Open(args[0])
		if err != nil {
			return fmt.Errorf("opening %s: %w", args[0], err)
		}
		defer f.Close() //nolint:errcheck // the file is open for reading
		in = f
	} else if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return errors.New("no identity file given: pass one as an argument or pipe it via stdin")
	}

	recipients, err := clustercfg.AgeRecipientsIn(in)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	for _, recipient := range recipients {
		if _, err := io.WriteString(out, recipient+"\n"); err != nil {
			return err
		}
	}
	return nil
}

// warnIfTerminal says so when a private key is about to be printed to a screen.
//
// Without --output the key goes to stdout, which is what makes "cargoship vault keygen > key.txt"
// work, and the file's mode is then whatever the shell gives it. Run bare, the same command puts
// the key on the screen and into the scrollback of whatever it was run in.
func warnIfTerminal(cmd *cobra.Command) {
	f, ok := cmd.OutOrStdout().(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "Warning: writing a private key to the terminal. It will stay in this terminal's scrollback. Use --output FILE, or redirect this to a file, and restrict it to the user that runs cargoship.")
}
