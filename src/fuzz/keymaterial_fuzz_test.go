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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/src/internal/clustercfg"
	"github.com/stretchr/testify/require"
)

// FuzzAgeRecipientsIn fuzzes the reader that turns an identity file into the public keys to encrypt
// to.
//
// This is what `cargoship vault keygen` prints and what an operator pipes a key file into, so its
// input is a file that may hold native age identities, an SSH private key, a recipients file passed
// by mistake, or a partially written file from an interrupted keygen. None of those may do worse
// than return an error, and a wrong answer is worse than no answer here: a recipient that is not
// the operator's key is a credential encrypted to someone else.
//
// The property beyond "does not panic" is that every recipient it prints is one the tool accepts
// back. The printed key is pasted into --age-recipient or a recipients file, so a spelling
// ResolveKeyring rejects would be a key the operator cannot use.
func FuzzAgeRecipientsIn(f *testing.F) {
	f.Add("AGE-SECRET-KEY-1QMHQ0ZRPZJD8L4M2QAC3XCVSWZGZ6QDXFHVCLQ8HZQ0PZVC5VXYSZ7YHVU\n")
	f.Add("# created: 2026-01-01T00:00:00Z\n# public key: age1abc\nAGE-SECRET-KEY-1\n")
	f.Add("-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaA==\n-----END OPENSSH PRIVATE KEY-----\n")
	f.Add("age1zvkyg2lqzraa2lnjvqej32nkuu0ues2s82hzrye869xeexvn73equnujw3\n")
	f.Add("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample comment\n")
	f.Add("not a key at all\n")
	f.Add("")

	f.Fuzz(func(t *testing.T, contents string) {
		recipients, err := clustercfg.AgeRecipientsIn(strings.NewReader(contents))
		if err != nil {
			return
		}
		if len(recipients) == 0 {
			return
		}

		// A printed recipient has to be one the encrypting side takes back, because printing it is
		// how an operator moves it from the machine that generated the key to the configuration.
		keyring, err := clustercfg.ResolveKeyring(clustercfg.KeyOptions{AgeRecipients: recipients})
		require.NoError(t, err, "AgeRecipientsIn printed %v, which ResolveKeyring will not take", recipients)

		format, err := keyring.EncryptFormat()
		require.NoError(t, err)
		require.Equal(t, clustercfg.FormatAge, format,
			"a keyring holding only age recipients does not encrypt with age")
	})
}

// FuzzResolveKeyringReadsAnyIdentityFile fuzzes the file --age-identity-file points at.
//
// The file is parsed by hand before age sees it, because it may hold either native age identities
// or an SSH private key, and an encrypted SSH key has to be told apart from an unencrypted one so
// that the passphrase is asked for rather than the parse failing. That is a decision made on the
// file's bytes, which is the sort of decision a fuzzer is for.
//
// The keyring that comes back has to be self-consistent: one holding no identity cannot claim to
// decrypt age, and one that does claim it has to hold something. A keyring wrong about itself sends
// an apply down a path that fails later, on a host, rather than at the point the key was read.
func FuzzResolveKeyringReadsAnyIdentityFile(f *testing.F) {
	f.Add("AGE-SECRET-KEY-1QMHQ0ZRPZJD8L4M2QAC3XCVSWZGZ6QDXFHVCLQ8HZQ0PZVC5VXYSZ7YHVU\n")
	f.Add("# public key: age1abc\nAGE-SECRET-KEY-1QMHQ0ZRPZJD8L4M2QAC3XCVSWZGZ6QDXFHVCLQ8HZQ0PZVC5VXYSZ7YHVU\n")
	f.Add("-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----\n")
	f.Add("-----BEGIN RSA PRIVATE KEY-----\nMIIBOgIBAAJBAK\n-----END RSA PRIVATE KEY-----\n")
	f.Add("-----BEGIN OPENSSH PRIVATE KEY-----\n")
	f.Add("# only a comment\n")
	f.Add("")

	f.Fuzz(func(t *testing.T, contents string) {
		path := filepath.Join(t.TempDir(), "identity")
		require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

		keyring, err := clustercfg.ResolveKeyring(clustercfg.KeyOptions{AgeIdentityFiles: []string{path}})
		if err != nil {
			return
		}

		// Empty and CanDecrypt are the two questions every caller asks a keyring before using it,
		// and they have to give the same account of what it holds. This keyring was given no vault
		// password and no recipients, so an identity is the only thing that can make it non-empty.
		require.Equal(t, keyring.Empty(), !keyring.CanDecrypt(clustercfg.FormatAge),
			"the keyring disagrees with itself about holding an age identity")
		require.False(t, keyring.CanDecrypt(clustercfg.FormatVault),
			"a keyring given no vault password claims to read vault ciphertext")

		if keyring.Empty() {
			// Nothing to encrypt to and nothing to decrypt with is the one state EncryptFormat has
			// to name rather than guess at.
			_, err := keyring.EncryptFormat()
			require.ErrorIs(t, err, clustercfg.ErrNoKeyMaterial)
		}
	})
}

// FuzzRekeyTargetPicksOneKey fuzzes the rule that decides which key a rotation writes with.
//
// RekeyTarget is small, and it is the last thing standing between an operator and a configuration
// rewritten under a key they did not mean. Its inputs come from two flags that each name a key --
// --new-vault-password-file and the age recipients -- and the rule is that both given is an error
// rather than a precedence. What this asserts is the part that has to hold whatever the password
// is: the target never carries both formats, because a keyring naming two is one EncryptFormat
// refuses, and a rotation that cannot encrypt is a rotation that fails after reading the file.
func FuzzRekeyTargetPicksOneKey(f *testing.F) {
	f.Add(rekeyPassword)
	f.Add(fuzzPassword)
	f.Add("")
	f.Add("\x00")

	f.Fuzz(func(t *testing.T, newPassword string) {
		for _, keyring := range encryptKeyrings() {
			target, rotated, err := keyring.ring.RekeyTarget(newPassword)
			if err != nil {
				// Only the age keyring can be asked for two keys at once, and only when a new
				// vault password was named as well.
				require.Equal(t, "age", keyring.name, "%s", keyring.name)
				require.NotEmpty(t, newPassword, "%s", keyring.name)
				continue
			}

			require.False(t, target.Empty(), "%s: rekeying targets a keyring holding no key", keyring.name)

			// The target has to be able to encrypt, and in exactly one format.
			format, err := target.EncryptFormat()
			require.NoError(t, err, "%s: the rekey target cannot choose a format", keyring.name)

			// Reporting no rotation is what tells the caller the pass only re-salted, which is
			// the difference between "the old key no longer reads this file" and "it still does".
			// So a target that did not rotate has to write something the keyring it came from can
			// still read -- stated by reading it back rather than by comparing key material, which
			// is unexported for the reason that a keyring is only ever judged by what it decrypts.
			if !rotated {
				require.Equal(t, clustercfg.FormatVault, format,
					"%s: a re-salt wrote a format the file does not already use", keyring.name)

				encrypted, err := clustercfg.EncryptValue("hunter2", target)
				require.NoError(t, err, "%s", keyring.name)
				plain, err := clustercfg.DecryptValue(encrypted, keyring.ring)
				require.NoError(t, err, "%s: nothing rotated but the old key cannot read the result", keyring.name)
				require.Equal(t, "hunter2", plain, "%s", keyring.name)
			}
		}
	})
}
