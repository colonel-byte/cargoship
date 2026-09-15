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
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"filippo.io/age/armor"
	"github.com/stretchr/testify/require"
)

// ageHeader is the first line of an armored age value, spelled out here rather than imported so
// that the test asserts on what an operator sees in the file.
const ageHeader = "-----BEGIN AGE ENCRYPTED FILE-----"

// ageKey is one age key pair and the files a command is pointed at to use it.
type ageKey struct {
	identity      *age.X25519Identity
	recipient     string
	identityFile  string
	recipientFile string
}

// newAgeKey generates a key pair and writes it out the way age-keygen does, so the commands under
// test are reading the file format an operator actually has.
func newAgeKey(t *testing.T) ageKey {
	t.Helper()

	id, err := age.GenerateX25519Identity()
	require.NoError(t, err)

	dir := t.TempDir()
	key := ageKey{
		identity:      id,
		recipient:     id.Recipient().String(),
		identityFile:  filepath.Join(dir, "key.txt"),
		recipientFile: filepath.Join(dir, "recipients.txt"),
	}
	require.NoError(t, os.WriteFile(key.identityFile,
		[]byte("# created by age-keygen\n# public key: "+key.recipient+"\n"+id.String()+"\n"), 0o600))
	require.NoError(t, os.WriteFile(key.recipientFile,
		[]byte("# the platform team\n"+key.recipient+"\n"), 0o600))
	return key
}

// decryptAge reads an armored value with the age library directly, so a round trip through
// cargoship is checked against age itself rather than only against cargoship.
func decryptAge(t *testing.T, value string, identities ...age.Identity) string {
	t.Helper()

	r, err := age.Decrypt(armor.NewReader(strings.NewReader(value)), identities...)
	require.NoError(t, err)
	plain, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(plain)
}

// cargoshipStdin runs a command with value on standard input, which is how an age value is handed
// to the value commands.
//
// An armored age value begins with "-----BEGIN AGE ENCRYPTED FILE-----", so passing one as an
// argument makes the shell and the flag parser both read it as a flag. Ansible Vault ciphertext
// begins with "$ANSIBLE_VAULT" and never had the problem. Piping is the ordinary way around it;
// "--" after the flags works too, and has its own subtest.
func cargoshipStdin(t *testing.T, value string, args ...string) (string, string, error) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), e2e.CargoBinPath, append(args, "--no-color")...)
	cmd.Stdin = strings.NewReader(value)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// clearKeyEnv empties every environment variable the key resolution reads, so a subtest says what
// it means whatever the machine running it already has set.
func clearKeyEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"CARGOSHIP_VAULT_PASSWORD",
		"ANSIBLE_VAULT_PASSWORD",
		"CARGOSHIP_AGE_IDENTITY_FILE",
		"CARGOSHIP_AGE_RECIPIENTS",
	} {
		t.Setenv(name, "")
	}
}

// TestCargoshipAgeEncrypt exercises the value commands with age keys: which format comes out, where
// the keys may come from, and the one combination that is refused.
func TestCargoshipAgeEncrypt(t *testing.T) {
	key := newAgeKey(t)

	t.Run("encrypts to a recipient and round-trips with age itself", func(t *testing.T) {
		clearKeyEnv(t)
		const value = "hello world"

		stdout, _, err := e2e.Cargoship(t, "vault", "encrypt", value, "--age-recipient", key.recipient)
		require.NoError(t, err)

		encrypted := strings.TrimSpace(stdout)
		require.True(t, strings.HasPrefix(encrypted, ageHeader), "got %q", encrypted)
		require.Equal(t, value, decryptAge(t, encrypted+"\n", key.identity))
	})

	t.Run("round-trips through decrypt with an identity file", func(t *testing.T) {
		clearKeyEnv(t)

		encrypted, _, err := e2e.Cargoship(t, "vault", "encrypt", "hunter2", "--age-recipient", key.recipient)
		require.NoError(t, err)

		stdout, stderr, err := cargoshipStdin(t, encrypted, "vault", "decrypt", "--age-identity-file", key.identityFile)
		require.NoError(t, err, stderr)
		require.Equal(t, "hunter2", strings.TrimRight(stdout, "\n"))
	})

	// The same value as an argument, for the operator who has it in a variable rather than a pipe.
	// "--" has to come after the flags: everything past it is a positional argument.
	t.Run("decrypts a value given as an argument after a double dash", func(t *testing.T) {
		clearKeyEnv(t)

		encrypted, _, err := e2e.Cargoship(t, "vault", "encrypt", "hunter2", "--age-recipient", key.recipient)
		require.NoError(t, err)

		stdout, _, err := e2e.Cargoship(t, "vault", "decrypt", "--age-identity-file", key.identityFile, "--no-color", "--", strings.TrimSpace(encrypted))
		require.NoError(t, err)
		require.Equal(t, "hunter2", strings.TrimRight(stdout, "\n"))
	})

	t.Run("encrypts to every recipient in a recipients file", func(t *testing.T) {
		clearKeyEnv(t)
		second := newAgeKey(t)

		stdout, _, err := e2e.Cargoship(t, "vault", "encrypt", "shared",
			"--age-recipients-file", key.recipientFile, "--age-recipient", second.recipient)
		require.NoError(t, err)

		encrypted := strings.TrimSpace(stdout) + "\n"
		// Each holder reads it alone. That is the key model age is wanted for: no shared secret
		// between the two of them.
		require.Equal(t, "shared", decryptAge(t, encrypted, key.identity))
		require.Equal(t, "shared", decryptAge(t, encrypted, second.identity))
	})

	t.Run("reads keys from the environment", func(t *testing.T) {
		clearKeyEnv(t)
		t.Setenv("CARGOSHIP_AGE_RECIPIENTS", key.recipient)
		t.Setenv("CARGOSHIP_AGE_IDENTITY_FILE", key.identityFile)

		encrypted, _, err := e2e.Cargoship(t, "vault", "encrypt", "env-secret")
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(strings.TrimSpace(encrypted), ageHeader))

		stdout, stderr, err := cargoshipStdin(t, encrypted, "vault", "decrypt")
		require.NoError(t, err, stderr)
		require.Equal(t, "env-secret", strings.TrimRight(stdout, "\n"))
	})

	// The common case on a machine already set up for vault: the password is in a shell profile,
	// and an operator passing --age-recipient means what they said.
	t.Run("an age recipient beats a vault password in the environment", func(t *testing.T) {
		clearKeyEnv(t)
		t.Setenv("CARGOSHIP_VAULT_PASSWORD", "supersecret")

		stdout, _, err := e2e.Cargoship(t, "vault", "encrypt", "hunter2", "--age-recipient", key.recipient)
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(strings.TrimSpace(stdout), ageHeader))
	})

	// Two explicit flags is the one case worth stopping for: a value is written in a single format,
	// and choosing writes the secret under a key the operator did not pick.
	t.Run("refuses an age recipient and a vault password file together", func(t *testing.T) {
		clearKeyEnv(t)
		passwordFile := filepath.Join(t.TempDir(), "vault-password")
		require.NoError(t, os.WriteFile(passwordFile, []byte("supersecret"), 0o600))

		_, stderr, err := e2e.Cargoship(t, "vault", "encrypt", "hunter2",
			"--age-recipient", key.recipient, "--vault-password-file", passwordFile)
		require.Error(t, err)
		require.Contains(t, stderr, "pass one or the other")
	})

	t.Run("decrypt names the key an age value needs", func(t *testing.T) {
		clearKeyEnv(t)
		passwordFile := filepath.Join(t.TempDir(), "vault-password")
		require.NoError(t, os.WriteFile(passwordFile, []byte("supersecret"), 0o600))

		encrypted, _, err := e2e.Cargoship(t, "vault", "encrypt", "hunter2", "--age-recipient", key.recipient)
		require.NoError(t, err)

		_, stderr, err := cargoshipStdin(t, encrypted, "vault", "decrypt", "--vault-password-file", passwordFile)
		require.Error(t, err)
		require.Contains(t, stderr, "--age-identity-file")
	})

	t.Run("reports a value encrypted to someone else's key", func(t *testing.T) {
		clearKeyEnv(t)
		other := newAgeKey(t)

		encrypted, _, err := e2e.Cargoship(t, "vault", "encrypt", "hunter2", "--age-recipient", key.recipient)
		require.NoError(t, err)

		_, stderr, err := cargoshipStdin(t, encrypted, "vault", "decrypt", "--age-identity-file", other.identityFile)
		require.Error(t, err)
		require.Contains(t, stderr, "none of the configured age identities")
	})
}

// TestCargoshipAgeEncryptFile exercises the file commands with age keys, including the migration
// off Ansible Vault, which is a rekey rather than a command of its own.
func TestCargoshipAgeEncryptFile(t *testing.T) {
	key := newAgeKey(t)

	passwordFile := filepath.Join(t.TempDir(), "vault-password")
	const password = "supersecret"
	require.NoError(t, os.WriteFile(passwordFile, []byte(password), 0o600))

	original, err := os.ReadFile(vaultInventory)
	require.NoError(t, err)

	writeDoc := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "cluster.yaml")
		require.NoError(t, os.WriteFile(path, original, 0o600))
		return path
	}

	t.Run("encrypts every credential to the recipient", func(t *testing.T) {
		clearKeyEnv(t)
		config := writeDoc(t)

		_, stderr, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--age-recipient", key.recipient)
		require.NoError(t, err)
		require.Contains(t, stderr, "count=4")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Contains(t, string(got), ageHeader)
		for _, plaintext := range []string{"hunter2", "quay-token", "aGVsbG8gd29ybGQ="} {
			require.NotContains(t, string(got), plaintext, "%q should not be left in the clear", plaintext)
		}
		// Nothing outside the credentials moves.
		require.Contains(t, string(got), "loadbalancer: lb.example.com")
		require.Contains(t, string(got), "# our mirror")
	})

	t.Run("decrypt-file restores the document", func(t *testing.T) {
		clearKeyEnv(t)
		config := writeDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--age-recipient", key.recipient)
		require.NoError(t, err)
		_, _, err = e2e.Cargoship(t, "vault", "decrypt-file", config, "--age-identity-file", key.identityFile)
		require.NoError(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(original), string(got))
	})

	t.Run("encrypt-path and decrypt-path walk one value", func(t *testing.T) {
		clearKeyEnv(t)
		config := writeDoc(t)
		const path = "$.spec.config.registries[0].auth.pass"

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-path", config, path, "--age-recipient", key.recipient)
		require.NoError(t, err)

		encrypted, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Contains(t, string(encrypted), ageHeader)
		require.NotContains(t, string(encrypted), "hunter2")
		// Only the one path moved: the token beside it is still in the clear.
		require.Contains(t, string(encrypted), "quay-token")

		_, _, err = e2e.Cargoship(t, "vault", "decrypt-path", config, path, "--age-identity-file", key.identityFile)
		require.NoError(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(original), string(got))
	})

	// The mistake the skip reporting exists for. Nothing here is an error, the file is already
	// encrypted, and without the warning the run that did nothing would be the quietest output the
	// command has.
	t.Run("encrypting an age file to new recipients warns instead of silently doing nothing", func(t *testing.T) {
		clearKeyEnv(t)
		config := writeDoc(t)
		other := newAgeKey(t)

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--age-recipient", key.recipient)
		require.NoError(t, err)

		before, err := os.ReadFile(config)
		require.NoError(t, err)

		_, stderr, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--age-recipient", other.recipient)
		require.NoError(t, err)
		require.Contains(t, stderr, "WRN")
		require.Contains(t, stderr, "does not record its recipients")
		require.Contains(t, stderr, "rekey")
		require.Contains(t, stderr, "nothing to encrypt")
		require.Contains(t, stderr, "auth.pass")

		after, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(before), string(after), "the file should be untouched")

		// And the command it names does the job.
		_, stderr, err = e2e.Cargoship(t, "vault", "rekey", config,
			"--age-identity-file", key.identityFile, "--age-recipient", other.recipient)
		require.NoError(t, err)
		require.Contains(t, stderr, "count=4")

		_, _, err = e2e.Cargoship(t, "vault", "decrypt-file", config, "--age-identity-file", other.identityFile)
		require.NoError(t, err)
		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(original), string(got))
	})

	t.Run("encrypting a vaulted file to age recipients names rekey", func(t *testing.T) {
		clearKeyEnv(t)
		config := writeDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--vault-password-file", passwordFile)
		require.NoError(t, err)

		_, stderr, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--age-recipient", key.recipient)
		require.NoError(t, err)
		require.Contains(t, stderr, "does not move a credential between formats")
		require.Contains(t, stderr, "rekey")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Contains(t, string(got), "$ANSIBLE_VAULT")
		require.NotContains(t, string(got), ageHeader)
	})

	// Warnings go to stderr, so a dry run piped somewhere still gets a document and not a document
	// with a complaint in the middle of it.
	t.Run("a dry run warns on stderr and leaves stdout clean", func(t *testing.T) {
		clearKeyEnv(t)
		config := writeDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--age-recipient", key.recipient)
		require.NoError(t, err)

		stdout, stderr, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--age-recipient", key.recipient, "--dry-run")
		require.NoError(t, err)
		require.Contains(t, stderr, "does not record its recipients")
		require.NotContains(t, stdout, "does not record its recipients")
		require.Contains(t, stdout, "apiVersion:")
	})

	// The migration path, and the reason it needs no command of its own: the old password reads,
	// the recipients write, and the plaintext never lands on disk.
	t.Run("rekey moves a vaulted file onto age", func(t *testing.T) {
		clearKeyEnv(t)
		config := writeDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--vault-password-file", passwordFile)
		require.NoError(t, err)

		_, stderr, err := e2e.Cargoship(t, "vault", "rekey", config,
			"--vault-password-file", passwordFile, "--age-recipient", key.recipient)
		require.NoError(t, err)
		require.Contains(t, stderr, "count=4")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Contains(t, string(got), ageHeader)
		require.NotContains(t, string(got), "$ANSIBLE_VAULT")
		require.NotContains(t, string(got), "hunter2")

		// And what it wrote is readable with the new key alone.
		_, _, err = e2e.Cargoship(t, "vault", "decrypt-file", config, "--age-identity-file", key.identityFile)
		require.NoError(t, err)
		got, err = os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(original), string(got))
	})
}
