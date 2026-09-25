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

// Package test provides e2e tests for cargoship
package noncluster

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// sshKey is one SSH key pair and the files a command is pointed at to use it: the private key an
// --age-identity-file names, and the authorized_keys line an --age-recipients-file holds.
type sshKey struct {
	authorized    string
	identityFile  string
	recipientFile string
}

// newSSHKey generates an ssh-ed25519 key pair and writes it out the way ssh-keygen does, so the
// commands under test read the files an operator already has rather than fixtures. Generating in
// process keeps the suite from depending on an ssh-keygen binary being installed.
func newSSHKey(t *testing.T, passphrase string) sshKey {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	require.NoError(t, err)

	sshPub, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)

	dir := t.TempDir()
	key := sshKey{
		authorized:    string(ssh.MarshalAuthorizedKey(sshPub)),
		identityFile:  filepath.Join(dir, "id_ed25519"),
		recipientFile: filepath.Join(dir, "authorized_keys"),
	}

	require.NoError(t, os.WriteFile(key.identityFile, pem.EncodeToMemory(block), 0o600))
	require.NoError(t, os.WriteFile(key.identityFile+".pub", []byte(key.authorized), 0o644)) //nolint:gosec // a public key is not secret
	require.NoError(t, os.WriteFile(key.recipientFile,
		[]byte("# the platform team\n"+key.authorized), 0o600))
	return key
}

// TestCargoshipSSHKeys exercises the age flags with SSH keys, which is the whole point of accepting
// them: a team encrypts to the keys it already distributes, with nothing new to hand out.
func TestCargoshipSSHKeys(t *testing.T) {
	key := newSSHKey(t, "")

	original, err := os.ReadFile(vaultInventory)
	require.NoError(t, err)

	writeDoc := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "cluster.yaml")
		require.NoError(t, os.WriteFile(path, original, 0o600))
		return path
	}

	t.Run("an authorized_keys file round trips", func(t *testing.T) {
		clearKeyEnv(t)
		config := writeDoc(t)

		_, stderr, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--age-recipients-file", key.recipientFile)
		require.NoError(t, err)
		require.Contains(t, stderr, "count=4")

		encrypted, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Contains(t, string(encrypted), ageHeader)
		require.NotContains(t, string(encrypted), "hunter2")

		_, _, err = e2e.Cargoship(t, "vault", "decrypt-file", config, "--age-identity-file", key.identityFile)
		require.NoError(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(original), string(got))
	})

	t.Run("an SSH public key works as an inline recipient", func(t *testing.T) {
		clearKeyEnv(t)
		config := writeDoc(t)

		// Trailing newline trimmed, because that is how the key arrives when it is pasted or read
		// out of a variable rather than out of the file.
		recipient := key.authorized[:len(key.authorized)-1]

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--age-recipient", recipient)
		require.NoError(t, err)

		_, _, err = e2e.Cargoship(t, "vault", "decrypt-file", config, "--age-identity-file", key.identityFile)
		require.NoError(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(original), string(got))
	})

	// A recipients file holding both kinds is what a migration looks like from the inside: the age
	// keys are in place before every SSH key is retired, and both have to keep working throughout.
	t.Run("a mixed recipients file is readable by either key", func(t *testing.T) {
		clearKeyEnv(t)
		age := newAgeKey(t)

		mixed := filepath.Join(t.TempDir(), "recipients.txt")
		require.NoError(t, os.WriteFile(mixed,
			[]byte("# migrating to age\n"+age.recipient+"\n\n"+key.authorized), 0o600))

		for _, identityFile := range []string{key.identityFile, age.identityFile} {
			config := writeDoc(t)

			_, _, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--age-recipients-file", mixed)
			require.NoError(t, err)
			_, _, err = e2e.Cargoship(t, "vault", "decrypt-file", config, "--age-identity-file", identityFile)
			require.NoError(t, err)

			got, err := os.ReadFile(config)
			require.NoError(t, err)
			require.Equal(t, string(original), string(got), "reading back with %s", identityFile)
		}
	})

	// Skipping the unusable line would encrypt to fewer recipients than the operator listed, and
	// the person who finds out is the one who cannot decrypt.
	t.Run("an unusable key fails the whole recipients file", func(t *testing.T) {
		clearKeyEnv(t)
		config := writeDoc(t)

		bad := filepath.Join(t.TempDir(), "recipients.txt")
		require.NoError(t, os.WriteFile(bad,
			[]byte(key.authorized+"ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTY= operator@example.com\n"), 0o600))

		_, stderr, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--age-recipients-file", bad)
		require.Error(t, err)
		require.Contains(t, stderr, "line 2")

		// The document is untouched, so a failed run leaves nothing half-encrypted.
		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(original), string(got))
	})

	// The CI case. There is nowhere to prompt, so the run has to fail with something actionable
	// rather than block on a read that will never be answered.
	t.Run("a passphrase-protected key fails without a terminal", func(t *testing.T) {
		clearKeyEnv(t)
		locked := newSSHKey(t, "hunter2")
		config := writeDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--age-recipients-file", locked.recipientFile)
		require.NoError(t, err)

		_, stderr, err := cargoshipStdin(t, "", "vault", "decrypt-file", config, "--age-identity-file", locked.identityFile)
		require.Error(t, err)
		require.Contains(t, stderr, "passphrase")
	})

	// keygen answers rather than guesses: the public key of an SSH key is already in the file
	// beside it, and an agessh recipient has no text encoding to print.
	t.Run("keygen --public-key points at the .pub file", func(t *testing.T) {
		clearKeyEnv(t)

		_, stderr, err := e2e.Cargoship(t, "vault", "keygen", "--public-key", key.identityFile)
		require.Error(t, err)
		require.Contains(t, stderr, ".pub")
	})
}
