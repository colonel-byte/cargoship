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
package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	vault "github.com/sosedoff/ansible-vault-go"
	"github.com/stretchr/testify/require"
)

// TestCargoshipVaultEncrypt exercises the `vault-encrypt` command with a value
// argument, a value piped over stdin, and its error paths.
func TestCargoshipVaultEncrypt(t *testing.T) {
	passwordFile := filepath.Join(t.TempDir(), "vault-password")
	const password = "supersecret"
	require.NoError(t, os.WriteFile(passwordFile, []byte(password), 0o600))

	t.Run("encrypts a value argument, round-trips with vault password", func(t *testing.T) {
		const value = "hello world"

		stdout, _, err := e2e.Cargoship(t, "vault-encrypt", "--vault-password-file", passwordFile, value)
		require.NoError(t, err)

		encrypted := strings.TrimSpace(stdout)
		require.True(t, strings.HasPrefix(encrypted, "$ANSIBLE_VAULT"))

		decrypted, err := vault.Decrypt(encrypted, password)
		require.NoError(t, err)
		require.Equal(t, value, decrypted)
	})

	t.Run("encrypts a value piped over stdin", func(t *testing.T) {
		const value = "piped-secret"

		cmd := exec.CommandContext(t.Context(), e2e.CargoBinPath, "vault-encrypt", "--vault-password-file", passwordFile, "--no-color")
		cmd.Stdin = strings.NewReader(value + "\n")
		out, err := cmd.Output()
		require.NoError(t, err)

		encrypted := strings.TrimSpace(string(out))
		require.True(t, strings.HasPrefix(encrypted, "$ANSIBLE_VAULT"))

		decrypted, err := vault.Decrypt(encrypted, password)
		require.NoError(t, err)
		require.Equal(t, value, decrypted)
	})

	t.Run("CARGOSHIP_VAULT_PASSWORD is used when the password file is empty", func(t *testing.T) {
		const value = "env-password-secret"
		t.Setenv("CARGOSHIP_VAULT_PASSWORD", password)

		// The flag is marked required, so it still has to be present -- an empty value
		// hands ResolveVaultPassword the fall-through to the environment.
		stdout, _, err := e2e.Cargoship(t, "vault-encrypt", "--vault-password-file", "", value)
		require.NoError(t, err)

		decrypted, err := vault.Decrypt(strings.TrimSpace(stdout), password)
		require.NoError(t, err)
		require.Equal(t, value, decrypted)
	})

	t.Run("ANSIBLE_VAULT_PASSWORD is used when CARGOSHIP_VAULT_PASSWORD is unset", func(t *testing.T) {
		const value = "ansible-env-password-secret"
		t.Setenv("CARGOSHIP_VAULT_PASSWORD", "")
		t.Setenv("ANSIBLE_VAULT_PASSWORD", password)

		stdout, _, err := e2e.Cargoship(t, "vault-encrypt", "--vault-password-file", "", value)
		require.NoError(t, err)

		decrypted, err := vault.Decrypt(strings.TrimSpace(stdout), password)
		require.NoError(t, err)
		require.Equal(t, value, decrypted)
	})

	t.Run("no password anywhere errors", func(t *testing.T) {
		t.Setenv("CARGOSHIP_VAULT_PASSWORD", "")
		t.Setenv("ANSIBLE_VAULT_PASSWORD", "")

		_, _, err := e2e.Cargoship(t, "vault-encrypt", "--vault-password-file", "", "value")
		require.Error(t, err)
	})

	t.Run("empty stdin errors", func(t *testing.T) {
		cmd := exec.CommandContext(t.Context(), e2e.CargoBinPath, "vault-encrypt", "--vault-password-file", passwordFile, "--no-color")
		cmd.Stdin = strings.NewReader("")
		require.Error(t, cmd.Run())
	})

	t.Run("missing password file errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "vault-encrypt", "--vault-password-file", filepath.Join(t.TempDir(), "does-not-exist"), "value")
		require.Error(t, err)
	})

	// --vault-password-file is marked required, so omitting it fails in cobra before
	// ResolveVaultPassword ever consults the environment.
	t.Run("missing password flag errors even with the env var set", func(t *testing.T) {
		t.Setenv("CARGOSHIP_VAULT_PASSWORD", password)

		_, _, err := e2e.Cargoship(t, "vault-encrypt", "value")
		require.Error(t, err)
	})

	t.Run("too many args errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "vault-encrypt", "--vault-password-file", passwordFile, "one", "two")
		require.Error(t, err)
	})
}
