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
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
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

func TestDecryptAtPathRoundTrip(t *testing.T) {
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
			encrypted, err := EncryptAtPath([]byte(pathTestDoc), tt.path, testPassword, false)
			if err != nil {
				t.Fatalf("EncryptAtPath() error = %v", err)
			}

			got, err := DecryptAtPath(encrypted, tt.path, testPassword)
			if err != nil {
				t.Fatalf("DecryptAtPath() error = %v", err)
			}
			if value := readPath(t, got, tt.path); value != tt.want {
				t.Errorf("value at %s = %q, want %q", tt.path, value, tt.want)
			}
		})
	}
}

// TestDecryptAtPathRestoresTheDocument pins the property that makes the pair safe to use on a
// configuration under version control: encrypting a value and decrypting it again gives back the
// file that went in, byte for byte, rather than a re-rendering of it.
func TestDecryptAtPathRestoresTheDocument(t *testing.T) {
	for _, path := range []string{
		".spec.config.registries[0].auth.user",
		".spec.config.registries[0].auth.pass",
		".spec.config.registries[0].auth.token",
		".spec.config.registries[0].tls.ca",
		".spec.config.loadbalancer",
	} {
		t.Run(path, func(t *testing.T) {
			encrypted, err := EncryptAtPath([]byte(pathTestDoc), path, testPassword, false)
			if err != nil {
				t.Fatalf("EncryptAtPath() error = %v", err)
			}
			got, err := DecryptAtPath(encrypted, path, testPassword)
			if err != nil {
				t.Fatalf("DecryptAtPath() error = %v", err)
			}
			if string(got) != pathTestDoc {
				t.Errorf("round trip changed the document:\n got:\n%s\nwant:\n%s", got, pathTestDoc)
			}
		})
	}
}

// TestDecryptAtPathWritesAnyPlaintext covers the values a scalar has to be able to hold on the way
// back in. Two of these are wrong if go-yaml's encoder is trusted with them -- a control character
// written raw into a plain scalar, and a block scalar line whose trailing space does not survive
// contact with anything that trims whitespace -- so this is the guard on renderScalar quoting them
// instead.
func TestDecryptAtPathWritesAnyPlaintext(t *testing.T) {
	values := []string{
		"hunter2",
		"has: a colon",
		"has #a hash",
		"*starts-with-an-alias-marker",
		"  leading space",
		"trailing space  ",
		"123",
		"true",
		"null",
		"",
		"tab\tseparated",
		"carriage\r\nreturn",
		"control\x1bcharacter",
		"line1\nline2",
		"line1\nline2\n",
		"line1\ntrailing space  \nline3",
		"  leading space\nsecond line",
		"blank\n\n\nlines",
		"trailing\nblank lines\n\n\n",
		"\nleading blank line",
		"-----BEGIN CERTIFICATE-----\naGVsbG8=\n-----END CERTIFICATE-----\n",
		"émoji 🚀 and ünicode",
	}

	const path = ".spec.config.registries[0].auth.pass"
	for _, want := range values {
		t.Run(strconv.Quote(want), func(t *testing.T) {
			ciphertext, err := EncryptValue(want, testPassword)
			if err != nil {
				t.Fatalf("EncryptValue() error = %v", err)
			}
			// Put the ciphertext in the document the way encrypt-path would, so the decrypt runs
			// against a real block scalar rather than a hand-built one.
			doc, err := EncryptAtPath([]byte(pathTestDoc), path, testPassword, false)
			if err != nil {
				t.Fatalf("EncryptAtPath() error = %v", err)
			}
			node, err := mustPath(t, path).FilterFile(mustParse(t, doc))
			if err != nil {
				t.Fatalf("FilterFile() error = %v", err)
			}
			doc, err = spliceScalar(doc, node, "|-", strings.Split(strings.TrimRight(ciphertext, "\n"), "\n"))
			if err != nil {
				t.Fatalf("spliceScalar() error = %v", err)
			}

			got, err := DecryptAtPath(doc, path, testPassword)
			if err != nil {
				t.Fatalf("DecryptAtPath() error = %v", err)
			}
			if value := readPath(t, got, path); value != want {
				t.Errorf("value at %s = %q, want %q\ndocument:\n%s", path, value, want, got)
			}
			// A neighbouring key is the canary for a replacement that ran past its own value.
			if value := readPath(t, got, ".spec.config.registries[0].auth.token"); value != "tok #1" {
				t.Errorf("neighbouring token = %q, want %q\ndocument:\n%s", value, "tok #1", got)
			}
		})
	}
}

func TestDecryptAtPathRejectsPlaintext(t *testing.T) {
	_, err := DecryptAtPath([]byte(pathTestDoc), ".spec.config.registries[0].auth.pass", testPassword)
	if !errors.Is(err, ErrNotEncrypted) {
		t.Fatalf("DecryptAtPath() error = %v, want ErrNotEncrypted", err)
	}
}

func TestDecryptAtPathRejectsTheWrongPassword(t *testing.T) {
	const path = ".spec.config.registries[0].auth.pass"
	encrypted, err := EncryptAtPath([]byte(pathTestDoc), path, testPassword, false)
	if err != nil {
		t.Fatalf("EncryptAtPath() error = %v", err)
	}
	if _, err := DecryptAtPath(encrypted, path, "not the password"); err == nil {
		t.Fatal("DecryptAtPath() error = nil, want a decryption failure")
	}
}

func TestDecryptAtPathErrors(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"path not found", ".spec.config.registries[0].auth.missing"},
		{"path is a mapping", ".spec.config.registries[0].auth"},
		{"path is a sequence", ".spec.config.registries"},
		{"invalid path", ".spec.config.registries[bogus"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecryptAtPath([]byte(pathTestDoc), tt.path, testPassword); err == nil {
				t.Errorf("DecryptAtPath(%q) error = nil, want an error", tt.path)
			}
		})
	}
}

func TestDecryptValue(t *testing.T) {
	encrypted, err := EncryptValue("hunter2", testPassword)
	if err != nil {
		t.Fatalf("EncryptValue() error = %v", err)
	}
	got, err := DecryptValue(encrypted, testPassword)
	if err != nil {
		t.Fatalf("DecryptValue() error = %v", err)
	}
	if got != "hunter2" {
		t.Errorf("DecryptValue() = %q, want %q", got, "hunter2")
	}
	if _, err := DecryptValue("hunter2", testPassword); !errors.Is(err, ErrNotEncrypted) {
		t.Errorf("DecryptValue(plaintext) error = %v, want ErrNotEncrypted", err)
	}
}

// mustPath and mustParse give the tests the same two steps EncryptAtPath takes, so that one can
// build a document holding an arbitrary value without going through the public entry points.
func mustPath(t *testing.T, path string) *goyaml.Path {
	t.Helper()
	p, err := parseYAMLPath(path)
	if err != nil {
		t.Fatalf("parseYAMLPath() error = %v", err)
	}
	return p
}

func mustParse(t *testing.T, doc []byte) *ast.File {
	t.Helper()
	file, err := parser.ParseBytes(doc, parser.ParseComments)
	if err != nil {
		t.Fatalf("parser.ParseBytes() error = %v", err)
	}
	return file
}

// configTestDoc holds two registries so that the whole-file commands have to walk more than one,
// and leaves gaps a real configuration has: a registry with a token instead of a password, one
// with no tls block, and an empty field.
const configTestDoc = `apiVersion: zarf.dev/v1alpha1
kind: ZarfCluster
spec:
  # keep this comment exactly where it is
  config:
    loadbalancer: lb.example.com
    registries:
      - name: harbor # our mirror
        auth:
          user: admin
          pass: hunter2 # rotate me
          token: ""
        tls:
          ca: |
            -----BEGIN CERTIFICATE-----
            aGVsbG8gd29ybGQ=
            -----END CERTIFICATE-----
          insecureSkipVerify: false
      - name: quay
        auth:
          token: "tok #2"
  hosts:
    - name: node1
`

func TestEncryptConfigEncryptsEveryCredential(t *testing.T) {
	got, changed, err := EncryptConfig([]byte(configTestDoc), testPassword, false)
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

	// The empty token and the loadbalancer are the two things that should have been walked past.
	if value := readPath(t, got, ".spec.config.registries[0].auth.token"); value != "" {
		t.Errorf("empty token = %q, want it left alone", value)
	}
	if value := readPath(t, got, ".spec.config.loadbalancer"); value != "lb.example.com" {
		t.Errorf("loadbalancer = %q, want it left alone", value)
	}
	if !strings.Contains(string(got), "  # keep this comment exactly where it is") {
		t.Errorf("comment did not survive:\n%s", got)
	}
	if !strings.Contains(string(got), "          insecureSkipVerify: false") {
		t.Errorf("the key after the CA block did not survive:\n%s", got)
	}

	for _, path := range want {
		if value := readPath(t, got, path); !cluster.IsVaultEncrypted(value) {
			t.Errorf("value at %s = %q, want ciphertext", path, value)
		}
	}
}

// TestEncryptConfigDecryptsAtApplyTime is the check that matters most: what encrypt-file writes has
// to be what an apply reads back.
func TestEncryptConfigDecryptsAtApplyTime(t *testing.T) {
	got, _, err := EncryptConfig([]byte(configTestDoc), testPassword, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	var dis cluster.ZarfCluster
	if err := goyaml.Unmarshal(got, &dis); err != nil {
		t.Fatalf("unmarshalling:\n%s\nerror = %v", got, err)
	}
	if err := DecryptRegistryAuth(&dis, testPassword); err != nil {
		t.Fatalf("DecryptRegistryAuth() error = %v", err)
	}

	registries := dis.Spec.Config.Registries
	if len(registries) != 2 {
		t.Fatalf("got %d registries, want 2", len(registries))
	}
	if got, want := registries[0].Authentication.Username, "admin"; got != want {
		t.Errorf("user = %q, want %q", got, want)
	}
	if got, want := registries[0].Authentication.Password, "hunter2"; got != want {
		t.Errorf("pass = %q, want %q", got, want)
	}
	if got, want := registries[0].TLS.CA, "-----BEGIN CERTIFICATE-----\naGVsbG8gd29ybGQ=\n-----END CERTIFICATE-----\n"; got != want {
		t.Errorf("ca = %q, want %q", got, want)
	}
	if got, want := registries[1].Authentication.Token, "tok #2"; got != want {
		t.Errorf("token = %q, want %q", got, want)
	}
}

// TestEncryptConfigRoundTrip pins the property that makes the pair safe on a file under version
// control, across a document with several registries.
func TestEncryptConfigRoundTrip(t *testing.T) {
	encrypted, encryptedPaths, err := EncryptConfig([]byte(configTestDoc), testPassword, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	got, decryptedPaths, err := DecryptConfig(encrypted, testPassword)
	if err != nil {
		t.Fatalf("DecryptConfig() error = %v", err)
	}
	if strings.Join(decryptedPaths, ",") != strings.Join(encryptedPaths, ",") {
		t.Errorf("decrypted %v, want the %v that were encrypted", decryptedPaths, encryptedPaths)
	}
	if string(got) != configTestDoc {
		t.Errorf("round trip changed the document:\n got:\n%s\nwant:\n%s", got, configTestDoc)
	}
}

func TestEncryptConfigIsIdempotent(t *testing.T) {
	once, _, err := EncryptConfig([]byte(configTestDoc), testPassword, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	twice, changed, err := EncryptConfig(once, testPassword, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("changed = %v, want nothing left to encrypt", changed)
	}
	if string(twice) != string(once) {
		t.Errorf("a second run changed the document:\n%s", twice)
	}
}

// TestEncryptConfigFinishesAPartlyVaultedFile covers the case an operator actually hits: some
// values were encrypted by hand, and encrypt-file has to pick up the rest without touching them.
func TestEncryptConfigFinishesAPartlyVaultedFile(t *testing.T) {
	const done = "$.spec.config.registries[0].auth.pass"
	partial, err := EncryptAtPath([]byte(configTestDoc), done, testPassword, false)
	if err != nil {
		t.Fatalf("EncryptAtPath() error = %v", err)
	}
	before := readPath(t, partial, done)

	got, changed, err := EncryptConfig(partial, testPassword, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	for _, path := range changed {
		if path == done {
			t.Errorf("re-encrypted %s, which was encrypted already", done)
		}
	}
	if after := readPath(t, got, done); after != before {
		t.Errorf("ciphertext at %s changed", done)
	}

	if _, remaining, err := DecryptConfig(got, testPassword); err != nil || len(remaining) != 4 {
		t.Errorf("after finishing the job, DecryptConfig found %v (err = %v), want all four", remaining, err)
	}
}

func TestEncryptConfigForceRewrapsEverything(t *testing.T) {
	once, _, err := EncryptConfig([]byte(configTestDoc), testPassword, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	twice, changed, err := EncryptConfig(once, testPassword, true)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	if len(changed) != 4 {
		t.Fatalf("changed = %v, want all four wrapped again", changed)
	}

	// Unwrapping once should leave ciphertext rather than plaintext.
	unwrapped, _, err := DecryptConfig(twice, testPassword)
	if err != nil {
		t.Fatalf("DecryptConfig() error = %v", err)
	}
	if value := readPath(t, unwrapped, ".spec.config.registries[0].auth.pass"); !cluster.IsVaultEncrypted(value) {
		t.Errorf("value after one unwrap = %q, want the inner ciphertext", value)
	}
}

func TestDecryptConfigSkipsPlaintext(t *testing.T) {
	got, changed, err := DecryptConfig([]byte(configTestDoc), testPassword)
	if err != nil {
		t.Fatalf("DecryptConfig() error = %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("changed = %v, want nothing to decrypt", changed)
	}
	if string(got) != configTestDoc {
		t.Errorf("a document with nothing to decrypt was rewritten:\n%s", got)
	}
}

func TestConfigRejectsADocumentWithoutRegistries(t *testing.T) {
	tests := []struct {
		name string
		doc  string
	}{
		{"no registries key", "apiVersion: zarf.dev/v1alpha1\nspec:\n  config: {}\n"},
		{"registries is not a list", "spec:\n  config:\n    registries: nope\n"},
		{"not YAML at all", "\tnot: [valid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := EncryptConfig([]byte(tt.doc), testPassword, false); err == nil {
				t.Error("EncryptConfig() error = nil, want an error")
			}
			if _, _, err := DecryptConfig([]byte(tt.doc), testPassword); err == nil {
				t.Error("DecryptConfig() error = nil, want an error")
			}
		})
	}
}

// TestVaultedFieldsMatchPathIsDecryptable ties the list the whole-file commands walk to the paths
// the warning accepts, so that the two cannot drift apart.
func TestVaultedFieldsMatchPathIsDecryptable(t *testing.T) {
	for _, field := range vaultedFields {
		path := registriesPath + "[0]." + field
		if !PathIsDecryptable(path) {
			t.Errorf("PathIsDecryptable(%q) = false, want true", path)
		}
	}
}

// TestEncryptConfigRotatesOneCredential covers the everyday mixed case: the registry password
// changed, so the operator pastes the new one in as plaintext and leaves the vaulted username
// alone. The same vault password is used throughout, so encrypt-file has to pick up the new value
// without disturbing the old one, and an apply has to read both back.
func TestEncryptConfigRotatesOneCredential(t *testing.T) {
	vaulted, _, err := EncryptConfig([]byte(configTestDoc), testPassword, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	user := readPath(t, vaulted, ".spec.config.registries[0].auth.user")

	// What the operator does by hand: replace the ciphertext with the new password in the clear.
	got, changed, err := EncryptConfig(editPass(t, vaulted, "hunter3"), testPassword, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	if want := []string{"$.spec.config.registries[0].auth.pass"}; strings.Join(changed, ",") != strings.Join(want, ",") {
		t.Fatalf("changed = %v, want only the edited password", changed)
	}
	if after := readPath(t, got, ".spec.config.registries[0].auth.user"); after != user {
		t.Errorf("the untouched username was rewritten")
	}

	var dis cluster.ZarfCluster
	if err := goyaml.Unmarshal(got, &dis); err != nil {
		t.Fatalf("unmarshalling:\n%s\nerror = %v", got, err)
	}
	if err := DecryptRegistryAuth(&dis, testPassword); err != nil {
		t.Fatalf("DecryptRegistryAuth() error = %v", err)
	}
	if got, want := dis.Spec.Config.Registries[0].Authentication.Username, "admin"; got != want {
		t.Errorf("user = %q, want %q", got, want)
	}
	if got, want := dis.Spec.Config.Registries[0].Authentication.Password, "hunter3"; got != want {
		t.Errorf("pass = %q, want %q", got, want)
	}
}

// TestEncryptConfigRejectsAnotherVaultPassword covers the mixed case that is not safe: a document
// holding values vaulted under one password, encrypted with another. An apply decrypts a registry's
// fields with a single password, so a document like that is one no password can read. Catching it
// here is the difference between a clear error and an apply that fails several phases in.
func TestEncryptConfigRejectsAnotherVaultPassword(t *testing.T) {
	const otherPassword = "a-different-vault-password"

	vaulted, _, err := EncryptConfig([]byte(configTestDoc), testPassword, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	edited := editPass(t, vaulted, "hunter3")

	_, _, err = EncryptConfig(edited, otherPassword, false)
	if err == nil {
		t.Fatal("EncryptConfig() error = nil, want a refusal to mix vault passwords")
	}
	for _, want := range []string{"auth.user", "different password", "decrypt the file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

// TestDecryptConfigRejectsAnotherVaultPassword is the same hazard from the other side, and matters
// more, because this is the direction the fix runs in: a wrong password has to fail rather than
// leave some values in the clear and the rest vaulted.
func TestDecryptConfigRejectsAnotherVaultPassword(t *testing.T) {
	vaulted, _, err := EncryptConfig([]byte(configTestDoc), testPassword, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	got, _, err := DecryptConfig(vaulted, "a-different-vault-password")
	if err == nil {
		t.Fatal("DecryptConfig() error = nil, want an error")
	}
	if got != nil {
		t.Errorf("a failed run returned a document, which a caller might write: \n%s", got)
	}
}

// editPass replaces the value at the first registry's auth.pass with plaintext, standing in for an
// operator editing the file by hand.
func editPass(t *testing.T, doc []byte, value string) []byte {
	t.Helper()
	got, err := DecryptAtPath(doc, "$.spec.config.registries[0].auth.pass", testPassword)
	if err != nil {
		t.Fatalf("DecryptAtPath() error = %v", err)
	}
	old := readPath(t, got, ".spec.config.registries[0].auth.pass")
	return []byte(strings.Replace(string(got), old, value, 1))
}
