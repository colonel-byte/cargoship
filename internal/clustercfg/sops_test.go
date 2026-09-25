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
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"filippo.io/age"
	goyaml "github.com/goccy/go-yaml"
)

func TestIsSopsEncrypted(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"sops document", "sops:\n  version: 3.13.3\nfoo: bar\n", true},
		{"plain cluster config", "apiVersion: zarf.dev/v1alpha1\nkind: ZarfCluster\n", false},
		{"empty", "", false},
		{"malformed yaml", "not: [valid: yaml", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSopsEncrypted([]byte(tt.in)); got != tt.want {
				t.Errorf("IsSopsEncrypted(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// sopsBinary skips the calling test when the sops CLI isn't on PATH. DecryptSops only ever calls
// the sops decrypt library, but that library has no encrypt counterpart -- the sops binary is the
// only way to produce the fixture these tests decrypt, exactly as it would be for an operator.
func sopsBinary(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("sops")
	if err != nil {
		t.Skip("sops binary not on PATH, skipping sops fixture test")
	}
	return path
}

// encryptFixture writes cleartext to a sops-encrypted file under dir, using the sops CLI and an
// age recipient, and returns the ciphertext bytes.
func encryptFixture(t *testing.T, sopsPath, dir, cleartext string, recipient *age.X25519Recipient) []byte {
	t.Helper()

	in := filepath.Join(dir, "in.yaml")
	if err := os.WriteFile(in, []byte(cleartext), 0o600); err != nil {
		t.Fatalf("writing cleartext fixture: %v", err)
	}

	out := filepath.Join(dir, "out.yaml")
	cmd := exec.Command(sopsPath, "--encrypt", "--age", recipient.String(), "--input-type", "yaml", "--output-type", "yaml", in)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("sops --encrypt: %v: %s", err, stderr.String())
	}
	if err := os.WriteFile(out, stdout.Bytes(), 0o600); err != nil {
		t.Fatalf("writing ciphertext fixture: %v", err)
	}
	return stdout.Bytes()
}

func TestDecryptSopsRoundTrip(t *testing.T) {
	sopsPath := sopsBinary(t)
	dir := t.TempDir()

	id := newAgeIdentity(t)
	cleartext := "apiVersion: zarf.dev/v1alpha1\nkind: ZarfCluster\nmetadata:\n  name: test-cluster\n"
	ciphertext := encryptFixture(t, sopsPath, dir, cleartext, id.Recipient())

	if !IsSopsEncrypted(ciphertext) {
		t.Fatalf("IsSopsEncrypted() = false for sops-encrypted fixture")
	}

	identityFile := filepath.Join(dir, "identity.txt")
	if err := os.WriteFile(identityFile, []byte(id.String()+"\n"), 0o600); err != nil {
		t.Fatalf("writing age identity file: %v", err)
	}
	t.Setenv("SOPS_AGE_KEY_FILE", identityFile)

	got, err := DecryptSops(ciphertext)
	if err != nil {
		t.Fatalf("DecryptSops() error = %v", err)
	}

	var gotDoc, wantDoc map[string]any
	if err := goyaml.Unmarshal(got, &gotDoc); err != nil {
		t.Fatalf("unmarshaling decrypted output: %v", err)
	}
	if err := goyaml.Unmarshal([]byte(cleartext), &wantDoc); err != nil {
		t.Fatalf("unmarshaling original cleartext: %v", err)
	}
	if !reflect.DeepEqual(gotDoc, wantDoc) {
		t.Errorf("DecryptSops() = %#v, want %#v", gotDoc, wantDoc)
	}
}

func TestDecryptSopsMissingKey(t *testing.T) {
	sopsPath := sopsBinary(t)
	dir := t.TempDir()

	id := newAgeIdentity(t)
	cleartext := "apiVersion: zarf.dev/v1alpha1\nkind: ZarfCluster\n"
	ciphertext := encryptFixture(t, sopsPath, dir, cleartext, id.Recipient())

	// A different identity than the one it was encrypted to: sops must fail closed rather than
	// silently returning ciphertext or a zero value.
	other := newAgeIdentity(t)
	identityFile := filepath.Join(dir, "wrong-identity.txt")
	if err := os.WriteFile(identityFile, []byte(other.String()+"\n"), 0o600); err != nil {
		t.Fatalf("writing age identity file: %v", err)
	}
	t.Setenv("SOPS_AGE_KEY_FILE", identityFile)

	if _, err := DecryptSops(ciphertext); err == nil {
		t.Errorf("DecryptSops() error = nil, want an error for a key that cannot decrypt the document")
	}
}
