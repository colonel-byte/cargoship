package noncluster

import (
	"os"
	"path/filepath"
	"testing"

	vault "github.com/sosedoff/ansible-vault-go"
	"github.com/stretchr/testify/require"
)

const vaultInventory = "src/test/e2e/noncluster/testdata/inventory-vault.yaml"

// anchoredInventoryFixture is a full inventory that leans on anchors, aliases and merge keys the
// way an operator's does: one robot account two mirrors share, one trust bundle three registries
// share, and one block of SSH defaults every host merges.
const anchoredInventoryFixture = "src/test/e2e/noncluster/testdata/inventory-anchors.yaml"

// TestCargoshipVaultAnchoredInventory runs the file commands over that inventory. A shared value
// is one value however many registries name it, so it is encrypted once, rekeyed once, and every
// alias and merge key that reaches it is still there afterwards.
func TestCargoshipVaultAnchoredInventory(t *testing.T) {
	dir := t.TempDir()
	passwordFile := filepath.Join(dir, "vault-password")
	const password = "supersecret"
	require.NoError(t, os.WriteFile(passwordFile, []byte(password), 0o600))

	newPasswordFile := filepath.Join(dir, "new-vault-password")
	const newPassword = "the password we are moving to"
	require.NoError(t, os.WriteFile(newPasswordFile, []byte(newPassword), 0o600))

	original, err := os.ReadFile(anchoredInventoryFixture)
	require.NoError(t, err)

	// The fixture is copied rather than used in place: these commands rewrite the file they are
	// given, and the checked-in one has to stay as it is.
	writeDoc := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "inventory-anchors.yaml")
		require.NoError(t, os.WriteFile(path, original, 0o600))
		return path
	}

	vaultedDoc := func(t *testing.T) string {
		t.Helper()
		config := writeDoc(t)
		_, _, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--vault-password-file", passwordFile)
		require.NoError(t, err)
		return config
	}

	// Six values, not eight: the account and the trust bundle the mirrors share are each named
	// twice in the document but are one value, and the registry that merges the trust bundle
	// reaches the same one.
	t.Run("encrypts each shared value once", func(t *testing.T) {
		config := writeDoc(t)

		_, stderr, err := e2e.Cargoship(t, "vault", "encrypt-file", config, "--vault-password-file", passwordFile, "--no-color")
		require.NoError(t, err)
		require.Contains(t, stderr, "count=6")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		for _, plaintext := range []string{"hunter2", "correct-horse", "ghcr-token", "aGVsbG8gd29ybGQ="} {
			require.NotContains(t, string(got), plaintext, "%q should not be left in the clear", plaintext)
		}

		decrypted, err := vault.Decrypt(vaultValueAt(t, string(got), "pass: |-"), password)
		require.NoError(t, err)
		require.Equal(t, "hunter2", decrypted)
	})

	t.Run("leaves the anchors, the aliases and the merge keys where they were", func(t *testing.T) {
		config := vaultedDoc(t)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		for _, marker := range []string{
			"auth: &robot-auth",
			"auth: *robot-auth",
			"tls: &internal-tls",
			"tls: *internal-tls",
			"          <<: *internal-tls",
			"        <<: &ssh-defaults",
			"        <<: *ssh-defaults",
		} {
			require.Contains(t, string(got), marker, "%q should survive encryption", marker)
		}

		require.Contains(t, string(got), "token: |- # rotate me", "the trailing comment should survive")
		require.Contains(t, string(got), "          insecureSkipVerify: true", "the key after a merge should survive")
		require.Contains(t, string(got), "    loadbalancer: e2e-anchors.test.local", "a field cargoship never encrypts should be left alone")
		require.Contains(t, string(got), "        address: 10.255.255.2", "the host that overrides one default should keep it")
	})

	t.Run("rekeys each shared value once and moves it to the new password", func(t *testing.T) {
		config := vaultedDoc(t)

		_, stderr, err := e2e.Cargoship(t, "vault", "rekey", config, "--vault-password-file", passwordFile, "--new-vault-password-file", newPasswordFile, "--no-color")
		require.NoError(t, err)
		require.Contains(t, stderr, "count=6")

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		for _, marker := range []string{"user: |-", "pass: |-", "ca: |-", "token: |- # rotate me"} {
			decrypted, err := vault.Decrypt(vaultValueAt(t, string(got), marker), newPassword)
			require.NoError(t, err, "value at %q should decrypt with the new password", marker)
			require.NotEmpty(t, decrypted)
		}

		// Decrypting under the new password is the only way back: a shared value rekeyed once is
		// rekeyed for every registry that names it.
		_, _, err = e2e.Cargoship(t, "vault", "decrypt-file", config, "--vault-password-file", newPasswordFile)
		require.NoError(t, err)

		got, err = os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(original), string(got))
	})

	t.Run("round-trips back to the file that went in", func(t *testing.T) {
		config := vaultedDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "decrypt-file", config, "--vault-password-file", passwordFile)
		require.NoError(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.Equal(t, string(original), string(got))
	})

	// A YAML path that lands on an alias names the value the anchor holds, so encrypting through
	// one is encrypting the block both mirrors read.
	t.Run("encrypt-path through an alias reaches the shared value", func(t *testing.T) {
		config := writeDoc(t)

		_, _, err := e2e.Cargoship(t, "vault", "encrypt-path", config, ".spec.config.registries[1].auth.pass", "--vault-password-file", passwordFile)
		require.NoError(t, err)

		got, err := os.ReadFile(config)
		require.NoError(t, err)
		require.NotContains(t, string(got), "pass: hunter2")
		require.Contains(t, string(got), "auth: *robot-auth", "the alias should survive")

		decrypted, err := vault.Decrypt(vaultValueAt(t, string(got), "pass: |-"), password)
		require.NoError(t, err)
		require.Equal(t, "hunter2", decrypted)
	})
}
