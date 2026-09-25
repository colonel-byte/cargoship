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

package fuzz

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/internal/clustercfg"
)

// fuzzPassword is the vault password every target in this package encrypts under. These targets
// fuzz the value, the path and the document, not the key: a wrong password is covered by the table
// tests in clustercfg, and varying it here would spend most of the corpus on inputs that fail for
// the uninteresting reason.
const fuzzPassword = "correct horse battery staple"

// rekeyPassword is the vault password a rotation moves to, kept distinct from fuzzPassword so that
// a rekey which quietly did nothing fails rather than passing.
const rekeyPassword = "a different vault password"

// vaultKeyring and ageKeyring are the two key formats a configuration can hold. Every target that
// encrypts runs against both, because the format is chosen once inside the keyring and everything
// downstream -- which scalar style the ciphertext is spliced in as, how long it is, whether it
// ends in a newline -- differs between them. Ansible Vault is a single header line followed by
// hexadecimal; age is PEM-style armor whose trailing newline the splice has to strip.
var (
	vaultKeyring      *clustercfg.Keyring
	ageKeyring        *clustercfg.Keyring
	rekeyVaultKeyring *clustercfg.Keyring
	rekeyAgeKeyring   *clustercfg.Keyring
)

// namedKeyring pairs a keyring with the name a failure should report, so that a target running
// over both formats says which one broke without the reader decoding a pointer.
type namedKeyring struct {
	name string
	ring *clustercfg.Keyring
}

// encryptKeyrings is every keyring a target can encrypt with, which is every format cargoship
// writes. Range over this in a target body rather than splitting the target in two: one corpus
// covering both formats is one set of interesting inputs, and a value that breaks the splice in
// one format is worth trying immediately in the other.
func encryptKeyrings() []namedKeyring {
	return []namedKeyring{
		{name: "vault", ring: vaultKeyring},
		{name: "age", ring: ageKeyring},
	}
}

// rekeyPair is one rotation: the keyring a document is read with and the one it is rewritten to.
type rekeyPair struct {
	name     string
	from, to *clustercfg.Keyring
}

// rekeyPairs is every from/to combination a rekey can be asked for, including the two that cross
// formats. Crossing is the migration path an operator takes off Ansible Vault, and it is the case
// where a value passes through both crypto implementations in one pass, so it is the one most
// worth putting a fuzzer behind.
func rekeyPairs() []rekeyPair {
	return []rekeyPair{
		{name: "vault-to-vault", from: vaultKeyring, to: rekeyVaultKeyring},
		{name: "vault-to-age", from: vaultKeyring, to: rekeyAgeKeyring},
		{name: "age-to-age", from: ageKeyring, to: rekeyAgeKeyring},
		{name: "age-to-vault", from: ageKeyring, to: rekeyVaultKeyring},
	}
}

// TestMain builds the keyrings the targets share and, first, takes the developer's environment out
// of the picture.
//
// ResolveKeyring reads CARGOSHIP_AGE_IDENTITY_FILE and CARGOSHIP_AGE_RECIPIENTS, and the vault
// password falls back to CARGOSHIP_VAULT_PASSWORD or ANSIBLE_VAULT_PASSWORD. A machine already
// using cargoship has those set, which would mean the fuzzer encrypted to a key the corpus was not
// built against -- and a crasher that reproduces only on the machine that found it.
func TestMain(m *testing.M) {
	for _, name := range []string{
		clustercfg.AgeIdentityFileEnvVar,
		clustercfg.AgeRecipientsEnvVar,
		clustercfg.VaultPasswordEnvVar,
		clustercfg.AnsibleVaultPasswordEnvVar,
	} {
		if err := os.Unsetenv(name); err != nil {
			fmt.Fprintf(os.Stderr, "clearing %s: %v\n", name, err)
			os.Exit(1)
		}
	}

	dir, err := os.MkdirTemp("", "cargoship-fuzz-keys")
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating the key directory: %v\n", err)
		os.Exit(1)
	}

	code := 1
	// The keyrings hold parsed key material rather than the paths it came from, so removing the
	// directory once they are built leaves them usable for the whole run.
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintf(os.Stderr, "removing the key directory: %v\n", err)
		}
		os.Exit(code)
	}()

	vaultKeyring = clustercfg.NewVaultKeyring(fuzzPassword)
	rekeyVaultKeyring = clustercfg.NewVaultKeyring(rekeyPassword)

	if ageKeyring, err = newAgeKeyring(filepath.Join(dir, "identity.txt")); err != nil {
		fmt.Fprintf(os.Stderr, "building the age keyring: %v\n", err)
		return
	}
	if rekeyAgeKeyring, err = newAgeKeyring(filepath.Join(dir, "rekey-identity.txt")); err != nil {
		fmt.Fprintf(os.Stderr, "building the rekey age keyring: %v\n", err)
		return
	}

	code = m.Run()
}

// newAgeKeyring generates an age identity, writes it to path, and resolves a keyring that both
// encrypts to it and decrypts with it.
//
// The identity goes through a file rather than straight into a keyring because that is the only
// route the package exports, and because it is the route an operator takes: `cargoship vault
// keygen` writes this file and `--age-identity-file` reads it back.
func newAgeKeyring(path string) (*clustercfg.Keyring, error) {
	identity, err := clustercfg.GenerateAgeIdentity()
	if err != nil {
		return nil, fmt.Errorf("generating an age identity: %w", err)
	}

	var written bytes.Buffer
	if err := clustercfg.WriteAgeIdentity(&written, identity); err != nil {
		return nil, fmt.Errorf("rendering the identity: %w", err)
	}
	if err := os.WriteFile(path, written.Bytes(), 0o600); err != nil {
		return nil, fmt.Errorf("writing %s: %w", path, err)
	}

	// The recipient is read back out of the written file rather than taken from the identity in
	// memory, so that what the keyring encrypts to is what an operator would have pasted out of
	// `cargoship vault keygen`.
	recipients, err := clustercfg.AgeRecipientsIn(bytes.NewReader(written.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("reading the recipient out of %s: %w", path, err)
	}

	return clustercfg.ResolveKeyring(clustercfg.KeyOptions{
		AgeIdentityFiles: []string{path},
		AgeRecipients:    recipients,
	})
}
