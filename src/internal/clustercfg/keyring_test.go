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

package clustercfg

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	vault "github.com/sosedoff/ansible-vault-go"
)

// writeFile writes content to a file in a temporary directory and returns its path.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	return path
}

// clearKeyEnv empties every environment variable ResolveKeyring reads, so that a test says what it
// means on a developer machine that already has one of them set for real work.
func clearKeyEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		VaultPasswordEnvVar,
		AnsibleVaultPasswordEnvVar,
		AgeIdentityFileEnvVar,
		AgeRecipientsEnvVar,
	} {
		t.Setenv(name, "")
	}
}

func TestFormatOf(t *testing.T) {
	id := newAgeIdentity(t)

	vaulted, err := vault.Encrypt("hunter2", testPassword)
	if err != nil {
		t.Fatalf("vault.Encrypt() error = %v", err)
	}
	aged, err := encryptAge("hunter2", []age.Recipient{id.Recipient()})
	if err != nil {
		t.Fatalf("encryptAge() error = %v", err)
	}

	tests := []struct {
		name          string
		value         string
		want          Format
		wantEncrypted bool
	}{
		{"vault", vaulted, FormatVault, true},
		{"age", aged, FormatAge, true},
		{"plaintext", "hunter2", "", false},
		{"empty", "", "", false},
		{"looks like a certificate", "-----BEGIN CERTIFICATE-----\naGk=\n-----END CERTIFICATE-----\n", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format, encrypted := FormatOf(tt.value)
			if format != tt.want || encrypted != tt.wantEncrypted {
				t.Errorf("FormatOf() = %q, %v, want %q, %v", format, encrypted, tt.want, tt.wantEncrypted)
			}
		})
	}
}

func TestResolveKeyringFromFlags(t *testing.T) {
	clearKeyEnv(t)
	id := newAgeIdentity(t)

	k, err := ResolveKeyring(KeyOptions{
		VaultPasswordFile: writeFile(t, "vault-pass", "filepass\n"),
		AgeIdentityFiles:  []string{writeFile(t, "key.txt", id.String()+"\n")},
		AgeRecipients:     []string{id.Recipient().String()},
	})
	if err != nil {
		t.Fatalf("ResolveKeyring() error = %v", err)
	}

	if k.vaultPassword != "filepass" {
		t.Errorf("vaultPassword = %q, want %q", k.vaultPassword, "filepass")
	}
	if len(k.identities) != 1 {
		t.Errorf("len(identities) = %d, want 1", len(k.identities))
	}
	if len(k.recipients) != 1 {
		t.Errorf("len(recipients) = %d, want 1", len(k.recipients))
	}
	if !k.CanDecrypt(FormatVault) || !k.CanDecrypt(FormatAge) {
		t.Error("CanDecrypt() = false for a keyring holding both kinds of key")
	}
}

// TestResolveKeyringFromEnv covers the fallbacks, including the one shape that is easy to get
// wrong: CARGOSHIP_AGE_RECIPIENTS holds several keys separated by whitespace.
func TestResolveKeyringFromEnv(t *testing.T) {
	clearKeyEnv(t)
	first, second := newAgeIdentity(t), newAgeIdentity(t)

	t.Setenv(AgeIdentityFileEnvVar, writeFile(t, "key.txt", first.String()+"\n"))
	t.Setenv(AgeRecipientsEnvVar, first.Recipient().String()+" "+second.Recipient().String())

	k, err := ResolveKeyring(KeyOptions{})
	if err != nil {
		t.Fatalf("ResolveKeyring() error = %v", err)
	}
	if len(k.identities) != 1 {
		t.Errorf("len(identities) = %d, want 1", len(k.identities))
	}
	if len(k.recipients) != 2 {
		t.Errorf("len(recipients) = %d, want 2", len(k.recipients))
	}

	// Recipients from the environment are not an explicit request for age, so an explicit vault
	// password file alongside them is not the conflict two flags would be.
	if k.ageExplicit {
		t.Error("ageExplicit = true for recipients that came from the environment")
	}
}

// TestResolveKeyringFlagsBeatEnv pins the precedence for the age flags to the one
// ResolveVaultPassword already had: a flag means the environment is not consulted at all.
func TestResolveKeyringFlagsBeatEnv(t *testing.T) {
	clearKeyEnv(t)
	flagged, fromEnv := newAgeIdentity(t), newAgeIdentity(t)

	t.Setenv(AgeIdentityFileEnvVar, writeFile(t, "env-key.txt", fromEnv.String()+"\n"))
	t.Setenv(AgeRecipientsEnvVar, fromEnv.Recipient().String())

	k, err := ResolveKeyring(KeyOptions{
		AgeIdentityFiles: []string{writeFile(t, "flag-key.txt", flagged.String()+"\n")},
		AgeRecipients:    []string{flagged.Recipient().String()},
	})
	if err != nil {
		t.Fatalf("ResolveKeyring() error = %v", err)
	}

	ciphertext, err := encryptAge("hunter2", k.recipients)
	if err != nil {
		t.Fatalf("encryptAge() error = %v", err)
	}
	if _, err := decryptAge(ciphertext, []age.Identity{flagged}); err != nil {
		t.Errorf("decryptAge(flagged) error = %v, want the flag's recipient to be the one used", err)
	}
	if _, err := decryptAge(ciphertext, []age.Identity{fromEnv}); err == nil {
		t.Error("decryptAge(fromEnv) error = nil, want the environment's recipient to have been ignored")
	}
}

func TestResolveKeyringRecipientsFile(t *testing.T) {
	clearKeyEnv(t)
	first, second := newAgeIdentity(t), newAgeIdentity(t)

	// Comments and blank lines are what an age recipients file looks like in practice: one key per
	// operator, each labelled.
	content := "# platform team\n" + first.Recipient().String() + "\n\n# ci\n" + second.Recipient().String() + "\n"

	k, err := ResolveKeyring(KeyOptions{AgeRecipientFiles: []string{writeFile(t, "recipients.txt", content)}})
	if err != nil {
		t.Fatalf("ResolveKeyring() error = %v", err)
	}
	if len(k.recipients) != 2 {
		t.Errorf("len(recipients) = %d, want 2", len(k.recipients))
	}
	if !k.ageExplicit {
		t.Error("ageExplicit = false for recipients named by a flag")
	}
}

func TestResolveKeyringErrors(t *testing.T) {
	clearKeyEnv(t)
	id := newAgeIdentity(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	tests := []struct {
		name    string
		options KeyOptions
		wantErr string
	}{
		{"missing identity file", KeyOptions{AgeIdentityFiles: []string{missing}}, "reading age identity file"},
		{"missing recipients file", KeyOptions{AgeRecipientFiles: []string{missing}}, "reading age recipients file"},
		{"missing vault password file", KeyOptions{VaultPasswordFile: missing}, "reading vault password file"},
		{
			"identity file holding a public key",
			KeyOptions{AgeIdentityFiles: []string{writeFile(t, "key.txt", id.Recipient().String()+"\n")}},
			"parsing age identity file",
		},
		// The error must not quote the key back, since it is private key material and parse errors
		// get printed and logged; TestParseRecipientDoesNotEchoPrivateKeys holds that in place.
		{
			"recipient that is a private key",
			KeyOptions{AgeRecipients: []string{id.String()}},
			"not a public key",
		},
		// A recipients file of nothing but comments is the one to refuse loudest: carrying on would
		// leave a keyring that quietly falls back to Ansible Vault.
		{
			"recipients file holding no keys",
			KeyOptions{AgeRecipientFiles: []string{writeFile(t, "recipients.txt", "# nobody yet\n")}},
			"holds no recipients",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveKeyring(tt.options)
			if err == nil {
				t.Fatalf("ResolveKeyring() error = nil, want one mentioning %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ResolveKeyring() error = %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestKeyringEmpty(t *testing.T) {
	clearKeyEnv(t)

	k, err := ResolveKeyring(KeyOptions{})
	if err != nil {
		t.Fatalf("ResolveKeyring() error = %v", err)
	}
	if !k.Empty() {
		t.Error("Empty() = false for a keyring resolved from nothing")
	}
	if (*Keyring)(nil).Empty() != true {
		t.Error("Empty() = false for a nil keyring")
	}
	if NewVaultKeyring("hunter2").Empty() {
		t.Error("Empty() = true for a keyring holding a vault password")
	}
}

func TestKeyringEncryptFormat(t *testing.T) {
	id := newAgeIdentity(t)
	recipients := []age.Recipient{id.Recipient()}

	tests := []struct {
		name    string
		keyring *Keyring
		want    Format
		wantErr string
	}{
		{"vault only", NewVaultKeyring("hunter2"), FormatVault, ""},
		{"age only", &Keyring{recipients: recipients, ageExplicit: true}, FormatAge, ""},
		// The common case on a machine already set up for vault: the password is in a shell
		// profile, and an operator passing --age-recipient means what they said.
		{
			"age flag beats an environment vault password",
			&Keyring{vaultPassword: "hunter2", recipients: recipients, ageExplicit: true},
			FormatAge, "",
		},
		{
			"both explicit",
			&Keyring{vaultPassword: "hunter2", vaultExplicit: true, recipients: recipients, ageExplicit: true},
			"", "pass one or the other",
		},
		{"nothing", &Keyring{}, "", ErrNoKeyMaterial.Error()},
		{"nil", nil, "", ErrNoKeyMaterial.Error()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.keyring.EncryptFormat()
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("EncryptFormat() error = %v", err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("EncryptFormat() error = nil, want one mentioning %q", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Fatalf("EncryptFormat() error = %q, want it to mention %q", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("EncryptFormat() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKeyringRekeyTarget(t *testing.T) {
	id := newAgeIdentity(t)
	aged := &Keyring{vaultPassword: testPassword, recipients: []age.Recipient{id.Recipient()}, ageExplicit: true}

	t.Run("new vault password rotates", func(t *testing.T) {
		to, rotated, err := NewVaultKeyring(testPassword).RekeyTarget(rekeyPassword)
		if err != nil {
			t.Fatalf("RekeyTarget() error = %v", err)
		}
		if !rotated {
			t.Error("rotated = false for a different new password")
		}
		if to.vaultPassword != rekeyPassword {
			t.Errorf("target password = %q, want %q", to.vaultPassword, rekeyPassword)
		}
	})

	t.Run("same vault password re-salts", func(t *testing.T) {
		to, rotated, err := NewVaultKeyring(testPassword).RekeyTarget(testPassword)
		if err != nil {
			t.Fatalf("RekeyTarget() error = %v", err)
		}
		if rotated {
			t.Error("rotated = true for the password the file already uses")
		}
		if to.vaultPassword != testPassword {
			t.Errorf("target password = %q, want %q", to.vaultPassword, testPassword)
		}
	})

	t.Run("no new password re-salts", func(t *testing.T) {
		to, rotated, err := NewVaultKeyring(testPassword).RekeyTarget("")
		if err != nil {
			t.Fatalf("RekeyTarget() error = %v", err)
		}
		if rotated {
			t.Error("rotated = true with no new key named")
		}
		if to.vaultPassword != testPassword {
			t.Errorf("target password = %q, want %q", to.vaultPassword, testPassword)
		}
	})

	// The migration case: the old password reads, the recipients write, and the target must carry
	// the recipients alone or EncryptFormat would see a keyring naming two formats.
	t.Run("age recipients rotate onto age", func(t *testing.T) {
		to, rotated, err := aged.RekeyTarget("")
		if err != nil {
			t.Fatalf("RekeyTarget() error = %v", err)
		}
		if !rotated {
			t.Error("rotated = false for a rekey onto age recipients")
		}
		if to.vaultPassword != "" {
			t.Errorf("target password = %q, want it empty", to.vaultPassword)
		}
		format, err := to.EncryptFormat()
		if err != nil {
			t.Fatalf("EncryptFormat() error = %v", err)
		}
		if format != FormatAge {
			t.Errorf("EncryptFormat() = %q, want %q", format, FormatAge)
		}
	})

	t.Run("both new keys is an error", func(t *testing.T) {
		_, _, err := aged.RekeyTarget(rekeyPassword)
		if err == nil || !strings.Contains(err.Error(), "pass one or the other") {
			t.Errorf("RekeyTarget() error = %v, want one mentioning \"pass one or the other\"", err)
		}
	})
}

func TestKeyringCanDecrypt(t *testing.T) {
	id := newAgeIdentity(t)

	vaultOnly := NewVaultKeyring("hunter2")
	if !vaultOnly.CanDecrypt(FormatVault) {
		t.Error("CanDecrypt(vault) = false for a keyring holding a password")
	}
	if vaultOnly.CanDecrypt(FormatAge) {
		t.Error("CanDecrypt(age) = true for a keyring holding only a password")
	}

	// Recipients are public keys, so they encrypt and never decrypt.
	ageOnly := &Keyring{recipients: []age.Recipient{id.Recipient()}}
	if ageOnly.CanDecrypt(FormatAge) {
		t.Error("CanDecrypt(age) = true for a keyring holding only recipients")
	}

	withIdentity := &Keyring{identities: []age.Identity{id}}
	if !withIdentity.CanDecrypt(FormatAge) {
		t.Error("CanDecrypt(age) = false for a keyring holding an identity")
	}
	if (*Keyring)(nil).CanDecrypt(FormatVault) {
		t.Error("CanDecrypt() = true for a nil keyring")
	}
}

func TestEncryptValueErrorsWithoutKeyMaterial(t *testing.T) {
	if _, err := EncryptValue("hunter2", &Keyring{}); !errors.Is(err, ErrNoKeyMaterial) {
		t.Errorf("EncryptValue() error = %v, want ErrNoKeyMaterial", err)
	}
}
