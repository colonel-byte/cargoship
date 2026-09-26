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
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/internal/clustercfg"
	"github.com/stretchr/testify/require"
)

// FuzzEncryptValueRoundTrip asserts that any value EncryptValue accepts comes back byte-identical
// from DecryptValue, in either format. That is the promise the pair makes to an operator vaulting
// a credential, and it is the property most likely to break quietly: a value is written once and
// read back much later, so a byte lost in the round trip surfaces as a login failure on a host
// rather than as an error at the time it was stored.
//
// Both formats matter because neither library is cargoship's. Ansible Vault takes and returns a
// string; age takes a reader and writes armor. Each has its own opinion about trailing newlines
// and about bytes that are not text, and the round trip is where a disagreement shows up.
func FuzzEncryptValueRoundTrip(f *testing.F) {
	// The seeds name the shapes a credential actually takes, plus the ones most likely to be
	// mishandled: an empty value, whitespace a trim would eat, a PEM block, control characters,
	// and bytes that are not UTF-8.
	f.Add("hunter2")
	f.Add("")
	f.Add("-----BEGIN CERTIFICATE-----\naGVsbG8gd29ybGQ=\n-----END CERTIFICATE-----\n")
	f.Add("trailing space \nsecond line")
	f.Add("tab\tand\rcarriage return")
	f.Add("control\x01character")
	f.Add("héllo wörld \U0001f680")
	f.Add("\xff\xfe not valid utf-8")
	f.Add("\n")
	f.Add("a value long enough to wrap the armor's line width, which age sets at sixty-four columns")

	f.Fuzz(func(t *testing.T, value string) {
		for _, keyring := range encryptKeyrings() {
			encrypted, err := clustercfg.EncryptValue(value, keyring.ring)
			require.NoError(t, err, "%s", keyring.name)

			// The header is what every other entry point dispatches on, so ciphertext that does
			// not carry one is ciphertext nothing downstream recognises as encrypted -- it would
			// be written to a host verbatim.
			require.True(t, cluster.IsEncrypted(encrypted),
				"%s ciphertext carries no header: %q", keyring.name, encrypted)

			decrypted, err := clustercfg.DecryptValue(encrypted, keyring.ring)
			require.NoError(t, err, "%s", keyring.name)
			require.Equal(t, value, decrypted, "%s: value did not survive the round trip", keyring.name)
		}
	})
}

// FuzzEncryptValueIsSalted asserts that encrypting the same value twice never produces the same
// ciphertext.
//
// Both formats are salted -- Ansible Vault with a random salt, age with an ephemeral file key --
// and a configuration in version control is where that matters: identical ciphertext for two
// registries would tell a reader of the repository that the two passwords are the same, which is
// exactly what encrypting them was meant to avoid. It is also the cheapest signal that a keyring
// has not silently degraded into a fixed-key mode.
func FuzzEncryptValueIsSalted(f *testing.F) {
	f.Add("hunter2")
	f.Add("")
	f.Add("héllo wörld \U0001f680")

	f.Fuzz(func(t *testing.T, value string) {
		for _, keyring := range encryptKeyrings() {
			first, err := clustercfg.EncryptValue(value, keyring.ring)
			require.NoError(t, err, "%s", keyring.name)
			second, err := clustercfg.EncryptValue(value, keyring.ring)
			require.NoError(t, err, "%s", keyring.name)

			require.NotEqual(t, first, second,
				"%s encrypted the same value to the same ciphertext twice", keyring.name)
		}
	})
}

// FuzzDecryptValueRejectsGarbage asserts that no input makes DecryptValue do anything worse than
// return an error. The ciphertext it parses is handled by a third-party library and comes from a
// file an operator edited by hand, so malformed input is the expected case rather than the exotic
// one: a truncated paste or a stray edit has to produce an error the operator can read.
//
// A panic fails the target on its own. What the body states is what a caller relies on when the
// call succeeds: DecryptValue dispatches on the header and reports ErrNotEncrypted for anything
// without one, so a value it decrypted has to have carried a header, and that header has to be one
// the keyring holds a key for.
func FuzzDecryptValueRejectsGarbage(f *testing.F) {
	vaulted, err := clustercfg.EncryptValue("hunter2", vaultKeyring)
	require.NoError(f, err)
	aged, err := clustercfg.EncryptValue("hunter2", ageKeyring)
	require.NoError(f, err)

	f.Add(vaulted)
	f.Add(vaulted[:len(vaulted)/2])
	f.Add(aged)
	f.Add(aged[:len(aged)/2])
	f.Add("$ANSIBLE_VAULT;1.1;AES256\n3336")
	f.Add("$ANSIBLE_VAULT;1.1;AES256\n")
	f.Add("$ANSIBLE_VAULT;9.9;ROT13\n61626364")
	f.Add("$ANSIBLE_VAULT;1.1;AES256\nnot hexadecimal at all")
	f.Add("-----BEGIN AGE ENCRYPTED FILE-----\n-----END AGE ENCRYPTED FILE-----")
	f.Add("-----BEGIN AGE ENCRYPTED FILE-----\nnot base64 at all\n-----END AGE ENCRYPTED FILE-----")
	f.Add("-----BEGIN AGE ENCRYPTED FILE-----\n")
	f.Add("not ciphertext")
	f.Add("")

	f.Fuzz(func(t *testing.T, blob string) {
		for _, keyring := range encryptKeyrings() {
			if _, err := clustercfg.DecryptValue(blob, keyring.ring); err != nil {
				continue
			}
			format, encrypted := clustercfg.FormatOf(blob)
			require.True(t, encrypted,
				"%s decrypted a value carrying no header: %q", keyring.name, blob)
			require.True(t, keyring.ring.CanDecrypt(format),
				"%s decrypted %s ciphertext it holds no key for: %q", keyring.name, format, blob)
		}
	})
}

// FuzzFormatOfAgreesWithTheAPITypes asserts that the two places a header is recognised agree.
//
// clustercfg.FormatOf dispatches every encrypt and decrypt in the tool, and cluster.IsEncrypted is
// what the API types expose to everything that only needs to know whether a field is ciphertext --
// the lint rules, the validator, the apply-time probe. They are separate functions over the same
// two headers, and if they ever disagree, one part of the tool treats a value as a secret and
// another treats it as a password to send to a registry.
func FuzzFormatOfAgreesWithTheAPITypes(f *testing.F) {
	f.Add("$ANSIBLE_VAULT;1.1;AES256\n3336")
	f.Add("  \n  $ANSIBLE_VAULT;1.1;AES256\n3336")
	f.Add("-----BEGIN AGE ENCRYPTED FILE-----\nYWJj\n-----END AGE ENCRYPTED FILE-----\n")
	f.Add("$ANSIBLE_VAULT")
	f.Add("-----BEGIN AGE ENCRYPTED FILE-----")
	f.Add("-----BEGIN CERTIFICATE-----")
	f.Add("hunter2")
	f.Add("")

	f.Fuzz(func(t *testing.T, value string) {
		format, encrypted := clustercfg.FormatOf(value)

		require.Equal(t, cluster.IsEncrypted(value), encrypted,
			"FormatOf and cluster.IsEncrypted disagree about %q", value)

		switch format {
		case clustercfg.FormatVault:
			require.True(t, cluster.IsVaultEncrypted(value), "%q is not vault ciphertext", value)
		case clustercfg.FormatAge:
			require.True(t, cluster.IsAgeEncrypted(value), "%q is not age ciphertext", value)
		default:
			require.False(t, encrypted, "FormatOf reported ciphertext in no format: %q", value)
			return
		}

		// A keyring reports what it can read per format, and the pairing has to be the one the
		// formats say: the vault keyring reads vault ciphertext and not age, and the reverse.
		require.Equal(t, format == clustercfg.FormatVault, vaultKeyring.CanDecrypt(format),
			"the vault keyring's answer for %s is wrong", format)
		require.Equal(t, format == clustercfg.FormatAge, ageKeyring.CanDecrypt(format),
			"the age keyring's answer for %s is wrong", format)
	})
}
