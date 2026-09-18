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
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	goyaml "github.com/goccy/go-yaml"
)

// newAgeKeyring returns a keyring that both encrypts to and decrypts with one fresh key pair, which
// is what an operator working on their own machine has.
//
// The recipient's text is carried alongside it, as ResolveKeyring carries it, so that a test
// encrypting with this keyring produces the recipient record a real run would.
func newAgeKeyring(t *testing.T) *Keyring {
	t.Helper()
	id := newAgeIdentity(t)
	return &Keyring{
		identities:       []age.Identity{id},
		recipients:       []age.Recipient{id.Recipient()},
		recipientStrings: []string{id.Recipient().String()},
		ageExplicit:      true,
	}
}

func TestEncryptAtPathAgeRoundTrip(t *testing.T) {
	k := newAgeKeyring(t)

	tests := []struct {
		name string
		path string
		want string
	}{
		{"plain scalar", ".spec.config.registries[0].auth.pass", "hunter2"},
		{"quoted scalar", ".spec.config.registries[0].auth.token", "tok #1"},
		{"literal block", ".spec.config.registries[0].tls.ca", "-----BEGIN CERTIFICATE-----\naGVsbG8gd29ybGQ=\n-----END CERTIFICATE-----\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncryptAtPath(readPathsInventoryFixture(t), tt.path, k, false)
			if err != nil {
				t.Fatalf("EncryptAtPath() error = %v", err)
			}

			ciphertext := readPath(t, got, tt.path)
			if !cluster.IsAgeEncrypted(ciphertext) {
				t.Fatalf("value at %s = %q, want age ciphertext", tt.path, ciphertext)
			}

			plain, err := DecryptValue(ciphertext, k)
			if err != nil {
				t.Fatalf("DecryptValue() error = %v", err)
			}
			if plain != tt.want {
				t.Errorf("decrypted value = %q, want %q", plain, tt.want)
			}
		})
	}
}

// TestDecryptAtPathAgeRestoresTheDocument checks that an age value survives the splice both ways:
// armored ciphertext is many lines, and putting it back has to restore the original scalar style.
func TestDecryptAtPathAgeRestoresTheDocument(t *testing.T) {
	k := newAgeKeyring(t)
	const path = ".spec.config.registries[0].auth.pass"

	encrypted, err := EncryptAtPath(readPathsInventoryFixture(t), path, k, false)
	if err != nil {
		t.Fatalf("EncryptAtPath() error = %v", err)
	}
	decrypted, err := DecryptAtPath(encrypted, path, k)
	if err != nil {
		t.Fatalf("DecryptAtPath() error = %v", err)
	}
	if value := readPath(t, decrypted, path); value != "hunter2" {
		t.Errorf("value at %s = %q, want %q", path, value, "hunter2")
	}
	if !strings.Contains(string(decrypted), "# not the password you think") {
		t.Errorf("trailing comment did not survive:\n%s", decrypted)
	}
}

func TestEncryptConfigWritesAge(t *testing.T) {
	k := newAgeKeyring(t)
	doc := readVaultInventoryFixture(t)

	got, changed, _, err := EncryptConfig(doc, k, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	want := []string{
		"$.spec.config.registries[0].auth.user",
		"$.spec.config.registries[0].auth.pass",
		"$.spec.config.registries[0].tls.ca",
		"$.spec.config.registries[1].auth.token",
	}
	if strings.Join(changed, ",") != strings.Join(want, ",") {
		t.Fatalf("changed = %v, want %v", changed, want)
	}
	for _, path := range want {
		if value := readPath(t, got, path); !cluster.IsAgeEncrypted(value) {
			t.Errorf("value at %s = %q, want age ciphertext", path, value)
		}
	}

	// What encrypt-file writes has to be what an apply reads back, for age as for vault.
	var dis cluster.ZarfCluster
	if err := goyaml.Unmarshal(got, &dis); err != nil {
		t.Fatalf("unmarshalling:\n%s\nerror = %v", got, err)
	}
	if err := DecryptRegistryAuth(&dis, k); err != nil {
		t.Fatalf("DecryptRegistryAuth() error = %v", err)
	}
	if dis.Spec.Config.Registries[0].Authentication.Password != "hunter2" {
		t.Errorf("auth.pass = %q, want %q", dis.Spec.Config.Registries[0].Authentication.Password, "hunter2")
	}
	if dis.Spec.Config.Registries[1].Authentication.Token != "tok #2" {
		t.Errorf("auth.token = %q, want %q", dis.Spec.Config.Registries[1].Authentication.Token, "tok #2")
	}
}

// TestEncryptConfigAgeIsIdempotent covers the skip that has no probe behind it. A public key cannot
// decrypt and the age header names no recipient, so the second pass can only tell that the value is
// already age ciphertext -- not that it is encrypted to these recipients. Leaving it alone is the
// right answer for the common case and the reason PR 2 reports the skip.
func TestEncryptConfigAgeIsIdempotent(t *testing.T) {
	k := newAgeKeyring(t)
	doc := readVaultInventoryFixture(t)

	once, _, _, err := EncryptConfig(doc, k, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	twice, changed, _, err := EncryptConfig(once, k, false)
	if err != nil {
		t.Fatalf("EncryptConfig() second pass error = %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("changed = %v, want nothing on the second pass", changed)
	}
	if string(twice) != string(once) {
		t.Errorf("second pass rewrote the document:\n%s", twice)
	}
}

// TestEncryptConfigSkipsVaultValuesWhenWritingAge is the other half of that: a vaulted value is
// skipped rather than refused, because the probe that would refuse it is a vault-to-vault check and
// there is nothing here to compare. The operator gets a no-op today and a warning after PR 2; rekey
// is what actually moves the file.
func TestEncryptConfigSkipsVaultValuesWhenWritingAge(t *testing.T) {
	doc := readVaultInventoryFixture(t)
	vaulted, _, _, err := EncryptConfig(doc, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	got, changed, _, err := EncryptConfig(vaulted, newAgeKeyring(t), false)
	if err != nil {
		t.Fatalf("EncryptConfig(age over vault) error = %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("changed = %v, want nothing", changed)
	}
	if string(got) != string(vaulted) {
		t.Errorf("document was rewritten:\n%s", got)
	}
}

// TestDecryptRegistryAuthMixedFormats is the claim the whole design rests on: because each value
// says which format it is in, one document can hold both, and a keyring carrying both keys reads
// all of it. That is what makes a migration a sequence of ordinary edits.
func TestDecryptRegistryAuthMixedFormats(t *testing.T) {
	ageKeyring := newAgeKeyring(t)
	rawDoc := readVaultInventoryFixture(t)

	// The first registry's credentials are vaulted; the second's token is age-encrypted.
	doc, _, _, err := EncryptConfig(rawDoc, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	doc, err = DecryptAtPath(doc, "$.spec.config.registries[1].auth.token", testKeyring)
	if err != nil {
		t.Fatalf("DecryptAtPath() error = %v", err)
	}
	doc, err = EncryptAtPath(doc, "$.spec.config.registries[1].auth.token", ageKeyring, false)
	if err != nil {
		t.Fatalf("EncryptAtPath() error = %v", err)
	}

	if value := readPath(t, doc, "$.spec.config.registries[0].auth.pass"); !cluster.IsVaultEncrypted(value) {
		t.Fatalf("auth.pass = %q, want Ansible Vault ciphertext", value)
	}
	if value := readPath(t, doc, "$.spec.config.registries[1].auth.token"); !cluster.IsAgeEncrypted(value) {
		t.Fatalf("auth.token = %q, want age ciphertext", value)
	}

	both := &Keyring{
		vaultPassword: testPassword,
		identities:    ageKeyring.identities,
	}

	var dis cluster.ZarfCluster
	if err := goyaml.Unmarshal(doc, &dis); err != nil {
		t.Fatalf("unmarshalling:\n%s\nerror = %v", doc, err)
	}
	if err := VerifyRegistryAuth(&dis, both); err != nil {
		t.Fatalf("VerifyRegistryAuth() error = %v", err)
	}
	if err := DecryptRegistryAuth(&dis, both); err != nil {
		t.Fatalf("DecryptRegistryAuth() error = %v", err)
	}
	if dis.Spec.Config.Registries[0].Authentication.Password != "hunter2" {
		t.Errorf("auth.pass = %q, want %q", dis.Spec.Config.Registries[0].Authentication.Password, "hunter2")
	}
	if dis.Spec.Config.Registries[1].Authentication.Token != "tok #2" {
		t.Errorf("auth.token = %q, want %q", dis.Spec.Config.Registries[1].Authentication.Token, "tok #2")
	}
}

// TestDecryptRegistryAuthMissingAgeIdentity checks that the error names the key the value's own
// format needs. Being told to pass a vault password for an age value is worse than being told
// nothing, and a mixed document makes that easy to get wrong.
func TestDecryptRegistryAuthMissingAgeIdentity(t *testing.T) {
	rawDoc := readVaultInventoryFixture(t)
	doc, _, _, err := EncryptConfig(rawDoc, newAgeKeyring(t), false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	var dis cluster.ZarfCluster
	if err := goyaml.Unmarshal(doc, &dis); err != nil {
		t.Fatalf("unmarshalling:\n%s\nerror = %v", doc, err)
	}

	err = DecryptRegistryAuth(&dis, NewVaultKeyring(testPassword))
	if err == nil {
		t.Fatal("DecryptRegistryAuth() error = nil, want one naming the missing age identity")
	}
	for _, want := range []string{"harbor", "auth.user", "age-encrypted", "--age-identity-file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("DecryptRegistryAuth() error = %q, want it to mention %q", err, want)
		}
	}
}

// TestDecryptRegistryAuthWrongAgeIdentity covers the failure age adds that vault never had: a
// document encrypted to a recipient nobody on this machine holds. Nothing in the ciphertext says
// whose key it is, so the pre-flight is the only thing that catches it.
func TestDecryptRegistryAuthWrongAgeIdentity(t *testing.T) {
	rawDoc := readVaultInventoryFixture(t)
	doc, _, _, err := EncryptConfig(rawDoc, newAgeKeyring(t), false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	var dis cluster.ZarfCluster
	if err := goyaml.Unmarshal(doc, &dis); err != nil {
		t.Fatalf("unmarshalling:\n%s\nerror = %v", doc, err)
	}

	err = VerifyRegistryAuth(&dis, newAgeKeyring(t))
	if err == nil {
		t.Fatal("VerifyRegistryAuth() error = nil, want one for an unmatched identity")
	}
	if !strings.Contains(err.Error(), "none of the configured age identities") {
		t.Errorf("VerifyRegistryAuth() error = %q, want it to mention the unmatched identities", err)
	}
}

// TestRekeyConfigMigratesVaultToAge is the migration path: read with the old password, write to the
// recipients, in one pass that never puts the plaintext on disk.
func TestRekeyConfigMigratesVaultToAge(t *testing.T) {
	ageKeyring := newAgeKeyring(t)
	rawDoc := readVaultInventoryFixture(t)

	vaulted, _, _, err := EncryptConfig(rawDoc, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	// The shape a command builds: one keyring reads and names the target, RekeyTarget splits it.
	from := &Keyring{
		vaultPassword: testPassword,
		vaultExplicit: true,
		recipients:    ageKeyring.recipients,
		ageExplicit:   true,
	}
	to, rotated, err := from.RekeyTarget("")
	if err != nil {
		t.Fatalf("RekeyTarget() error = %v", err)
	}
	if !rotated {
		t.Error("rotated = false for a migration onto age recipients")
	}

	got, changed, _, err := RekeyConfig(vaulted, from, to)
	if err != nil {
		t.Fatalf("RekeyConfig() error = %v", err)
	}
	want := []string{
		"$.spec.config.registries[0].auth.user",
		"$.spec.config.registries[0].auth.pass",
		"$.spec.config.registries[0].tls.ca",
		"$.spec.config.registries[1].auth.token",
	}
	if strings.Join(changed, ",") != strings.Join(want, ",") {
		t.Fatalf("changed = %v, want %v", changed, want)
	}
	for _, path := range want {
		if value := readPath(t, got, path); !cluster.IsAgeEncrypted(value) {
			t.Errorf("value at %s = %q, want age ciphertext", path, value)
		}
	}

	var dis cluster.ZarfCluster
	if err := goyaml.Unmarshal(got, &dis); err != nil {
		t.Fatalf("unmarshalling:\n%s\nerror = %v", got, err)
	}
	if err := DecryptRegistryAuth(&dis, ageKeyring); err != nil {
		t.Fatalf("DecryptRegistryAuth() error = %v", err)
	}
	if dis.Spec.Config.Registries[0].Authentication.Password != "hunter2" {
		t.Errorf("auth.pass = %q, want %q", dis.Spec.Config.Registries[0].Authentication.Password, "hunter2")
	}
}

// TestRekeyConfigResaltsAge pins the property rekey has for vault to age as well: encrypting the
// same plaintext twice produces different bytes, so a re-salt with no new key is not a no-op.
func TestRekeyConfigResaltsAge(t *testing.T) {
	k := newAgeKeyring(t)
	doc := readVaultInventoryFixture(t)

	once, _, _, err := EncryptConfig(doc, k, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	to, rotated, err := k.RekeyTarget("")
	if err != nil {
		t.Fatalf("RekeyTarget() error = %v", err)
	}
	if !rotated {
		t.Error("rotated = false; recipients always name a target to write to")
	}

	twice, changed, _, err := RekeyConfig(once, k, to)
	if err != nil {
		t.Fatalf("RekeyConfig() error = %v", err)
	}
	if len(changed) != 4 {
		t.Errorf("changed = %v, want all four credentials", changed)
	}

	const path = "$.spec.config.registries[0].auth.pass"
	if readPath(t, twice, path) == readPath(t, once, path) {
		t.Error("ciphertext is unchanged, want fresh randomness")
	}
	plain, err := DecryptValue(readPath(t, twice, path), k)
	if err != nil {
		t.Fatalf("DecryptValue() error = %v", err)
	}
	if plain != "hunter2" {
		t.Errorf("decrypted value = %q, want %q", plain, "hunter2")
	}
}

// TestRekeyConfigRejectsAnAgeValueItCannotRead covers the message an operator sees when a file was
// encrypted to someone else's key. It cannot say whose, so it says what to check.
func TestRekeyConfigRejectsAnAgeValueItCannotRead(t *testing.T) {
	rawDoc := readVaultInventoryFixture(t)
	doc, _, _, err := EncryptConfig(rawDoc, newAgeKeyring(t), false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	from := newAgeKeyring(t)
	to, _, err := from.RekeyTarget("")
	if err != nil {
		t.Fatalf("RekeyTarget() error = %v", err)
	}

	_, _, _, err = RekeyConfig(doc, from, to)
	if err == nil {
		t.Fatal("RekeyConfig() error = nil, want one for a value it cannot read")
	}
	for _, want := range []string{"age identities", "recipient you do not hold a key for"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("RekeyConfig() error = %q, want it to mention %q", err, want)
		}
	}
}

// TestEncryptValueRefusesTwoExplicitFormats is the one case worth stopping for: a value is written
// in a single format, and choosing on the operator's behalf means writing a secret under a key they
// did not pick.
func TestEncryptValueRefusesTwoExplicitFormats(t *testing.T) {
	doc := readVaultInventoryFixture(t)
	k := newAgeKeyring(t)
	k.vaultPassword = testPassword
	k.vaultExplicit = true

	if _, _, _, err := EncryptConfig(doc, k, false); err == nil {
		t.Fatal("EncryptConfig() error = nil, want a refusal naming both formats")
	} else if !strings.Contains(err.Error(), "pass one or the other") {
		t.Errorf("EncryptConfig() error = %q, want it to mention \"pass one or the other\"", err)
	}
}
