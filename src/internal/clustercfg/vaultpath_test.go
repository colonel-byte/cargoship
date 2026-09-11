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
	"strconv"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	goyaml "github.com/goccy/go-yaml"
	vault "github.com/sosedoff/ansible-vault-go"
)

// pathTestDoc covers the shapes a registry credential is written in: a bare scalar, a quoted one,
// a scalar carrying a trailing comment, and a multi-line PEM block.
const pathTestDoc = `apiVersion: zarf.dev/v1alpha1
kind: ZarfCluster
spec:
  # keep this comment exactly where it is
  config:
    loadbalancer: lb.example.com
    registries:
      - name: harbor # a trailing comment
        auth:
          user: admin
          pass: hunter2 # not the password you think
          token: "tok #1"
        tls:
          ca: |
            -----BEGIN CERTIFICATE-----
            aGVsbG8gd29ybGQ=
            -----END CERTIFICATE-----
          insecureSkipVerify: false
  hosts:
    - name: node1
`

const testPassword = "correct horse battery staple"

func TestEncryptAtPathRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"plain scalar", ".spec.config.registries[0].auth.pass", "hunter2"},
		{"quoted scalar", ".spec.config.registries[0].auth.token", "tok #1"},
		{"literal block", ".spec.config.registries[0].tls.ca", "-----BEGIN CERTIFICATE-----\naGVsbG8gd29ybGQ=\n-----END CERTIFICATE-----\n"},
		{"rooted path", "$.spec.config.registries[0].auth.user", "admin"},
		{"unprefixed path", "spec.config.loadbalancer", "lb.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncryptAtPath([]byte(pathTestDoc), tt.path, testPassword, false)
			if err != nil {
				t.Fatalf("EncryptAtPath() error = %v", err)
			}

			ciphertext := readPath(t, got, tt.path)
			if !cluster.IsVaultEncrypted(ciphertext) {
				t.Fatalf("value at %s = %q, want Ansible Vault ciphertext", tt.path, ciphertext)
			}
			plain, err := vault.Decrypt(ciphertext, testPassword)
			if err != nil {
				t.Fatalf("vault.Decrypt() error = %v", err)
			}
			if plain != tt.want {
				t.Errorf("decrypted value = %q, want %q", plain, tt.want)
			}
		})
	}
}

// TestEncryptAtPathPreservesEverythingElse is the guard against re-rendering the document instead
// of splicing it: printing a replaced node back out drops comments and truncates ciphertext nested
// in a sequence, and both would show up here.
func TestEncryptAtPathPreservesEverythingElse(t *testing.T) {
	got, err := EncryptAtPath([]byte(pathTestDoc), ".spec.config.registries[0].auth.pass", testPassword, false)
	if err != nil {
		t.Fatalf("EncryptAtPath() error = %v", err)
	}

	gotLines := strings.Split(string(got), "\n")
	wantLines := strings.Split(pathTestDoc, "\n")

	// Everything above the replaced value is untouched.
	const replaced = 10 // 0-based index of the "pass:" line
	for i := range replaced {
		if gotLines[i] != wantLines[i] {
			t.Errorf("line %d = %q, want %q", i+1, gotLines[i], wantLines[i])
		}
	}
	if want := "          pass: |- # not the password you think"; gotLines[replaced] != want {
		t.Errorf("replaced line = %q, want %q", gotLines[replaced], want)
	}

	// Everything below it is untouched, and shifted only by the ciphertext lines inserted.
	shift := len(gotLines) - len(wantLines)
	if shift < 1 {
		t.Fatalf("document grew by %d lines, want at least 1", shift)
	}
	for i := replaced + 1; i < len(wantLines); i++ {
		if gotLines[i+shift] != wantLines[i] {
			t.Errorf("line %d = %q, want %q", i+shift+1, gotLines[i+shift], wantLines[i])
		}
	}
	for i := replaced + 1; i <= replaced+shift; i++ {
		if !strings.HasPrefix(gotLines[i], "            ") {
			t.Errorf("ciphertext line %d = %q, want it indented two past its key", i+1, gotLines[i])
		}
	}
}

// TestEncryptAtPathDecryptsAtApplyTime checks the whole point of the command: a document it has
// written is one the apply path can still read.
func TestEncryptAtPathDecryptsAtApplyTime(t *testing.T) {
	got := []byte(pathTestDoc)
	for _, path := range []string{
		".spec.config.registries[0].auth.user",
		".spec.config.registries[0].auth.pass",
		".spec.config.registries[0].tls.ca",
	} {
		var err error
		if got, err = EncryptAtPath(got, path, testPassword, false); err != nil {
			t.Fatalf("EncryptAtPath(%s) error = %v", path, err)
		}
	}

	dis := &cluster.ZarfCluster{}
	if err := goyaml.Unmarshal(got, dis); err != nil {
		t.Fatalf("goyaml.Unmarshal() error = %v", err)
	}
	if err := DecryptRegistryAuth(dis, testPassword); err != nil {
		t.Fatalf("DecryptRegistryAuth() error = %v", err)
	}

	auth := dis.Spec.Config.Registries[0].Authentication
	if auth.Username != "admin" {
		t.Errorf("auth.user = %q, want %q", auth.Username, "admin")
	}
	if auth.Password != "hunter2" {
		t.Errorf("auth.pass = %q, want %q", auth.Password, "hunter2")
	}
	if want := "-----BEGIN CERTIFICATE-----\naGVsbG8gd29ybGQ=\n-----END CERTIFICATE-----\n"; dis.Spec.Config.Registries[0].TLS.CA != want {
		t.Errorf("tls.ca = %q, want %q", dis.Spec.Config.Registries[0].TLS.CA, want)
	}
}

func TestEncryptAtPathRejectsAlreadyEncrypted(t *testing.T) {
	const path = ".spec.config.registries[0].auth.pass"
	once, err := EncryptAtPath([]byte(pathTestDoc), path, testPassword, false)
	if err != nil {
		t.Fatalf("EncryptAtPath() error = %v", err)
	}

	if _, err := EncryptAtPath(once, path, testPassword, false); !errors.Is(err, ErrAlreadyEncrypted) {
		t.Fatalf("EncryptAtPath() error = %v, want ErrAlreadyEncrypted", err)
	}

	twice, err := EncryptAtPath(once, path, testPassword, true)
	if err != nil {
		t.Fatalf("EncryptAtPath(reencrypt) error = %v", err)
	}
	outer, err := vault.Decrypt(readPath(t, twice, path), testPassword)
	if err != nil {
		t.Fatalf("vault.Decrypt() error = %v", err)
	}
	plain, err := vault.Decrypt(outer, testPassword)
	if err != nil {
		t.Fatalf("vault.Decrypt() error = %v", err)
	}
	if plain != "hunter2" {
		t.Errorf("twice-decrypted value = %q, want %q", plain, "hunter2")
	}
}

func TestEncryptAtPathErrors(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{"missing path", ".spec.config.registries[0].auth.missing", "no value found"},
		{"missing index", ".spec.config.registries[7].auth.pass", "no value found"},
		{"mapping", ".spec.config.registries[0].auth", "not a single scalar value"},
		{"sequence", ".spec.config.registries", "not a single scalar value"},
		{"malformed path", ".spec.config.registries[", "invalid YAML path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := EncryptAtPath([]byte(pathTestDoc), tt.path, testPassword, false)
			if err == nil {
				t.Fatalf("EncryptAtPath() error = nil, want one containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("EncryptAtPath() error = %q, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestEncryptAtPathRejectsInvalidYAML(t *testing.T) {
	_, err := EncryptAtPath([]byte("spec:\n\tpass: x\n"), ".spec.pass", testPassword, false)
	if err == nil || !strings.Contains(err.Error(), "parsing YAML") {
		t.Fatalf("EncryptAtPath() error = %v, want one containing %q", err, "parsing YAML")
	}
}

func TestPathIsDecryptable(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{".spec.config.registries[0].auth.user", true},
		{".spec.config.registries[0].auth.pass", true},
		{".spec.config.registries[12].auth.token", true},
		{".spec.config.registries[0].tls.ca", true},
		{"$.spec.config.registries[0].auth.pass", true},
		{".spec.config.registries[0].tls.insecureSkipVerify", false},
		{".spec.config.registries[0].name", false},
		{".spec.config.loadbalancer", false},
		{".spec.config.registries[", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := PathIsDecryptable(tt.path); got != tt.want {
				t.Errorf("PathIsDecryptable(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// readPath returns the string value the document holds at path, so a test can assert on what a
// YAML reader sees rather than on the block scalar it is spelled with.
//
// It walks a plain unmarshalled document rather than using go-yaml's own Path.Read, which drops
// the trailing newline a clipped block scalar carries -- the very thing several of these tests are
// checking survives. Unmarshalling is also what cargoship does to a configuration for real.
func readPath(t *testing.T, doc []byte, path string) string {
	t.Helper()
	var root any
	if err := goyaml.Unmarshal(doc, &root); err != nil {
		t.Fatalf("unmarshalling:\n%s\nerror = %v", doc, err)
	}

	cursor := root
	for _, segment := range strings.Split(strings.TrimPrefix(strings.TrimPrefix(path, "$"), "."), ".") {
		key, indexes, found := strings.Cut(segment, "[")
		mapping, ok := cursor.(map[string]any)
		if !ok {
			t.Fatalf("reading %s: %q is not a mapping in:\n%s", path, key, doc)
		}
		cursor = mapping[key]
		if !found {
			continue
		}
		index, err := strconv.Atoi(strings.TrimSuffix(indexes, "]"))
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		sequence, ok := cursor.([]any)
		if !ok || index >= len(sequence) {
			t.Fatalf("reading %s: %q is not a sequence with an index %d in:\n%s", path, key, index, doc)
		}
		cursor = sequence[index]
	}

	value, ok := cursor.(string)
	if !ok {
		t.Fatalf("reading %s: value is %T, not a string, in:\n%s", path, cursor, doc)
	}
	return value
}
