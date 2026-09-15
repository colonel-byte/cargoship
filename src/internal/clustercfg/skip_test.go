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
)

// skipPaths renders skips as their paths, so that a test comparing what was left alone does not
// have to restate the reasons.
func skipPaths(skips []Skip) []string {
	paths := make([]string, len(skips))
	for i, skip := range skips {
		paths[i] = skip.Path
	}
	return paths
}

// Encrypting a plaintext configuration reports no skips. The absent and empty fields the document
// holds are skipped, and that is exactly the kind of skip nobody wants to hear about.
func TestEncryptConfigReportsNoSkipsForPlaintext(t *testing.T) {
	_, changed, skipped, err := EncryptConfig([]byte(configTestDoc), NewVaultKeyring("testpass"), false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	if len(changed) != 4 {
		t.Errorf("changed = %v, want all four credentials", changed)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none: an absent or empty field is not worth reporting", skipPaths(skipped))
	}
}

// The probe answers the question encrypt-file exists to ask, so a value already vaulted under the
// password being encrypted with is a silent skip rather than a warning on every second run.
func TestEncryptConfigVaultUnderTheSamePasswordIsSilent(t *testing.T) {
	k := NewVaultKeyring("testpass")

	once, _, _, err := EncryptConfig([]byte(configTestDoc), k, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	_, changed, skipped, err := EncryptConfig(once, k, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("changed = %v, want nothing on the second run", changed)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none: the probe proved the values are done", skipPaths(skipped))
	}
}

// The case the reporting exists for. Encrypting an age-encrypted file to age recipients does
// nothing, and cannot tell whether that was right, so every credential comes back as a skip.
func TestEncryptConfigAgeToAgeReportsEverySkip(t *testing.T) {
	k := newAgeKeyring(t)

	once, _, _, err := EncryptConfig([]byte(configTestDoc), k, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	_, changed, skipped, err := EncryptConfig(once, k, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("changed = %v, want nothing on the second run", changed)
	}
	if len(skipped) != 4 {
		t.Fatalf("skipped = %v, want all four credentials", skipPaths(skipped))
	}
	for _, skip := range skipped {
		if !strings.Contains(skip.Reason, "cannot tell whether they are the ones you named") {
			t.Errorf("reason for %s = %q, want it to say the recipients cannot be checked", skip.Path, skip.Reason)
		}
		if !strings.Contains(skip.Reason, "rekey") {
			t.Errorf("reason for %s = %q, want it to name rekey", skip.Path, skip.Reason)
		}
	}
}

// The migration case: a vaulted file, age recipients. encrypt-file does not move a credential
// between formats, so it says what does.
func TestEncryptConfigVaultValuesWithAgeRecipientsReportSkips(t *testing.T) {
	vaulted, _, _, err := EncryptConfig([]byte(configTestDoc), NewVaultKeyring("testpass"), false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	_, changed, skipped, err := EncryptConfig(vaulted, newAgeKeyring(t), false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("changed = %v, want nothing: encrypt-file does not migrate between formats", changed)
	}
	if len(skipped) != 4 {
		t.Fatalf("skipped = %v, want all four credentials", skipPaths(skipped))
	}
	for _, skip := range skipped {
		if !strings.Contains(skip.Reason, "Ansible Vault-encrypted") {
			t.Errorf("reason for %s = %q, want it to name the format the value is in", skip.Path, skip.Reason)
		}
		if !strings.Contains(skip.Reason, "rekey") {
			t.Errorf("reason for %s = %q, want it to name rekey", skip.Path, skip.Reason)
		}
	}
}

// The mirror image, which an operator moving back to a shared password walks into.
func TestEncryptConfigAgeValuesWithAVaultPasswordReportSkips(t *testing.T) {
	aged, _, _, err := EncryptConfig([]byte(configTestDoc), newAgeKeyring(t), false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	_, changed, skipped, err := EncryptConfig(aged, NewVaultKeyring("testpass"), false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("changed = %v, want nothing: encrypt-file does not migrate between formats", changed)
	}
	if len(skipped) != 4 {
		t.Fatalf("skipped = %v, want all four credentials", skipPaths(skipped))
	}
	for _, skip := range skipped {
		if !strings.Contains(skip.Reason, "age-encrypted") {
			t.Errorf("reason for %s = %q, want it to name the format the value is in", skip.Path, skip.Reason)
		}
	}
}

// --force is the operator saying they know, so there is nothing left to tell them.
func TestEncryptConfigForceReportsNoSkips(t *testing.T) {
	k := newAgeKeyring(t)

	once, _, _, err := EncryptConfig([]byte(configTestDoc), k, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	_, changed, skipped, err := EncryptConfig(once, k, true)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	if len(changed) != 4 {
		t.Errorf("changed = %v, want all four credentials wrapped again", changed)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none when every value was rewritten", skipPaths(skipped))
	}
}

// A document holding one credential in each format, encrypted with an age keyring: the vaulted
// values and the age ones are each skipped for their own reason, and the reasons differ.
func TestEncryptConfigMixedFormatsReportsBothReasons(t *testing.T) {
	ageKeyring := newAgeKeyring(t)

	const path = "$.spec.config.registries[0].auth.pass"
	mixed, _, _, err := EncryptConfig([]byte(configTestDoc), NewVaultKeyring("testpass"), false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	mixed, err = RekeyAtPath(mixed, path, NewVaultKeyring("testpass"), ageKeyring)
	if err != nil {
		t.Fatalf("RekeyAtPath() error = %v", err)
	}

	_, _, skipped, err := EncryptConfig(mixed, ageKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	if len(skipped) != 4 {
		t.Fatalf("skipped = %v, want all four credentials", skipPaths(skipped))
	}
	for _, skip := range skipped {
		wantAge := skip.Path == path
		gotAge := strings.Contains(skip.Reason, "cannot tell whether they are the ones you named")
		if gotAge != wantAge {
			t.Errorf("reason for %s = %q, want the reason for the format it is in", skip.Path, skip.Reason)
		}
	}
}

// Decrypting and rekeying skip only values there was nothing to do about, so neither reports one.
func TestDecryptAndRekeyConfigReportNoSkips(t *testing.T) {
	k := NewVaultKeyring("testpass")

	// One credential vaulted, the rest left as plaintext, so both calls have something to skip.
	const path = "$.spec.config.registries[0].auth.pass"
	partial, err := EncryptAtPath([]byte(configTestDoc), path, k, false)
	if err != nil {
		t.Fatalf("EncryptAtPath() error = %v", err)
	}

	_, changed, skipped, err := DecryptConfig(partial, k)
	if err != nil {
		t.Fatalf("DecryptConfig() error = %v", err)
	}
	if len(changed) != 1 || len(skipped) != 0 {
		t.Errorf("DecryptConfig() changed = %v, skipped = %v; want one change and no skips worth reporting", changed, skipPaths(skipped))
	}

	to, _, err := k.RekeyTarget("newpass")
	if err != nil {
		t.Fatalf("RekeyTarget() error = %v", err)
	}
	_, changed, skipped, err = RekeyConfig(partial, k, to)
	if err != nil {
		t.Fatalf("RekeyConfig() error = %v", err)
	}
	if len(changed) != 1 || len(skipped) != 0 {
		t.Errorf("RekeyConfig() changed = %v, skipped = %v; want one change and no skips worth reporting", changed, skipPaths(skipped))
	}
}
