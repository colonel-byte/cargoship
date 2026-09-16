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
	"strings"
	"testing"

	"filippo.io/age"
	"filippo.io/age/armor"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
)

// newAgeIdentity returns a fresh key pair for a test that needs one.
func newAgeIdentity(t *testing.T) *age.X25519Identity {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("age.GenerateX25519Identity() error = %v", err)
	}
	return id
}

// TestAgeHeaderMatchesArmorHeader pins the constant the API package detects age ciphertext with to
// the one the armor writer actually emits. The two are spelled out separately so that the API
// package stays free of crypto dependencies, and this is what keeps them from drifting apart.
func TestAgeHeaderMatchesArmorHeader(t *testing.T) {
	if cluster.AgeHeader != armor.Header {
		t.Errorf("cluster.AgeHeader = %q, want %q", cluster.AgeHeader, armor.Header)
	}
}

func TestEncryptAgeRoundTrip(t *testing.T) {
	id := newAgeIdentity(t)

	tests := []struct {
		name  string
		value string
	}{
		{"short", "hunter2"},
		{"empty", ""},
		{"multi-line", "-----BEGIN CERTIFICATE-----\naGVsbG8gd29ybGQ=\n-----END CERTIFICATE-----\n"},
		{"unicode", "pässwörd ✓"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ciphertext, err := encryptAge(tt.value, []age.Recipient{id.Recipient()})
			if err != nil {
				t.Fatalf("encryptAge() error = %v", err)
			}
			if !cluster.IsAgeEncrypted(ciphertext) {
				t.Fatalf("encryptAge() = %q, want armored age ciphertext", ciphertext)
			}

			plain, err := decryptAge(ciphertext, []age.Identity{id})
			if err != nil {
				t.Fatalf("decryptAge() error = %v", err)
			}
			if plain != tt.value {
				t.Errorf("decryptAge() = %q, want %q", plain, tt.value)
			}
		})
	}
}

// TestEncryptAgeArmorIsTerminated is the guard against closing only the outer writer: a value
// missing its footer still starts with the header, so nothing but decryption catches it.
func TestEncryptAgeArmorIsTerminated(t *testing.T) {
	id := newAgeIdentity(t)

	ciphertext, err := encryptAge("hunter2", []age.Recipient{id.Recipient()})
	if err != nil {
		t.Fatalf("encryptAge() error = %v", err)
	}
	if !strings.Contains(ciphertext, "-----END AGE ENCRYPTED FILE-----") {
		t.Errorf("encryptAge() = %q, want it to end with the armor footer", ciphertext)
	}
}

// TestEncryptAgeMultipleRecipients covers the key model age is wanted for: every recipient can
// read the value on their own, without any shared secret between them.
func TestEncryptAgeMultipleRecipients(t *testing.T) {
	first, second := newAgeIdentity(t), newAgeIdentity(t)

	ciphertext, err := encryptAge("hunter2", []age.Recipient{first.Recipient(), second.Recipient()})
	if err != nil {
		t.Fatalf("encryptAge() error = %v", err)
	}

	for name, id := range map[string]*age.X25519Identity{"first": first, "second": second} {
		t.Run(name, func(t *testing.T) {
			plain, err := decryptAge(ciphertext, []age.Identity{id})
			if err != nil {
				t.Fatalf("decryptAge() error = %v", err)
			}
			if plain != "hunter2" {
				t.Errorf("decryptAge() = %q, want %q", plain, "hunter2")
			}
		})
	}
}

// TestDecryptAgeWrongIdentity checks the error an operator sees most often, and the limit on how
// specific it can be: the ciphertext records the recipient type but never the recipient.
func TestDecryptAgeWrongIdentity(t *testing.T) {
	recipient, other := newAgeIdentity(t), newAgeIdentity(t)

	ciphertext, err := encryptAge("hunter2", []age.Recipient{recipient.Recipient()})
	if err != nil {
		t.Fatalf("encryptAge() error = %v", err)
	}

	_, err = decryptAge(ciphertext, []age.Identity{other})
	if err == nil {
		t.Fatal("decryptAge() error = nil, want a no-identity-match error")
	}
	for _, want := range []string{"none of the configured age identities", "X25519"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("decryptAge() error = %q, want it to mention %q", err, want)
		}
	}
}

func TestAgeMissingKeyMaterial(t *testing.T) {
	id := newAgeIdentity(t)

	if _, err := encryptAge("hunter2", nil); !errors.Is(err, ErrNoAgeRecipients) {
		t.Errorf("encryptAge() error = %v, want ErrNoAgeRecipients", err)
	}

	ciphertext, err := encryptAge("hunter2", []age.Recipient{id.Recipient()})
	if err != nil {
		t.Fatalf("encryptAge() error = %v", err)
	}
	if _, err := decryptAge(ciphertext, nil); !errors.Is(err, ErrNoAgeIdentities) {
		t.Errorf("decryptAge() error = %v, want ErrNoAgeIdentities", err)
	}
}

func TestDecryptAgeRejectsGarbage(t *testing.T) {
	id := newAgeIdentity(t)

	ciphertext, err := encryptAge("hunter2", []age.Recipient{id.Recipient()})
	if err != nil {
		t.Fatalf("encryptAge() error = %v", err)
	}

	// Truncated at the last body line, keeping the header, which is what a document edited by
	// hand or wrapped by a YAML writer that does not know it is ciphertext looks like.
	lines := strings.Split(strings.TrimRight(ciphertext, "\n"), "\n")
	truncated := strings.Join(lines[:len(lines)-2], "\n") + "\n"

	if _, err := decryptAge(truncated, []age.Identity{id}); err == nil {
		t.Error("decryptAge(truncated) error = nil, want an error")
	}
}
