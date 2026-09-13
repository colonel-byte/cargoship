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

	"github.com/stretchr/testify/require"
)

// TestCargoshipVaultDecryptPath exercises `vault decrypt-path`, which has to put a plaintext value
// back into a document without disturbing anything around it.
func TestCargoshipVaultDecryptPath(t *testing.T) {
	passwordFile := filepath.Join(t.TempDir(), "vault-password")
	const password = "supersecret"
	require.NoError(t, os.WriteFile(passwordFile, []byte(password), 0o600))

	// encryptedDoc is vaultPathDoc with the value at path encrypted, which is the state
	// decrypt-path is meant to undo.
	encryptedDoc := func(t *testing.T, path string) string {
		t.Helper()
		config := filepath.Join(t.TempDir(), "cluster.yaml")
		require.NoError(t, os.WriteFile(config, []byte(vaultPathDoc), 0o600))

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-path", config, path, "--vault-password-file", passwordFile)
		require.NoError(t, err)
		return config
	}

	t.Run("restores the document a matching encrypt-path produced", func(t *testing.T) {
		for _, path := range []string{
			".spec.config.registries[0].auth.pass",
			".spec.config.registries[0].tls.ca",
			".spec.config.loadbalancer",
		} {
			t.Run(path, func(t *testing.T) {
				config := encryptedDoc(t, path)

				_, _, err := e2e.Cargoship(t, "vault", "decrypt-path", config, path, "--vault-password-file", passwordFile)
				require.NoError(t, err)

				got, err := os.ReadFile(config)
				require.NoError(t, err)
				require.Equal(t, vaultPathDoc, string(got), "encrypt then decrypt should give back the original file")
			})
		}
	})

	t.Run("decrypts several paths in one run", func(t *testing.T) {
		config := filepath.Join(t.TempDir(), "cluster.yaml")
		require.NoError(t, os.WriteFile(config, []byte(vaultPathDoc), 0o600))

		paths := []string{
			".spec.config.registries[0].auth.user",
			".spec.config.registries[0].auth.pass",
			".spec.config.registries[0].tls.ca",
		}
		_, _, err := e2e.Cargoship(t, append([]string{"vault", "encrypt-path", config}, append(paths, "--vault-password-file", passwordFile)...)...)
		require.NoError(t, err)

		_, _, err = e2e.Cargoship(t, append([]string{"vault", "decrypt-path", config}, append(paths, "--vault-password-file", passwordFile)...)...)
		require.NoError(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, vaultPathDoc, string(got), "encrypting then decrypting the same set should give back the original file")
	})

	t.Run("writes nothing when one of several paths fails", func(t *testing.T) {
		config := encryptedDoc(t, ".spec.config.registries[0].auth.pass")
		before, err := os.ReadFile(config)
		require.NoError(t, err)

		// The second path is plaintext, which decrypt-path refuses; the first would have decrypted.
		_, _, err = e2e.Cargoship(t, "vault", "decrypt-path", config,
			".spec.config.registries[0].auth.pass",
			".spec.config.registries[0].auth.user",
			"--vault-password-file", passwordFile)
		require.Error(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(before), string(got), "a failed run should not touch the file")
	})

	t.Run("keeps the file's mode", func(t *testing.T) {
		config := encryptedDoc(t, ".spec.config.registries[0].auth.pass")
		require.NoError(t, os.Chmod(config, 0o640))

		_, _, err := e2e.Cargoship(t, "vault", "decrypt-path", config, ".spec.config.registries[0].auth.pass", "--vault-password-file", passwordFile)
		require.NoError(t, err)

		info, err := os.Stat(config)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	})

	t.Run("warns that the file now holds plaintext", func(t *testing.T) {
		config := encryptedDoc(t, ".spec.config.registries[0].auth.pass")

		_, stderr, err := e2e.Cargoship(t, "vault", "decrypt-path", config, ".spec.config.registries[0].auth.pass", "--vault-password-file", passwordFile, "--no-color")
		require.NoError(t, err)
		require.Contains(t, stderr, "plaintext")
	})

	t.Run("dry run prints the document and leaves the file alone", func(t *testing.T) {
		config := encryptedDoc(t, ".spec.config.registries[0].auth.pass")
		before, err := os.ReadFile(config)
		require.NoError(t, err)

		stdout, _, err := e2e.Cargoship(t, "vault", "decrypt-path", config, ".spec.config.registries[0].auth.pass", "--vault-password-file", passwordFile, "--dry-run")
		require.NoError(t, err)
		require.Contains(t, stdout, "pass: hunter2 # rotate me")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(before), string(got))
	})

	t.Run("refuses a value that is not encrypted", func(t *testing.T) {
		config := filepath.Join(t.TempDir(), "cluster.yaml")
		require.NoError(t, os.WriteFile(config, []byte(vaultPathDoc), 0o600))

		_, stderr, err := e2e.Cargoship(t, "vault", "decrypt-path", config, ".spec.config.registries[0].auth.pass", "--vault-password-file", passwordFile, "--no-color")
		require.Error(t, err)
		require.Contains(t, stderr, "not Ansible Vault-encrypted")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, vaultPathDoc, string(got), "a failed run should not touch the file")
	})

	t.Run("errors on the wrong password", func(t *testing.T) {
		config := encryptedDoc(t, ".spec.config.registries[0].auth.pass")
		before, err := os.ReadFile(config)
		require.NoError(t, err)

		wrong := filepath.Join(t.TempDir(), "wrong-password")
		require.NoError(t, os.WriteFile(wrong, []byte("not the password"), 0o600))

		_, _, err = e2e.Cargoship(t, "vault", "decrypt-path", config, ".spec.config.registries[0].auth.pass", "--vault-password-file", wrong)
		require.Error(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(before), string(got), "a failed run should not touch the file")
	})

	t.Run("errors on a missing file", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")

		_, _, err := e2e.Cargoship(t, "vault", "decrypt-path", missing, ".spec.config.loadbalancer", "--vault-password-file", passwordFile)
		require.Error(t, err)
	})

	t.Run("errors on a path the document does not have", func(t *testing.T) {
		config := encryptedDoc(t, ".spec.config.registries[0].auth.pass")

		_, _, err := e2e.Cargoship(t, "vault", "decrypt-path", config, ".spec.config.registries[0].auth.nope", "--vault-password-file", passwordFile)
		require.Error(t, err)
	})
}
