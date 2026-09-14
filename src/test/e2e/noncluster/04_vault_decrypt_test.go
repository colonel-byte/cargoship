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
	"strings"
	"testing"

	vault "github.com/sosedoff/ansible-vault-go"
	"github.com/stretchr/testify/require"
)

// TestCargoshipVaultDecrypt exercises `vault decrypt`, the inverse of `vault encrypt`.
func TestCargoshipVaultDecrypt(t *testing.T) {
	passwordFile := filepath.Join(t.TempDir(), "vault-password")
	const password = "supersecret"
	require.NoError(t, os.WriteFile(passwordFile, []byte(password), 0o600))

	encrypt := func(t *testing.T, value string) string {
		t.Helper()
		encrypted, err := vault.Encrypt(value, password)
		require.NoError(t, err)
		return encrypted
	}

	t.Run("decrypts a value given as an argument", func(t *testing.T) {
		stdout, _, err := e2e.Cargoship(t, "vault", "decrypt", encrypt(t, "hunter2"), "--vault-password-file", passwordFile)
		require.NoError(t, err)
		require.Equal(t, "hunter2", strings.TrimRight(stdout, "\n"))
	})

	t.Run("round-trips a value through encrypt", func(t *testing.T) {
		encrypted, _, err := e2e.Cargoship(t, "vault", "encrypt", "hunter2", "--vault-password-file", passwordFile)
		require.NoError(t, err)

		stdout, _, err := e2e.Cargoship(t, "vault", "decrypt", strings.TrimRight(encrypted, "\n"), "--vault-password-file", passwordFile)
		require.NoError(t, err)
		require.Equal(t, "hunter2", strings.TrimRight(stdout, "\n"))
	})

	t.Run("keeps a multi-line value intact", func(t *testing.T) {
		const pem = "-----BEGIN CERTIFICATE-----\naGVsbG8gd29ybGQ=\n-----END CERTIFICATE-----\n"

		stdout, _, err := e2e.Cargoship(t, "vault", "decrypt", encrypt(t, pem), "--vault-password-file", passwordFile)
		require.NoError(t, err)
		require.Equal(t, pem, stdout, "a value ending in a newline should not gain a second one")
	})

	t.Run("takes the password from the environment", func(t *testing.T) {
		t.Setenv("CARGOSHIP_VAULT_PASSWORD", password)

		stdout, _, err := e2e.Cargoship(t, "vault", "decrypt", encrypt(t, "hunter2"))
		require.NoError(t, err)
		require.Equal(t, "hunter2", strings.TrimRight(stdout, "\n"))
	})

	t.Run("errors on the wrong password", func(t *testing.T) {
		t.Setenv("CARGOSHIP_VAULT_PASSWORD", "not the password")

		_, _, err := e2e.Cargoship(t, "vault", "decrypt", encrypt(t, "hunter2"))
		require.Error(t, err)
	})

	t.Run("errors on a value that is not encrypted", func(t *testing.T) {
		_, stderr, err := e2e.Cargoship(t, "vault", "decrypt", "hunter2", "--vault-password-file", passwordFile, "--no-color")
		require.Error(t, err)
		require.Contains(t, stderr, "not Ansible Vault-encrypted")
	})

	t.Run("errors without a vault password", func(t *testing.T) {
		t.Setenv("CARGOSHIP_VAULT_PASSWORD", "")
		t.Setenv("ANSIBLE_VAULT_PASSWORD", "")

		_, _, err := e2e.Cargoship(t, "vault", "decrypt", "anything")
		require.Error(t, err)
	})
}
