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
	"os"
	"path/filepath"
	"testing"

	vault "github.com/sosedoff/ansible-vault-go"
	"github.com/stretchr/testify/require"
)

// TestCargoshipVaultRekey exercises `vault rekey` against a config file on disk: that it moves
// every credential to the new password, that the plaintext never lands in the file, and that it
// refuses the states from which a rotation cannot produce a readable configuration.
func TestCargoshipVaultRekey(t *testing.T) {
	dir := t.TempDir()
	passwordFile := filepath.Join(dir, "vault-password")
	const password = "supersecret"
	require.NoError(t, os.WriteFile(passwordFile, []byte(password), 0o600))

	newPasswordFile := filepath.Join(dir, "new-vault-password")
	const newPassword = "the password we are moving to"
	require.NoError(t, os.WriteFile(newPasswordFile, []byte(newPassword), 0o600))

	original, err := os.ReadFile(vaultInventory)
	require.NoError(t, err)

	writeDoc := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "cluster.yaml")
		require.NoError(t, os.WriteFile(path, []byte(original), 0o600))
		return path
	}

	vaultedDoc := func(t *testing.T) string {
		t.Helper()
		config := writeDoc(t)
		_, _, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--vault-password-file", passwordFile)
		require.NoError(t, err)
		return config
	}

	t.Run("moves every credential to the new password", func(t *testing.T) {
		config := vaultedDoc(t)

		_, stderr, err := e2e.Cargoship(t, "vault", "rekey", config, "--vault-password-file", passwordFile, "--new-vault-password-file", newPasswordFile, "--no-color")
		require.NoError(t, err)
		require.Contains(t, stderr, "count=4")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		for _, marker := range []string{"user: |-", "pass: |- # rotate me", "ca: |-", "token: |-"} {
			decrypted, err := vault.Decrypt(vaultValueAt(t, string(got), marker), newPassword)
			require.NoError(t, err, "value at %q should decrypt with the new password", marker)
			require.NotEmpty(t, decrypted)

			_, err = vault.Decrypt(vaultValueAt(t, string(got), marker), password)
			require.Error(t, err, "value at %q should no longer decrypt with the old password", marker)
		}
	})

	// The reason this is one command rather than a decrypt-file followed by an encrypt-file: at no
	// point does the file on disk hold the credential in the clear.
	t.Run("never writes the plaintext to the file", func(t *testing.T) {
		config := vaultedDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "rekey", config, "--vault-password-file", passwordFile, "--new-vault-password-file", newPasswordFile)
		require.NoError(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.NotContains(t, string(got), "hunter2", "the rekeyed password should never appear in the clear")
		require.NotContains(t, string(got), "quay-token")
		require.NotContains(t, string(got), "admin")
	})

	t.Run("leaves everything else alone", func(t *testing.T) {
		config := vaultedDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "rekey", config, "--vault-password-file", passwordFile, "--new-vault-password-file", newPasswordFile)
		require.NoError(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Contains(t, string(got), "  # registries the cluster pulls from", "comments elsewhere should survive")
		require.Contains(t, string(got), "      - name: harbor # our mirror")
		require.Contains(t, string(got), "pass: |- # rotate me", "the trailing comment should survive")
		require.Contains(t, string(got), "          insecureSkipVerify: false", "the key after the CA block should survive")
		require.Contains(t, string(got), "    loadbalancer: lb.example.com", "a field cargoship never decrypts should be left alone")
		require.Contains(t, string(got), `          token: ""`, "an empty field should be left alone")
	})

	t.Run("round-trips back to the original document under the new password", func(t *testing.T) {
		config := vaultedDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "rekey", config, "--vault-password-file", passwordFile, "--new-vault-password-file", newPasswordFile)
		require.NoError(t, err)
		_, _, err = e2e.Cargoship(t, "vault", "decrypt-file", config, "--vault-password-file", newPasswordFile)
		require.NoError(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, original, got)
	})

	// A file already vaulted under two passwords cannot be rotated into a working state, so the
	// command has to stop and say which value it could not read.
	t.Run("refuses a file vaulted under two passwords", func(t *testing.T) {
		config := vaultedDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "decrypt-path", config, ".spec.config.registries[0].auth.pass", "--vault-password-file", passwordFile)
		require.NoError(t, err)
		other := filepath.Join(t.TempDir(), "other-password")
		require.NoError(t, os.WriteFile(other, []byte("a different vault password"), 0o600))
		_, _, err = e2e.Cargoship(t, "vault", "encrypt-path", config, ".spec.config.registries[0].auth.pass", "--vault-password-file", other)
		require.NoError(t, err)

		before, err := os.ReadFile(config)
		require.NoError(t, err)

		_, stderr, err := e2e.Cargoship(t, "vault", "rekey", config, "--vault-password-file", passwordFile, "--new-vault-password-file", newPasswordFile, "--no-color")
		require.Error(t, err)
		require.Contains(t, stderr, "old vault password")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(before), string(got), "a failed run should not touch the file")
	})

	// With no new password named, every value is re-salted under the password the file already
	// carries: fresh ciphertext, same password, and the plaintext still never on disk.
	t.Run("re-salts every credential when the new password is omitted", func(t *testing.T) {
		config := vaultedDoc(t)
		before, err := os.ReadFile(config)
		require.NoError(t, err)

		_, stderr, err := e2e.Cargoship(t, "vault", "rekey", config, "--vault-password-file", passwordFile, "--no-color")
		require.NoError(t, err)
		require.Contains(t, stderr, "count=4")
		require.Contains(t, stderr, "re-salted", "a re-salt should not be reported as a rotation")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.NotEqual(t, string(before), string(got), "every value should come back under a fresh salt")
		require.NotContains(t, string(got), "hunter2", "the plaintext should never reach the file")

		for _, marker := range []string{"user: |-", "pass: |- # rotate me", "ca: |-", "token: |-"} {
			require.NotEqual(t, vaultValueAt(t, string(before), marker), vaultValueAt(t, string(got), marker),
				"ciphertext at %q should have changed", marker)

			decrypted, err := vault.Decrypt(vaultValueAt(t, string(got), marker), password)
			require.NoError(t, err, "value at %q should still decrypt with the same password", marker)
			require.NotEmpty(t, decrypted)
		}
	})

	// Naming a file holding the password the configuration already uses is the same request spelled
	// out, and is reported the same way rather than as a rotation.
	t.Run("re-salts when the new password file holds the old password", func(t *testing.T) {
		config := vaultedDoc(t)

		_, stderr, err := e2e.Cargoship(t, "vault", "rekey", config, "--vault-password-file", passwordFile, "--new-vault-password-file", passwordFile, "--no-color")
		require.NoError(t, err)
		require.Contains(t, stderr, "re-salted")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		_, err = vault.Decrypt(vaultValueAt(t, string(got), "pass: |- # rotate me"), password)
		require.NoError(t, err)
	})

	// Deliberately no environment fallback for the new password: the environment holds the password
	// the file is vaulted under now, so falling back to it would report a rotation onto a password
	// nobody asked to move to.
	t.Run("does not take the new password from the environment", func(t *testing.T) {
		config := vaultedDoc(t)
		t.Setenv("CARGOSHIP_VAULT_PASSWORD", newPassword)

		_, stderr, err := e2e.Cargoship(t, "vault", "rekey", config, "--vault-password-file", passwordFile, "--no-color")
		require.NoError(t, err)
		require.Contains(t, stderr, "re-salted", "an omitted flag means the old password, not one from the environment")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		_, err = vault.Decrypt(vaultValueAt(t, string(got), "pass: |- # rotate me"), password)
		require.NoError(t, err)
	})

	t.Run("errors when the named new password file is empty", func(t *testing.T) {
		config := vaultedDoc(t)
		empty := filepath.Join(t.TempDir(), "empty-password")
		require.NoError(t, os.WriteFile(empty, []byte("\n"), 0o600))

		_, stderr, err := e2e.Cargoship(t, "vault", "rekey", config, "--vault-password-file", passwordFile, "--new-vault-password-file", empty, "--no-color")
		require.Error(t, err)
		require.Contains(t, stderr, "new-vault-password-file")
	})

	t.Run("dry run prints the document and leaves the file alone", func(t *testing.T) {
		config := vaultedDoc(t)
		before, err := os.ReadFile(config)
		require.NoError(t, err)

		stdout, _, err := e2e.Cargoship(t, "vault", "rekey", config, "--vault-password-file", passwordFile, "--new-vault-password-file", newPasswordFile, "--dry-run")
		require.NoError(t, err)
		require.Contains(t, stdout, "$ANSIBLE_VAULT")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(before), string(got))
	})

	t.Run("has nothing to do on a file with no ciphertext in it", func(t *testing.T) {
		config := writeDoc(t)

		_, stderr, err := e2e.Cargoship(t, "vault", "rekey", config, "--vault-password-file", passwordFile, "--new-vault-password-file", newPasswordFile, "--no-color")
		require.NoError(t, err)
		require.Contains(t, stderr, "nothing to rekey")

		_, stderr, err = e2e.Cargoship(t, "vault", "rekey", config, "--vault-password-file", passwordFile, "--no-color")
		require.NoError(t, err)
		require.Contains(t, stderr, "nothing to re-salt")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, original, got)
	})

	t.Run("errors on a missing file", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")

		_, _, err := e2e.Cargoship(t, "vault", "rekey", missing, "--vault-password-file", passwordFile, "--new-vault-password-file", newPasswordFile)
		require.Error(t, err)
	})
}
