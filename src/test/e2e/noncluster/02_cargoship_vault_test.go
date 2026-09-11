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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	vault "github.com/sosedoff/ansible-vault-go"
	"github.com/stretchr/testify/require"
)

// TestCargoshipVaultEncrypt exercises the `vault encrypt` command with a value
// argument, a value piped over stdin, and its error paths.
func TestCargoshipVaultEncrypt(t *testing.T) {
	passwordFile := filepath.Join(t.TempDir(), "vault-password")
	const password = "supersecret"
	require.NoError(t, os.WriteFile(passwordFile, []byte(password), 0o600))

	t.Run("encrypts a value argument, round-trips with vault password", func(t *testing.T) {
		const value = "hello world"

		stdout, _, err := e2e.Cargoship(t, "vault", "encrypt", "--vault-password-file", passwordFile, value)
		require.NoError(t, err)

		encrypted := strings.TrimSpace(stdout)
		require.True(t, strings.HasPrefix(encrypted, "$ANSIBLE_VAULT"))

		decrypted, err := vault.Decrypt(encrypted, password)
		require.NoError(t, err)
		require.Equal(t, value, decrypted)
	})

	t.Run("encrypts a value piped over stdin", func(t *testing.T) {
		const value = "piped-secret"

		cmd := exec.CommandContext(t.Context(), e2e.CargoBinPath, "vault", "encrypt", "--vault-password-file", passwordFile, "--no-color")
		cmd.Stdin = strings.NewReader(value + "\n")
		out, err := cmd.Output()
		require.NoError(t, err)

		encrypted := strings.TrimSpace(string(out))
		require.True(t, strings.HasPrefix(encrypted, "$ANSIBLE_VAULT"))

		decrypted, err := vault.Decrypt(encrypted, password)
		require.NoError(t, err)
		require.Equal(t, value, decrypted)
	})

	t.Run("CARGOSHIP_VAULT_PASSWORD is used when the password file is omitted", func(t *testing.T) {
		const value = "env-password-secret"
		t.Setenv("CARGOSHIP_VAULT_PASSWORD", password)

		stdout, _, err := e2e.Cargoship(t, "vault", "encrypt", value)
		require.NoError(t, err)

		decrypted, err := vault.Decrypt(strings.TrimSpace(stdout), password)
		require.NoError(t, err)
		require.Equal(t, value, decrypted)
	})

	t.Run("ANSIBLE_VAULT_PASSWORD is used when CARGOSHIP_VAULT_PASSWORD is unset", func(t *testing.T) {
		const value = "ansible-env-password-secret"
		t.Setenv("CARGOSHIP_VAULT_PASSWORD", "")
		t.Setenv("ANSIBLE_VAULT_PASSWORD", password)

		stdout, _, err := e2e.Cargoship(t, "vault", "encrypt", value)
		require.NoError(t, err)

		decrypted, err := vault.Decrypt(strings.TrimSpace(stdout), password)
		require.NoError(t, err)
		require.Equal(t, value, decrypted)
	})

	// Passing the flag with an empty value was the only way to reach the environment
	// while --vault-password-file was still marked required. It has to keep working so
	// scripts written against that behavior do not break.
	t.Run("an empty password file flag still falls through to the environment", func(t *testing.T) {
		const value = "empty-flag-secret"
		t.Setenv("CARGOSHIP_VAULT_PASSWORD", password)

		stdout, _, err := e2e.Cargoship(t, "vault", "encrypt", "--vault-password-file", "", value)
		require.NoError(t, err)

		decrypted, err := vault.Decrypt(strings.TrimSpace(stdout), password)
		require.NoError(t, err)
		require.Equal(t, value, decrypted)
	})

	t.Run("no password anywhere errors", func(t *testing.T) {
		t.Setenv("CARGOSHIP_VAULT_PASSWORD", "")
		t.Setenv("ANSIBLE_VAULT_PASSWORD", "")

		_, stderr, err := e2e.Cargoship(t, "vault", "encrypt", "value")
		require.Error(t, err)
		require.Contains(t, stderr, "no vault password found")
	})

	t.Run("empty stdin errors", func(t *testing.T) {
		cmd := exec.CommandContext(t.Context(), e2e.CargoBinPath, "vault", "encrypt", "--vault-password-file", passwordFile, "--no-color")
		cmd.Stdin = strings.NewReader("")
		require.Error(t, cmd.Run())
	})

	t.Run("missing password file errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "vault", "encrypt", "--vault-password-file", filepath.Join(t.TempDir(), "does-not-exist"), "value")
		require.Error(t, err)
	})

	t.Run("too many args errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "vault", "encrypt", "--vault-password-file", passwordFile, "one", "two")
		require.Error(t, err)
	})
}

// TestCargoshipVaultEncryptDeprecatedAlias checks that the top-level spelling the vault group
// replaced still works for scripts that predate it, and says so when it runs.
func TestCargoshipVaultEncryptDeprecatedAlias(t *testing.T) {
	passwordFile := filepath.Join(t.TempDir(), "vault-password")
	const password = "supersecret"
	require.NoError(t, os.WriteFile(passwordFile, []byte(password), 0o600))

	t.Run("still encrypts, and warns that it is deprecated", func(t *testing.T) {
		const value = "legacy-secret"

		stdout, stderr, err := e2e.Cargoship(t, "vault-encrypt", "--vault-password-file", passwordFile, value, "--no-color")
		require.NoError(t, err)
		require.Contains(t, stderr, `use "cargoship vault encrypt" instead`)

		decrypted, err := vault.Decrypt(strings.TrimSpace(stdout), password)
		require.NoError(t, err)
		require.Equal(t, value, decrypted)
	})

	t.Run("is not offered in help", func(t *testing.T) {
		stdout, _, err := e2e.Cargoship(t, "--help")
		require.NoError(t, err)
		require.NotContains(t, stdout, "vault-encrypt")
		require.Contains(t, stdout, "vault ")
	})
}

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

// vaultValueAt returns the ciphertext of the block scalar introduced by header, undoing the
// indentation the document stores it with so that it can be handed back to the vault library.
func vaultValueAt(t *testing.T, doc, header string) string {
	t.Helper()

	lines := strings.Split(doc, "\n")
	start := -1
	for i, line := range lines {
		if strings.Contains(line, header) {
			start = i + 1
			break
		}
	}
	require.NotEqual(t, -1, start, "no %q in:\n%s", header, doc)
	require.Less(t, start, len(lines), "nothing follows %q", header)

	indent := len(lines[start]) - len(strings.TrimLeft(lines[start], " "))
	var body []string
	for _, line := range lines[start:] {
		if strings.TrimSpace(line) == "" || len(line)-len(strings.TrimLeft(line, " ")) < indent {
			break
		}
		body = append(body, line[indent:])
	}

	value := strings.Join(body, "\n")
	require.True(t, strings.HasPrefix(value, "$ANSIBLE_VAULT"), "value at %q is not ciphertext: %q", header, value)
	return value
}

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
