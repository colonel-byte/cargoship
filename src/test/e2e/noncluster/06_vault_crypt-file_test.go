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

// TestCargoshipVaultEncryptFile exercises `vault encrypt-file` and `vault decrypt-file` against a
// config file on disk: which fields they walk, what they leave alone, and the round trip between
// them.
func TestCargoshipVaultEncryptFile(t *testing.T) {
	dir := t.TempDir()
	passwordFile := filepath.Join(dir, "vault-password")
	const password = "supersecret"
	require.NoError(t, os.WriteFile(passwordFile, []byte(password), 0o600))

	original, err := os.ReadFile(vaultInventory)
	require.NoError(t, err)

	writeDoc := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "cluster.yaml")

		require.NoError(t, os.WriteFile(path, []byte(original), 0o600))
		return path
	}

	// vaultedDoc is a copy of vaultFileDoc with every credential already encrypted, which is where
	// the decrypt-file subtests start from.
	vaultedDoc := func(t *testing.T) string {
		t.Helper()
		config := writeDoc(t)
		_, _, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--vault-password-file", passwordFile)
		require.NoError(t, err)
		return config
	}

	// A configuration that shares one auth block between two registries: the credential lives under
	// the anchor, and the second registry holds an alias to it.
	t.Run("encrypts a credential two registries share through an anchor", func(t *testing.T) {
		const anchored = `apiVersion: zarf.dev/v1alpha1
kind: ZarfCluster
spec:
  config:
    registries:
      - name: docker.io
        auth: &auth
          user: robot
          pass: hunter2
      - name: test.io
        auth: *auth
  hosts:
    - name: node1
`
		config := filepath.Join(t.TempDir(), "cluster.yaml")
		require.NoError(t, os.WriteFile(config, []byte(anchored), 0o600))

		_, stderr, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--vault-password-file", passwordFile, "--no-color")
		require.NoError(t, err)
		require.Contains(t, stderr, "count=2", "the shared user and pass, once each")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.NotContains(t, string(got), "pass: hunter2", "the shared password should not be left in the clear")
		require.Contains(t, string(got), "auth: *auth", "the alias should survive")

		decrypted, err := vault.Decrypt(vaultValueAt(t, string(got), "pass: |-"), password)
		require.NoError(t, err)
		require.Equal(t, "hunter2", decrypted)

		// Back to the file that went in, which is the check that nothing about the anchor moved.
		_, _, err = e2e.Cargoship(t, "vault", "decrypt-file", config, "--vault-password-file", passwordFile)
		require.NoError(t, err)
		got, err = os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, anchored, string(got))
	})

	t.Run("encrypts every credential and leaves everything else alone", func(t *testing.T) {
		config := writeDoc(t)

		_, stderr, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--vault-password-file", passwordFile, "--no-color")
		require.NoError(t, err)
		require.Contains(t, stderr, "count=4")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Contains(t, string(got), "  # registries the cluster pulls from", "comments elsewhere should survive")
		require.Contains(t, string(got), "      - name: harbor # our mirror")
		require.Contains(t, string(got), "pass: |- # rotate me", "the trailing comment should survive")
		require.Contains(t, string(got), "          insecureSkipVerify: false", "the key after the CA block should survive")
		require.Contains(t, string(got), "    loadbalancer: lb.example.com", "a field cargoship never decrypts should be left alone")
		require.Contains(t, string(got), `          token: ""`, "an empty field should be left alone")

		for _, marker := range []string{"user: |-", "pass: |- # rotate me", "ca: |-", "token: |-"} {
			decrypted, err := vault.Decrypt(vaultValueAt(t, string(got), marker), password)
			require.NoError(t, err, "value at %q should decrypt", marker)
			require.NotEmpty(t, decrypted)
		}
	})

	t.Run("round-trips a document byte for byte", func(t *testing.T) {
		config := vaultedDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "decrypt-file", config, "--vault-password-file", passwordFile)
		require.NoError(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, original, got)
	})

	t.Run("a second run has nothing to do", func(t *testing.T) {
		config := vaultedDoc(t)
		before, err := os.ReadFile(config)
		require.NoError(t, err)

		_, stderr, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--vault-password-file", passwordFile, "--no-color")
		require.NoError(t, err)
		require.Contains(t, stderr, "nothing to encrypt")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(before), string(got))
	})

	t.Run("finishes a partly vaulted file", func(t *testing.T) {
		config := writeDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-path", config, ".spec.config.registries[0].auth.pass", "--vault-password-file", passwordFile)
		require.NoError(t, err)
		partial, err := os.ReadFile(config)
		require.NoError(t, err)

		_, stderr, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--vault-password-file", passwordFile, "--no-color")
		require.NoError(t, err)
		require.Contains(t, stderr, "count=3", "the value encrypted already should be skipped")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, vaultValueAt(t, string(partial), "pass: |- # rotate me"), vaultValueAt(t, string(got), "pass: |- # rotate me"), "the existing ciphertext should be left as it was")
	})

	// The case an operator hits after rotating a registry credential: one value edited back to
	// plaintext, the rest still vaulted under the same password.
	t.Run("picks up an edited credential without disturbing the others", func(t *testing.T) {
		config := vaultedDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "decrypt-path", config, ".spec.config.registries[0].auth.pass", "--vault-password-file", passwordFile)
		require.NoError(t, err)
		edited, err := os.ReadFile(config)
		require.NoError(t, err)
		user := vaultValueAt(t, string(edited), "user: |-")

		_, stderr, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--vault-password-file", passwordFile, "--no-color")
		require.NoError(t, err)
		require.Contains(t, stderr, "count=1")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, user, vaultValueAt(t, string(got), "user: |-"), "the untouched username should keep its ciphertext")

		decrypted, err := vault.Decrypt(vaultValueAt(t, string(got), "pass: |- # rotate me"), password)
		require.NoError(t, err)
		require.Equal(t, "hunter2", decrypted)
	})

	// The same mixed shape, but vaulted under two different passwords, which is a document no
	// apply can read: it decrypts a registry's fields with one password.
	t.Run("refuses to mix vault passwords", func(t *testing.T) {
		config := vaultedDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "decrypt-path", config, ".spec.config.registries[0].auth.pass", "--vault-password-file", passwordFile)
		require.NoError(t, err)
		before, err := os.ReadFile(config)
		require.NoError(t, err)

		other := filepath.Join(t.TempDir(), "other-password")
		require.NoError(t, os.WriteFile(other, []byte("a different vault password"), 0o600))

		_, stderr, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--vault-password-file", other, "--no-color")
		require.Error(t, err)
		require.Contains(t, stderr, "encrypted with a different password")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(before), string(got), "a failed run should not touch the file")
	})

	t.Run("rotates the password a whole file is vaulted with", func(t *testing.T) {
		config := vaultedDoc(t)

		rotated := filepath.Join(t.TempDir(), "new-password")
		require.NoError(t, os.WriteFile(rotated, []byte("the new password"), 0o600))

		_, _, err := e2e.Cargoship(t, "vault", "decrypt-file", config, "--vault-password-file", passwordFile)
		require.NoError(t, err)
		_, _, err = e2e.Cargoship(t, "vault", "encrypt-file", config, "--vault-password-file", rotated)
		require.NoError(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		decrypted, err := vault.Decrypt(vaultValueAt(t, string(got), "pass: |- # rotate me"), "the new password")
		require.NoError(t, err)
		require.Equal(t, "hunter2", decrypted)
	})

	t.Run("dry run prints the document and leaves the file alone", func(t *testing.T) {
		config := writeDoc(t)

		stdout, _, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--vault-password-file", passwordFile, "--dry-run")
		require.NoError(t, err)
		require.Contains(t, stdout, "$ANSIBLE_VAULT")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, original, got)
	})

	t.Run("a dry-run decrypt checks every value without writing plaintext", func(t *testing.T) {
		config := vaultedDoc(t)
		before, err := os.ReadFile(config)
		require.NoError(t, err)

		stdout, _, err := e2e.Cargoship(t, "vault", "decrypt-file", config, "--vault-password-file", passwordFile, "--dry-run")
		require.NoError(t, err)
		require.Equal(t, string(original), stdout)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(before), string(got))
	})

	t.Run("errors on the wrong password", func(t *testing.T) {
		config := vaultedDoc(t)
		before, err := os.ReadFile(config)
		require.NoError(t, err)

		wrong := filepath.Join(t.TempDir(), "wrong-password")
		require.NoError(t, os.WriteFile(wrong, []byte("not the password"), 0o600))

		_, _, err = e2e.Cargoship(t, "vault", "decrypt-file", config, "--vault-password-file", wrong)
		require.Error(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(before), string(got), "a failed run should not touch the file")
	})

	t.Run("errors on a document with no registries", func(t *testing.T) {
		config := filepath.Join(t.TempDir(), "not-a-cluster.yaml")
		require.NoError(t, os.WriteFile(config, []byte("apiVersion: v1\nkind: ConfigMap\n"), 0o600))

		_, stderr, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--vault-password-file", passwordFile, "--no-color")
		require.Error(t, err)
		require.Contains(t, stderr, "is this a cluster configuration?")
	})

	t.Run("errors on a missing file", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-file", missing, "--vault-password-file", passwordFile)
		require.Error(t, err)
		_, _, err = e2e.Cargoship(t, "vault", "decrypt-file", missing, "--vault-password-file", passwordFile)
		require.Error(t, err)
	})
}
