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

	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/internal/clustercfg"
	"github.com/spf13/cobra"
)

const (
	// FlagAgeIdentityFile names the flag holding an age identity file, which decrypts
	FlagAgeIdentityFile = "age-identity-file"
	// FlagAgeRecipient names the flag holding an age public key, which encrypts
	FlagAgeRecipient = "age-recipient"
	// FlagAgeRecipientsFile names the flag holding a file of age public keys, which encrypt
	FlagAgeRecipientsFile = "age-recipients-file"
)

// keyFlags is the key material flags of one command, shared by every command that reads or writes
// encrypted registry credentials so that the three age flags are spelled and defaulted in one
// place rather than nine.
//
// The vault password file keeps its own flag name per command group -- MiscVaultPasswordFile and
// InstallVaultPasswordFile are the same string, but the two constants have always been separate --
// so it is registered by the caller and only carried here.
type keyFlags struct {
	vaultPasswordFile string
	ageIdentityFiles  []string
	ageRecipients     []string
	ageRecipientFiles []string
}

// addAgeFlags registers the three age flags on cmd, defaulted from the `.age` section of the
// cargoship config file.
//
// Defaulting from the config file rather than leaving them empty is what lets a team commit its
// recipient set once and have every `vault encrypt-file` in the repository write age ciphertext
// without anyone restating the keys. A value from the config file counts as the operator asking
// for age, exactly as the flag does.
func addAgeFlags(cmd *cobra.Command, f *keyFlags) {
	cmd.Flags().StringArrayVar(&f.ageIdentityFiles, FlagAgeIdentityFile, resolvedConfig.AgeOpts.IdentityFiles, lang.CmdFlagAgeIdentityFile)
	cmd.Flags().StringArrayVar(&f.ageRecipients, FlagAgeRecipient, resolvedConfig.AgeOpts.Recipients, lang.CmdFlagAgeRecipient)
	cmd.Flags().StringArrayVar(&f.ageRecipientFiles, FlagAgeRecipientsFile, resolvedConfig.AgeOpts.RecipientsFiles, lang.CmdFlagAgeRecipientsFile)
}

// keyOptions renders the flags as the options clustercfg resolves key material from.
func (f *keyFlags) keyOptions() clustercfg.KeyOptions {
	return clustercfg.KeyOptions{
		VaultPasswordFile: f.vaultPasswordFile,
		AgeIdentityFiles:  f.ageIdentityFiles,
		AgeRecipients:     f.ageRecipients,
		AgeRecipientFiles: f.ageRecipientFiles,
	}
}

// resolveKeyring reads every key the flags name, allowing the result to be empty.
//
// An apply is the caller for that: a configuration holding no encrypted credential needs no key,
// and demanding one would break every plaintext configuration that works today.
func (f *keyFlags) resolveKeyring() (*clustercfg.Keyring, error) {
	return clustercfg.ResolveKeyring(f.keyOptions())
}

// requireKeyring reads every key the flags name, treating the absence of all of them as an error
// rather than as an empty password.
//
// This is what the vault commands call. Each of them exists to encrypt or decrypt one thing, so a
// command that got no key at all cannot do its job, and saying so is better than failing later
// inside the crypto with something less clear.
func (f *keyFlags) requireKeyring() (*clustercfg.Keyring, error) {
	keyring, err := f.resolveKeyring()
	if err != nil {
		return nil, err
	}
	if keyring.Empty() {
		return nil, errors.New("no encryption key found: set --vault-password-file or the CARGOSHIP_VAULT_PASSWORD/ANSIBLE_VAULT_PASSWORD environment variable, or pass --age-recipient and --age-identity-file")
	}
	return keyring, nil
}
