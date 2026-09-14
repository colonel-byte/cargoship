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

// vaultPathDoc is a cluster configuration carrying the shapes vault encrypt-path has to rewrite:
// a scalar with a trailing comment, and a multi-line PEM block with a sibling key after it.
const vaultPathDoc = `apiVersion: zarf.dev/v1alpha1
kind: ZarfCluster
spec:
  # registries the cluster pulls from
  config:
    loadbalancer: lb.example.com
    registries:
      - name: harbor # our mirror
        auth:
          user: admin
          pass: hunter2 # rotate me
        tls:
          ca: |
            -----BEGIN CERTIFICATE-----
            aGVsbG8gd29ybGQ=
            -----END CERTIFICATE-----
          insecureSkipVerify: false
  hosts:
    - name: node1
`

// TestCargoshipVaultEncryptPath exercises the `vault encrypt-path` command against a config file
// on disk: what it writes back, what it leaves alone, and its error paths.
func TestCargoshipVaultEncryptPath(t *testing.T) {
	passwordFile := filepath.Join(t.TempDir(), "vault-password")
	const password = "supersecret"
	require.NoError(t, os.WriteFile(passwordFile, []byte(password), 0o600))

	// writeDoc puts a fresh copy of vaultPathDoc in its own directory, so that one subtest's
	// rewrite cannot be seen by another.
	writeDoc := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "cluster.yaml")
		require.NoError(t, os.WriteFile(path, []byte(vaultPathDoc), 0o600))
		return path
	}

	t.Run("encrypts a scalar in place, round-trips with vault password", func(t *testing.T) {
		config := writeDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-path", config, ".spec.config.registries[0].auth.pass", "--vault-password-file", passwordFile)
		require.NoError(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Contains(t, string(got), "pass: |- # rotate me", "the trailing comment should survive")
		require.Contains(t, string(got), "  # registries the cluster pulls from", "comments elsewhere should survive")
		require.Contains(t, string(got), "      - name: harbor # our mirror")

		decrypted, err := vault.Decrypt(vaultValueAt(t, string(got), "pass: |-"), password)
		require.NoError(t, err)
		require.Equal(t, "hunter2", decrypted)
	})

	t.Run("encrypts a multi-line literal block without truncating it", func(t *testing.T) {
		config := writeDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-path", config, ".spec.config.registries[0].tls.ca", "--vault-password-file", passwordFile)
		require.NoError(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Contains(t, string(got), "          insecureSkipVerify: false", "the key after the block should survive")

		decrypted, err := vault.Decrypt(vaultValueAt(t, string(got), "ca: |-"), password)
		require.NoError(t, err)
		require.Equal(t, "-----BEGIN CERTIFICATE-----\naGVsbG8gd29ybGQ=\n-----END CERTIFICATE-----\n", decrypted)
	})

	t.Run("dry run prints the document and leaves the file alone", func(t *testing.T) {
		config := writeDoc(t)

		stdout, _, err := e2e.Cargoship(t, "vault", "encrypt-path", config, ".spec.config.registries[0].auth.pass", "--vault-password-file", passwordFile, "--dry-run")
		require.NoError(t, err)
		require.Contains(t, stdout, "pass: |- # rotate me")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, vaultPathDoc, string(got))
	})

	t.Run("warns when the path is not one cargoship decrypts", func(t *testing.T) {
		config := writeDoc(t)

		_, stderr, err := e2e.Cargoship(t, "vault", "encrypt-path", config, ".spec.config.loadbalancer", "--vault-password-file", passwordFile, "--dry-run")
		require.NoError(t, err, "an unsupported path is a warning, not a failure")
		require.Contains(t, stderr, "does not decrypt this field at apply time")
	})

	t.Run("refuses to encrypt an already-encrypted value without --force", func(t *testing.T) {
		config := writeDoc(t)
		const path = ".spec.config.registries[0].auth.pass"

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-path", config, path, "--vault-password-file", passwordFile)
		require.NoError(t, err)

		_, stderr, err := e2e.Cargoship(t, "vault", "encrypt-path", config, path, "--vault-password-file", passwordFile, "--no-color")
		require.Error(t, err)
		require.Contains(t, stderr, "already Ansible Vault-encrypted")
		require.Contains(t, stderr, "--force")
	})

	t.Run("encrypts several paths in one run", func(t *testing.T) {
		config := writeDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-path", config,
			".spec.config.registries[0].auth.user",
			".spec.config.registries[0].auth.pass",
			".spec.config.registries[0].tls.ca",
			"--vault-password-file", passwordFile)
		require.NoError(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Contains(t, string(got), "pass: |- # rotate me", "the trailing comment should survive")
		require.Contains(t, string(got), "          insecureSkipVerify: false", "the key after the CA block should survive")
		require.Contains(t, string(got), "    loadbalancer: lb.example.com", "a path that was not named should be left alone")

		for marker, want := range map[string]string{
			"user: |-": "admin",
			"pass: |-": "hunter2",
			"ca: |-":   "-----BEGIN CERTIFICATE-----\naGVsbG8gd29ybGQ=\n-----END CERTIFICATE-----\n",
		} {
			decrypted, err := vault.Decrypt(vaultValueAt(t, string(got), marker), password)
			require.NoError(t, err, "value at %q should decrypt", marker)
			require.Equal(t, want, decrypted)
		}
	})

	// The whole file is written once, at the end, so a run that cannot finish leaves nothing behind
	// -- which is what makes it safe to name several paths at a time.
	t.Run("writes nothing when one of several paths fails", func(t *testing.T) {
		config := writeDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-path", config,
			".spec.config.registries[0].auth.pass",
			".spec.config.registries[0].auth.nope",
			"--vault-password-file", passwordFile)
		require.Error(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, vaultPathDoc, string(got), "the path that did encrypt should not have been written")
	})

	t.Run("rejects the same path named twice, however it is spelled", func(t *testing.T) {
		config := writeDoc(t)

		_, stderr, err := e2e.Cargoship(t, "vault", "encrypt-path", config,
			".spec.config.registries[0].auth.pass",
			"spec.config.registries[0].auth.pass",
			"--vault-password-file", passwordFile, "--no-color")
		require.Error(t, err)
		require.Contains(t, stderr, "name the same value")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, vaultPathDoc, string(got), "a failed run should not touch the file")
	})

	t.Run("errors on a missing file", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-path", missing, ".spec.config.loadbalancer", "--vault-password-file", passwordFile)
		require.Error(t, err)
	})

	t.Run("errors on a path the document does not have", func(t *testing.T) {
		config := writeDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-path", config, ".spec.config.registries[0].auth.nope", "--vault-password-file", passwordFile)
		require.Error(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, vaultPathDoc, string(got), "a failed run should not touch the file")
	})

	t.Run("errors without a vault password", func(t *testing.T) {
		config := writeDoc(t)
		t.Setenv("CARGOSHIP_VAULT_PASSWORD", "")
		t.Setenv("ANSIBLE_VAULT_PASSWORD", "")

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-path", config, ".spec.config.registries[0].auth.pass")
		require.Error(t, err)
	})
}
