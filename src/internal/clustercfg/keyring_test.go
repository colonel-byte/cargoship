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
	"slices"
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

// TestKeyringRecipientStringsRoundTrip covers the one thing the record needs that the keyring did
// not already have: the text of each recipient.
//
// A parsed age.Recipient cannot be turned back into a string -- the agessh types carry no text
// encoding -- so if the text is not kept as it is read, it is gone, and the document has nothing to
// record. Every source a recipient can arrive from therefore has to keep it.
func TestKeyringRecipientStringsRoundTrip(t *testing.T) {
	first, second := newAgeIdentity(t), newAgeIdentity(t)
	ssh := newEd25519SSHKey(t)
	commented := ssh.authorized + " alice@laptop"

	tests := []struct {
		name string
		// options and env are the two ways recipients reach a keyring.
		options KeyOptions
		env     string
		// want is what the document records; wantParsed is how many keys the values are actually
		// encrypted to. They differ only where the same key was named twice.
		want       []string
		wantParsed int
	}{
		{
			name:       "flags in the order given",
			options:    KeyOptions{AgeRecipients: []string{second.Recipient().String(), first.Recipient().String()}},
			want:       []string{second.Recipient().String(), first.Recipient().String()},
			wantParsed: 2,
		},
		{
			// The comment is the part that says whose key it is, so it is the part most worth
			// recording. Nothing downstream parses it back out.
			name:       "an SSH key keeps its comment",
			options:    KeyOptions{AgeRecipients: []string{commented}},
			want:       []string{commented},
			wantParsed: 1,
		},
		{
			name: "a recipients file, minus its comments and blank lines",
			options: KeyOptions{AgeRecipientFiles: []string{writeFile(t, "recipients.txt",
				"# platform team\n"+first.Recipient().String()+"\n\n# ci\n"+commented+"\n")}},
			want:       []string{first.Recipient().String(), commented},
			wantParsed: 2,
		},
		{
			// Naming one key on a flag and again in a file encrypts to it twice, which is harmless
			// and is not this change's to fix. The record is a list of who can read the file, and
			// saying one of them twice would read as two operators.
			name: "a flag and a file, with the duplicate recorded once",
			options: KeyOptions{
				AgeRecipients:     []string{first.Recipient().String()},
				AgeRecipientFiles: []string{writeFile(t, "dupe.txt", first.Recipient().String()+"\n"+second.Recipient().String()+"\n")},
			},
			want:       []string{first.Recipient().String(), second.Recipient().String()},
			wantParsed: 3,
		},
		{
			name:       "the environment, when no flag named any",
			env:        first.Recipient().String() + " " + second.Recipient().String(),
			want:       []string{first.Recipient().String(), second.Recipient().String()},
			wantParsed: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearKeyEnv(t)
			if tt.env != "" {
				t.Setenv(AgeRecipientsEnvVar, tt.env)
			}

			k, err := ResolveKeyring(tt.options)
			if err != nil {
				t.Fatalf("ResolveKeyring() error = %v", err)
			}
			if got := k.RecipientStrings(); !slices.Equal(got, tt.want) {
				t.Errorf("RecipientStrings() = %q, want %q", got, tt.want)
			}
			if len(k.recipients) != tt.wantParsed {
				t.Errorf("len(recipients) = %d, want %d", len(k.recipients), tt.wantParsed)
			}
		})
	}
}

// TestResolveKeyringRejectsAWhitespacePaddedRecipient holds the reason the recorded text is trimmed
// and the parsed text is not.
//
// parseRecipient dispatches on the "age1" and private-key prefixes by matching the raw string, so
// trimming before it would change which inputs are accepted -- a padded key is refused today, and
// silently accepting one to make the record tidier is not a trade worth making.
func TestResolveKeyringRejectsAWhitespacePaddedRecipient(t *testing.T) {
	clearKeyEnv(t)
	id := newAgeIdentity(t)

	_, err := ResolveKeyring(KeyOptions{AgeRecipients: []string{id.Recipient().String() + "  "}})
	if err == nil {
		t.Fatal("ResolveKeyring() error = nil, want the padded key refused rather than trimmed")
	}
	if !strings.Contains(err.Error(), "parsing age recipient") {
		t.Errorf("ResolveKeyring() error = %q, want it to name the recipient it could not parse", err)
	}
}

// TestKeyringRecipientStringsAreACopy keeps the accessor from handing out the keyring's own slice.
// What it returns is written into a document and compared against one, and neither of those should
// be able to reach back into the key material's provenance.
func TestKeyringRecipientStringsAreACopy(t *testing.T) {
	clearKeyEnv(t)
	id := newAgeIdentity(t)

	k, err := ResolveKeyring(KeyOptions{AgeRecipients: []string{id.Recipient().String()}})
	if err != nil {
		t.Fatalf("ResolveKeyring() error = %v", err)
	}

	got := k.RecipientStrings()
	got[0] = "age1tampered"
	if again := k.RecipientStrings(); again[0] != id.Recipient().String() {
		t.Errorf("RecipientStrings() = %q after the caller wrote to an earlier result", again[0])
	}

	// A keyring with no recipients has nothing to record, and nil rather than an empty slice is
	// what says so.
	if strings := (&Keyring{}).RecipientStrings(); strings != nil {
		t.Errorf("RecipientStrings() = %q on an empty keyring, want nil", strings)
	}
}

// TestRekeyTargetCarriesRecipientStrings holds the seam rekey writes its record through. The target
// keyring is built fresh from the recipients, so anything not copied onto it is lost -- and a rekey
// that wrote no record would leave the file claiming the keys it used to be encrypted to.
func TestRekeyTargetCarriesRecipientStrings(t *testing.T) {
	clearKeyEnv(t)
	id := newAgeIdentity(t)

	from, err := ResolveKeyring(KeyOptions{AgeRecipients: []string{id.Recipient().String()}})
	if err != nil {
		t.Fatalf("ResolveKeyring() error = %v", err)
	}

	to, _, err := from.RekeyTarget("")
	if err != nil {
		t.Fatalf("RekeyTarget() error = %v", err)
	}
	if got := to.RecipientStrings(); !slices.Equal(got, from.RecipientStrings()) {
		t.Errorf("RecipientStrings() = %q on the target, want %q", got, from.RecipientStrings())
	}
}
