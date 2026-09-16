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
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	goyaml "github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	vault "github.com/sosedoff/ansible-vault-go"
)

const testPassword = "correct horse battery staple"

// testKeyring is testPassword in the form every encrypt and decrypt entry point takes. The raw
// password is kept beside it for the assertions that go straight to the vault library.
var testKeyring = NewVaultKeyring(testPassword)

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
			got, err := EncryptAtPath(readPathsInventoryFixture(t), tt.path, testKeyring, false)
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
	got, err := EncryptAtPath(readPathsInventoryFixture(t), ".spec.config.registries[0].auth.pass", testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptAtPath() error = %v", err)
	}

	gotLines := strings.Split(string(got), "\n")
	wantLines := strings.Split(string(readPathsInventoryFixture(t)), "\n")

	// Everything above the replaced value is untouched. The line is found rather than counted to,
	// so that a comment added to the fixture does not read as the command having moved something.
	replaced := -1
	for i, line := range wantLines {
		if strings.HasPrefix(line, "          pass: ") {
			replaced = i
			break
		}
	}
	if replaced < 0 {
		t.Fatalf("the fixture holds no pass line:\n%s", strings.Join(wantLines, "\n"))
	}
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
	got := readPathsInventoryFixture(t)
	for _, path := range []string{
		".spec.config.registries[0].auth.user",
		".spec.config.registries[0].auth.pass",
		".spec.config.registries[0].tls.ca",
	} {
		var err error
		if got, err = EncryptAtPath(got, path, testKeyring, false); err != nil {
			t.Fatalf("EncryptAtPath(%s) error = %v", path, err)
		}
	}

	dis := &cluster.ZarfCluster{}
	if err := goyaml.Unmarshal(got, dis); err != nil {
		t.Fatalf("goyaml.Unmarshal() error = %v", err)
	}
	if err := DecryptRegistryAuth(dis, testKeyring); err != nil {
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
	once, err := EncryptAtPath(readPathsInventoryFixture(t), path, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptAtPath() error = %v", err)
	}

	if _, err := EncryptAtPath(once, path, testKeyring, false); !errors.Is(err, ErrAlreadyEncrypted) {
		t.Fatalf("EncryptAtPath() error = %v, want ErrAlreadyEncrypted", err)
	}

	twice, err := EncryptAtPath(once, path, testKeyring, true)
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
			_, err := EncryptAtPath(readPathsInventoryFixture(t), tt.path, testKeyring, false)
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
	_, err := EncryptAtPath([]byte("spec:\n\tpass: x\n"), ".spec.pass", testKeyring, false)
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
			encrypted, err := EncryptAtPath(readPathsInventoryFixture(t), tt.path, testKeyring, false)
			if err != nil {
				t.Fatalf("EncryptAtPath() error = %v", err)
			}

			got, err := DecryptAtPath(encrypted, tt.path, testKeyring)
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
			doc := readPathsInventoryFixture(t)
			encrypted, err := EncryptAtPath(doc, path, testKeyring, false)
			if err != nil {
				t.Fatalf("EncryptAtPath() error = %v", err)
			}
			got, err := DecryptAtPath(encrypted, path, testKeyring)
			if err != nil {
				t.Fatalf("DecryptAtPath() error = %v", err)
			}
			if string(got) != string(doc) {
				t.Errorf("round trip changed the document:\n got:\n%s\nwant:\n%s", got, doc)
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
			ciphertext, err := EncryptValue(want, testKeyring)
			if err != nil {
				t.Fatalf("EncryptValue() error = %v", err)
			}
			// Put the ciphertext in the document the way encrypt-path would, so the decrypt runs
			// against a real block scalar rather than a hand-built one.
			doc, err := EncryptAtPath(readPathsInventoryFixture(t), path, testKeyring, false)
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

			got, err := DecryptAtPath(doc, path, testKeyring)
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
	_, err := DecryptAtPath(readPathsInventoryFixture(t), ".spec.config.registries[0].auth.pass", testKeyring)
	if !errors.Is(err, ErrNotEncrypted) {
		t.Fatalf("DecryptAtPath() error = %v, want ErrNotEncrypted", err)
	}
}

func TestDecryptAtPathRejectsTheWrongPassword(t *testing.T) {
	const path = ".spec.config.registries[0].auth.pass"
	encrypted, err := EncryptAtPath(readPathsInventoryFixture(t), path, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptAtPath() error = %v", err)
	}
	if _, err := DecryptAtPath(encrypted, path, NewVaultKeyring("not the password")); err == nil {
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
			if _, err := DecryptAtPath(readPathsInventoryFixture(t), tt.path, testKeyring); err == nil {
				t.Errorf("DecryptAtPath(%q) error = nil, want an error", tt.path)
			}
		})
	}
}

func TestDecryptValue(t *testing.T) {
	encrypted, err := EncryptValue("hunter2", testKeyring)
	if err != nil {
		t.Fatalf("EncryptValue() error = %v", err)
	}
	got, err := DecryptValue(encrypted, testKeyring)
	if err != nil {
		t.Fatalf("DecryptValue() error = %v", err)
	}
	if got != "hunter2" {
		t.Errorf("DecryptValue() = %q, want %q", got, "hunter2")
	}
	if _, err := DecryptValue("hunter2", testKeyring); !errors.Is(err, ErrNotEncrypted) {
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

const (
	anchoredInventoryFixture      = "../../test/e2e/noncluster/testdata/inventory-anchors.yaml"
	vaultInventoryFixture         = "../../test/e2e/noncluster/testdata/inventory-vault.yaml"
	mergeOverrideInventoryFixture = "../../test/e2e/noncluster/testdata/inventory-merge-override.yaml"
	listMergeInventoryFixture     = "../../test/e2e/noncluster/testdata/inventory-list-merge.yaml"
	pathsInventoryFixture         = "../../test/e2e/noncluster/testdata/inventory-paths.yaml"
	flowInventoryFixture          = "../../test/e2e/noncluster/testdata/inventory-flow.yaml"
)

func readAnchoredInventoryFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(anchoredInventoryFixture)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", anchoredInventoryFixture, err)
	}
	return data
}

func readVaultInventoryFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(vaultInventoryFixture)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", vaultInventoryFixture, err)
	}
	return data
}

func readMergeOverrideInventoryFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(mergeOverrideInventoryFixture)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", mergeOverrideInventoryFixture, err)
	}
	return data
}

func readListMergeInventoryFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(listMergeInventoryFixture)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", listMergeInventoryFixture, err)
	}
	return data
}

func readFlowInventoryFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(flowInventoryFixture)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", flowInventoryFixture, err)
	}
	return data
}

func readPathsInventoryFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(pathsInventoryFixture)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", pathsInventoryFixture, err)
	}
	return data
}

// TestEncryptConfigResolvesAnchors covers the document go-yaml's path filter cannot walk on its
// own: resolving anchors, aliases, and merge keys so that each shared credential is encrypted once.
func TestEncryptConfigResolvesAnchors(t *testing.T) {
	fixture := readAnchoredInventoryFixture(t)
	got, changed, _, err := EncryptConfig(fixture, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	// Once each: a value two registries share is one value, so the second registry's path names
	// bytes the first has already rewritten and is left out rather than walked twice.
	want := []string{
		"$.spec.config.registries[0].auth.user",
		"$.spec.config.registries[0].auth.pass",
		"$.spec.config.registries[0].tls.ca",
		"$.spec.config.registries[2].auth.user",
		"$.spec.config.registries[2].auth.pass",
		"$.spec.config.registries[3].auth.token",
		"$.spec.config.registries[4].auth.user",
		"$.spec.config.registries[4].auth.pass",
	}
	if strings.Join(changed, ",") != strings.Join(want, ",") {
		t.Fatalf("changed = %v, want %v", changed, want)
	}
	for _, plaintext := range []string{"hunter2", "correct-horse", "ghcr-token", "proxy-robot", "proxy-pass"} {
		if strings.Contains(string(got), plaintext) {
			t.Errorf("the credential %q is still in the clear:\n%s", plaintext, got)
		}
	}
	for _, marker := range []string{
		"auth: &robot-auth",
		"auth: *robot-auth",
		"tls: &internal-tls",
		"tls: *internal-tls",
		"<<: *internal-tls",
		"<<: &proxy-auth-tls",
		"<<: *proxy-auth-tls",
	} {
		if !strings.Contains(string(got), marker) {
			t.Errorf("the marker %q did not survive:\n%s", marker, got)
		}
	}

	// All registries read the credentials back, which is what sharing is for.
	var dis cluster.ZarfCluster
	if err := goyaml.Unmarshal(got, &dis); err != nil {
		t.Fatalf("unmarshalling:\n%s\nerror = %v", got, err)
	}
	if err := DecryptRegistryAuth(&dis, testKeyring); err != nil {
		t.Fatalf("DecryptRegistryAuth() error = %v", err)
	}
	for _, registry := range dis.Spec.Config.Registries {
		switch registry.Name {
		case "registry.test.local", "mirror.test.local":
			if registry.Authentication.Username != "robot" {
				t.Errorf("registry %s user = %q, want %q", registry.Name, registry.Authentication.Username, "robot")
			}
			if registry.Authentication.Password != "hunter2" {
				t.Errorf("registry %s pass = %q, want %q", registry.Name, registry.Authentication.Password, "hunter2")
			}
		case "lab.test.local":
			if registry.Authentication.Username != "lab-robot" {
				t.Errorf("registry %s user = %q, want %q", registry.Name, registry.Authentication.Username, "lab-robot")
			}
			if registry.Authentication.Password != "correct-horse" {
				t.Errorf("registry %s pass = %q, want %q", registry.Name, registry.Authentication.Password, "correct-horse")
			}
		case "ghcr.test.local":
			if registry.Authentication.Token != "ghcr-token" {
				t.Errorf("registry %s token = %q, want %q", registry.Name, registry.Authentication.Token, "ghcr-token")
			}
		case "proxy.test.local", "proxy-mirror.test.local":
			if registry.Authentication.Username != "proxy-robot" {
				t.Errorf("registry %s user = %q, want %q", registry.Name, registry.Authentication.Username, "proxy-robot")
			}
			if registry.Authentication.Password != "proxy-pass" {
				t.Errorf("registry %s pass = %q, want %q", registry.Name, registry.Authentication.Password, "proxy-pass")
			}
		default:
			t.Errorf("registry %q is not one this test checks; the fixture and this list have drifted apart", registry.Name)
		}
	}
}

// TestRekeyConfigResolvesAnchors is the case a shared value breaks if each path is walked on its
// own: the second visit would find ciphertext under the new password and report it as a value the
// old password cannot read.
func TestRekeyConfigResolvesAnchors(t *testing.T) {
	fixture := readAnchoredInventoryFixture(t)
	vaulted, _, _, err := EncryptConfig(fixture, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	got, changed, _, err := RekeyConfig(vaulted, testKeyring, rekeyKeyring)
	if err != nil {
		t.Fatalf("RekeyConfig() error = %v", err)
	}
	want := []string{
		"$.spec.config.registries[0].auth.user",
		"$.spec.config.registries[0].auth.pass",
		"$.spec.config.registries[0].tls.ca",
		"$.spec.config.registries[2].auth.user",
		"$.spec.config.registries[2].auth.pass",
		"$.spec.config.registries[3].auth.token",
		"$.spec.config.registries[4].auth.user",
		"$.spec.config.registries[4].auth.pass",
	}
	if strings.Join(changed, ",") != strings.Join(want, ",") {
		t.Fatalf("changed = %v, want the shared credentials once each: %v", changed, want)
	}

	plain, _, _, err := DecryptConfig(got, rekeyKeyring)
	if err != nil {
		t.Fatalf("DecryptConfig() error = %v", err)
	}
	if string(plain) != string(fixture) {
		t.Errorf("round trip through a rotation changed the document:\n%s\nwant:\n%s", plain, string(fixture))
	}
}

// TestEncryptConfigMergeKeyOverride is the case that used to leave a credential in the clear: the
// merged keys were placed ahead of the mapping's own, so the walk reached the shared password
// through both registries, rewrote it twice over, and never saw the one written beside the merge.
// encrypt-file then reported the file as done over a password still sitting in it.
//
// There is no apply-time half to this test, for the reason inventory-merge-override.yaml gives: the
// decoder refuses the document. That refusal is the operator's answer for what the file means; it is
// not a reason for the command that exists to get credentials out of the clear to leave one there.
func TestEncryptConfigMergeKeyOverride(t *testing.T) {
	fixture := readMergeOverrideInventoryFixture(t)
	got, changed, _, err := EncryptConfig(fixture, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	want := []string{
		"$.spec.config.registries[0].auth.user",
		"$.spec.config.registries[0].auth.pass",
		"$.spec.config.registries[1].auth.user",
		"$.spec.config.registries[1].auth.pass",
	}
	if strings.Join(changed, ",") != strings.Join(want, ",") {
		t.Fatalf("changed = %v, want %v", changed, want)
	}
	for _, plaintext := range []string{"shared-user", "shared-pass", "own-user", "own-pass"} {
		if strings.Contains(string(got), plaintext) {
			t.Errorf("the credential %q is still in the clear:\n%s", plaintext, got)
		}
	}
	if !strings.Contains(string(got), "<<: *shared-auth") {
		t.Errorf("the merge did not survive:\n%s", got)
	}
}

// TestEncryptConfigResolvesASequenceMerge covers the list form of a merge key, which used to be
// walked past without a word: the registry read as one holding no credentials at all rather than as
// one reaching three through the merge, and encrypt-file reported the file as done over all three.
// There is no apply-time half here, for the reason inventory-list-merge.yaml gives.
func TestEncryptConfigResolvesASequenceMerge(t *testing.T) {
	fixture := readListMergeInventoryFixture(t)
	got, changed, _, err := EncryptConfig(fixture, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	want := []string{
		"$.spec.config.registries[0].auth.user",
		"$.spec.config.registries[0].auth.pass",
		"$.spec.config.registries[0].tls.ca",
	}
	if strings.Join(changed, ",") != strings.Join(want, ",") {
		t.Fatalf("changed = %v, want %v", changed, want)
	}
	for _, plaintext := range []string{"list-user", "list-pass", "list-ca"} {
		if strings.Contains(string(got), plaintext) {
			t.Errorf("the credential %q is still in the clear:\n%s", plaintext, got)
		}
	}
	for _, marker := range []string{"        <<:", "          - auth:", "          - tls:"} {
		if !strings.Contains(string(got), marker) {
			t.Errorf("the marker %q did not survive:\n%s", marker, got)
		}
	}
}

// TestEncryptConfigRejectsAMergeKeyNamingAScalar is the other half of resolving merges, as rejecting
// an alias with no anchor is for aliases: a merge this package cannot follow has to say so, rather
// than report the keys behind it as absent and the file as having had nothing to do.
func TestEncryptConfigRejectsAMergeKeyNamingAScalar(t *testing.T) {
	fixture := readAnchoredInventoryFixture(t)
	doc := strings.Replace(string(fixture), "<<: *ssh-defaults", "<<: not-a-mapping", 1)

	_, _, _, err := EncryptConfig([]byte(doc), testKeyring, false)
	if err == nil {
		t.Fatal("EncryptConfig() error = nil, want an error naming the merge key")
	}
	if !strings.Contains(err.Error(), "merge key") {
		t.Errorf("error = %v, want it to name the merge key", err)
	}
}

// TestEncryptAtPathThroughAlias covers naming the aliased registry rather than the anchored one:
// the value it reaches is the anchored one, so that is what gets rewritten.
func TestEncryptAtPathThroughAlias(t *testing.T) {
	fixture := readAnchoredInventoryFixture(t)
	got, err := EncryptAtPath(fixture, ".spec.config.registries[1].auth.pass", testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptAtPath() error = %v", err)
	}
	if strings.Contains(string(got), "pass: hunter2") {
		t.Errorf("the shared password is still in the clear:\n%s", got)
	}
	if !strings.Contains(string(got), "auth: *robot-auth") {
		t.Errorf("the alias did not survive:\n%s", got)
	}

	value := readPath(t, got, ".spec.config.registries[0].auth.pass")
	if !cluster.IsVaultEncrypted(value) {
		t.Errorf("value at the anchor = %q, want the ciphertext written through the alias", value)
	}
}

// TestEncryptConfigRejectsAnAliasWithoutAnAnchor is the other half of resolving them: a document
// this package cannot walk has to say so, rather than come back reporting nothing to do.
func TestEncryptConfigRejectsAnAliasWithoutAnAnchor(t *testing.T) {
	fixture := readAnchoredInventoryFixture(t)
	doc := strings.Replace(string(fixture), "auth: &robot-auth", "auth:", 1)

	_, _, _, err := EncryptConfig([]byte(doc), testKeyring, false)
	if err == nil {
		t.Fatal("EncryptConfig() error = nil, want an error naming the alias")
	}
	if !strings.Contains(err.Error(), "*robot-auth") {
		t.Errorf("error = %v, want it to name the alias", err)
	}
}

func TestEncryptConfigEncryptsEveryCredential(t *testing.T) {
	doc := readVaultInventoryFixture(t)
	got, changed, _, err := EncryptConfig(doc, testKeyring, false)
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
	if !strings.Contains(string(got), "  # registries the cluster pulls from") {
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
	doc := readVaultInventoryFixture(t)
	got, _, _, err := EncryptConfig(doc, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	var dis cluster.ZarfCluster
	if err := goyaml.Unmarshal(got, &dis); err != nil {
		t.Fatalf("unmarshalling:\n%s\nerror = %v", got, err)
	}
	if err := DecryptRegistryAuth(&dis, testKeyring); err != nil {
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
	doc := readVaultInventoryFixture(t)
	encrypted, encryptedPaths, _, err := EncryptConfig(doc, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	got, decryptedPaths, _, err := DecryptConfig(encrypted, testKeyring)
	if err != nil {
		t.Fatalf("DecryptConfig() error = %v", err)
	}
	if strings.Join(decryptedPaths, ",") != strings.Join(encryptedPaths, ",") {
		t.Errorf("decrypted %v, want the %v that were encrypted", decryptedPaths, encryptedPaths)
	}
	if string(got) != string(doc) {
		t.Errorf("round trip changed the document:\n got:\n%s\nwant:\n%s", got, string(doc))
	}
}

func TestEncryptConfigIsIdempotent(t *testing.T) {
	doc := readVaultInventoryFixture(t)
	once, _, _, err := EncryptConfig(doc, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	twice, changed, _, err := EncryptConfig(once, testKeyring, false)
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
	doc := readVaultInventoryFixture(t)
	const done = "$.spec.config.registries[0].auth.pass"
	partial, err := EncryptAtPath(doc, done, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptAtPath() error = %v", err)
	}
	before := readPath(t, partial, done)

	got, changed, _, err := EncryptConfig(partial, testKeyring, false)
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

	if _, remaining, _, err := DecryptConfig(got, testKeyring); err != nil || len(remaining) != 4 {
		t.Errorf("after finishing the job, DecryptConfig found %v (err = %v), want all four", remaining, err)
	}
}

func TestEncryptConfigForceRewrapsEverything(t *testing.T) {
	doc := readVaultInventoryFixture(t)
	once, _, _, err := EncryptConfig(doc, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	twice, changed, _, err := EncryptConfig(once, testKeyring, true)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	if len(changed) != 4 {
		t.Fatalf("changed = %v, want all four wrapped again", changed)
	}

	// Unwrapping once should leave ciphertext rather than plaintext.
	unwrapped, _, _, err := DecryptConfig(twice, testKeyring)
	if err != nil {
		t.Fatalf("DecryptConfig() error = %v", err)
	}
	if value := readPath(t, unwrapped, ".spec.config.registries[0].auth.pass"); !cluster.IsVaultEncrypted(value) {
		t.Errorf("value after one unwrap = %q, want the inner ciphertext", value)
	}
}

func TestDecryptConfigSkipsPlaintext(t *testing.T) {
	doc := readVaultInventoryFixture(t)
	got, changed, _, err := DecryptConfig(doc, testKeyring)
	if err != nil {
		t.Fatalf("DecryptConfig() error = %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("changed = %v, want nothing to decrypt", changed)
	}
	if string(got) != string(doc) {
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
			if _, _, _, err := EncryptConfig([]byte(tt.doc), testKeyring, false); err == nil {
				t.Error("EncryptConfig() error = nil, want an error")
			}
			if _, _, _, err := DecryptConfig([]byte(tt.doc), testKeyring); err == nil {
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
	doc := readVaultInventoryFixture(t)
	vaulted, _, _, err := EncryptConfig(doc, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	user := readPath(t, vaulted, ".spec.config.registries[0].auth.user")

	// What the operator does by hand: replace the ciphertext with the new password in the clear.
	got, changed, _, err := EncryptConfig(editPass(t, vaulted, "hunter3"), testKeyring, false)
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
	if err := DecryptRegistryAuth(&dis, testKeyring); err != nil {
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

	doc := readVaultInventoryFixture(t)
	vaulted, _, _, err := EncryptConfig(doc, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	edited := editPass(t, vaulted, "hunter3")

	_, _, _, err = EncryptConfig(edited, NewVaultKeyring(otherPassword), false)
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
	doc := readVaultInventoryFixture(t)
	vaulted, _, _, err := EncryptConfig(doc, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	got, _, _, err := DecryptConfig(vaulted, NewVaultKeyring("a-different-vault-password"))
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
	got, err := DecryptAtPath(doc, "$.spec.config.registries[0].auth.pass", testKeyring)
	if err != nil {
		t.Fatalf("DecryptAtPath() error = %v", err)
	}
	old := readPath(t, got, ".spec.config.registries[0].auth.pass")
	return []byte(strings.Replace(string(got), old, value, 1))
}

// rekeyPassword is the password a rotation moves to, kept distinct from testPassword so that a
// test asserting a value moved cannot pass by accident.
const rekeyPassword = "a-new-vault-password"

// rekeyKeyring is rekeyPassword in keyring form, the counterpart to testKeyring.
var rekeyKeyring = NewVaultKeyring(rekeyPassword)

const passPath = "$.spec.config.registries[0].auth.pass"

func TestRekeyConfigMovesEveryCredential(t *testing.T) {
	doc := readVaultInventoryFixture(t)
	vaulted, _, _, err := EncryptConfig(doc, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	got, changed, _, err := RekeyConfig(vaulted, testKeyring, rekeyKeyring)
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

	// What the rotation is for: the new password reads every value, and the old one reads none of
	// them. A rekey that left even one value behind would be the half-rotated file this command
	// exists to avoid.
	for _, path := range want {
		value := readPath(t, got, path)
		if !cluster.IsVaultEncrypted(value) {
			t.Errorf("value at %s = %q, want ciphertext", path, value)
		}
		if _, err := DecryptValue(value, rekeyKeyring); err != nil {
			t.Errorf("value at %s does not decrypt with the new password: %v", path, err)
		}
		if _, err := DecryptValue(value, testKeyring); err == nil {
			t.Errorf("value at %s still decrypts with the old password", path)
		}
	}
}

// TestRekeyConfigResaltsUnderTheSamePassword covers the password-preserving case behind a "vault
// rekey" with no new password named: every value comes back as different ciphertext that the
// password the file already carried still reads.
func TestRekeyConfigResaltsUnderTheSamePassword(t *testing.T) {
	doc := readVaultInventoryFixture(t)
	vaulted, _, _, err := EncryptConfig(doc, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	got, changed, _, err := RekeyConfig(vaulted, testKeyring, testKeyring)
	if err != nil {
		t.Fatalf("RekeyConfig() error = %v", err)
	}
	if len(changed) != 4 {
		t.Fatalf("changed = %v, want all four credentials re-salted", changed)
	}

	for _, path := range changed {
		value := readPath(t, got, path)
		if value == readPath(t, vaulted, path) {
			t.Errorf("value at %s is byte-identical, want a fresh salt", path)
		}
		if _, err := DecryptValue(value, testKeyring); err != nil {
			t.Errorf("value at %s no longer decrypts with the password it was vaulted under: %v", path, err)
		}
	}

	// The plaintext is what it always was, which is the part a re-salt must not disturb.
	plain, _, _, err := DecryptConfig(got, testKeyring)
	if err != nil {
		t.Fatalf("DecryptConfig() error = %v", err)
	}
	if string(plain) != string(doc) {
		t.Errorf("a re-salt changed the document:\n%s\nwant:\n%s", plain, string(doc))
	}
}

// TestRekeyConfigDecryptsAtApplyTime is the check that matters most, as it is for encrypt-file:
// what a rotation writes has to be what an apply reads back, under the new password.
func TestRekeyConfigDecryptsAtApplyTime(t *testing.T) {
	doc := readVaultInventoryFixture(t)
	vaulted, _, _, err := EncryptConfig(doc, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	rekeyed, _, _, err := RekeyConfig(vaulted, testKeyring, rekeyKeyring)
	if err != nil {
		t.Fatalf("RekeyConfig() error = %v", err)
	}

	plain, _, _, err := DecryptConfig(rekeyed, rekeyKeyring)
	if err != nil {
		t.Fatalf("DecryptConfig() error = %v", err)
	}

	// Back to the document it started as, which is the strongest statement that nothing was lost
	// on the way through two encryptions.
	if string(plain) != string(doc) {
		t.Errorf("round trip through a rotation changed the document:\n%s\nwant:\n%s", plain, string(doc))
	}
}

func TestRekeyConfigPreservesEverythingElse(t *testing.T) {
	doc := readVaultInventoryFixture(t)
	vaulted, _, _, err := EncryptConfig(doc, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	got, _, _, err := RekeyConfig(vaulted, testKeyring, rekeyKeyring)
	if err != nil {
		t.Fatalf("RekeyConfig() error = %v", err)
	}

	if !strings.Contains(string(got), "  # registries the cluster pulls from") {
		t.Errorf("comment did not survive:\n%s", got)
	}
	if !strings.Contains(string(got), "pass: |- # rotate me") {
		t.Errorf("the trailing comment on the rekeyed value did not survive:\n%s", got)
	}
	if !strings.Contains(string(got), "          insecureSkipVerify: false") {
		t.Errorf("the key after the CA block did not survive:\n%s", got)
	}
	if value := readPath(t, got, ".spec.config.loadbalancer"); value != "lb.example.com" {
		t.Errorf("loadbalancer = %q, want it left alone", value)
	}
	if value := readPath(t, got, ".spec.config.registries[0].auth.token"); value != "" {
		t.Errorf("empty token = %q, want it left alone", value)
	}
}

// TestRekeyConfigSkipsPlaintext covers the file an operator has half-edited: a credential pasted
// back in the clear is not this command's business, and must come out the other side untouched
// rather than vaulted under the new password as a side effect of a rotation.
func TestRekeyConfigSkipsPlaintext(t *testing.T) {
	doc := readVaultInventoryFixture(t)
	vaulted, _, _, err := EncryptConfig(doc, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	edited := editPass(t, vaulted, "hunter3")

	got, changed, _, err := RekeyConfig(edited, testKeyring, rekeyKeyring)
	if err != nil {
		t.Fatalf("RekeyConfig() error = %v", err)
	}

	for _, path := range changed {
		if path == passPath {
			t.Errorf("changed = %v, want the plaintext pass left out", changed)
		}
	}
	if value := readPath(t, got, passPath); value != "hunter3" {
		t.Errorf("pass = %q, want the plaintext left as it was", value)
	}
}

func TestRekeyConfigRejectsTheWrongOldPassword(t *testing.T) {
	doc := readVaultInventoryFixture(t)
	vaulted, _, _, err := EncryptConfig(doc, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	got, _, _, err := RekeyConfig(vaulted, NewVaultKeyring("not-the-old-password"), rekeyKeyring)
	if err == nil {
		t.Fatal("RekeyConfig() error = nil, want an error")
	}
	if got != nil {
		t.Errorf("a failed run returned a document, which a caller might write:\n%s", got)
	}
}

// TestRekeyConfigRejectsAMixedFile is the hazard a rotation has to refuse rather than work around.
// A file already holding ciphertext under two passwords cannot be rekeyed into a working state,
// because an apply reads a registry's fields with one password, so the command stops and names the
// value it could not read.
func TestRekeyConfigRejectsAMixedFile(t *testing.T) {
	const otherPassword = "a-different-vault-password"

	doc := readVaultInventoryFixture(t)
	vaulted, _, _, err := EncryptConfig(doc, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	// Take one value back to plaintext and vault it again under a password nothing else uses.
	edited := editPass(t, vaulted, "hunter3")
	mixed, err := EncryptAtPath(edited, passPath, NewVaultKeyring(otherPassword), false)
	if err != nil {
		t.Fatalf("EncryptAtPath() error = %v", err)
	}

	got, _, _, err := RekeyConfig(mixed, testKeyring, rekeyKeyring)
	if err == nil {
		t.Fatal("RekeyConfig() error = nil, want a refusal to rekey a file vaulted under two passwords")
	}
	if got != nil {
		t.Errorf("a failed run returned a document, which a caller might write:\n%s", got)
	}
	for _, want := range []string{"auth.pass", "old vault password"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

func TestRekeyAtPathRejectsPlaintext(t *testing.T) {
	_, err := RekeyAtPath(readPathsInventoryFixture(t), passPath, testKeyring, rekeyKeyring)
	if !errors.Is(err, ErrNotEncrypted) {
		t.Fatalf("RekeyAtPath() error = %v, want ErrNotEncrypted", err)
	}
}

// TestRekeyAtPathRejectsADoubleWrappedValue covers what encrypt-path --force leaves behind. Rekeying
// it would move the outer layer to the new password and leave the inner one on the old, which is a
// value no single password can read back -- so it is refused rather than half done.
func TestRekeyAtPathRejectsADoubleWrappedValue(t *testing.T) {
	once, err := EncryptAtPath(readPathsInventoryFixture(t), passPath, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptAtPath() error = %v", err)
	}
	twice, err := EncryptAtPath(once, passPath, testKeyring, true)
	if err != nil {
		t.Fatalf("EncryptAtPath() error = %v", err)
	}

	if _, err := RekeyAtPath(twice, passPath, testKeyring, rekeyKeyring); !errors.Is(err, ErrWrappedTwice) {
		t.Fatalf("RekeyAtPath() error = %v, want ErrWrappedTwice", err)
	}
}

// TestRekeyAtPathCarriesAValueDecryptWouldRefuse is what separates rekeying in one step from a
// decrypt followed by an encrypt. A secret that is not valid UTF-8 has no faithful YAML
// representation, so DecryptAtPath refuses to write it back -- but a rotation never renders the
// plaintext as YAML, so it moves the value across without ever having to.
func TestRekeyAtPathCarriesAValueDecryptWouldRefuse(t *testing.T) {
	secret := string([]byte{0x00, 0xff, 0xfe})
	if utf8.ValidString(secret) {
		t.Fatal("the fixture is supposed to be invalid UTF-8")
	}

	encrypted, err := EncryptValue(secret, testKeyring)
	if err != nil {
		t.Fatalf("EncryptValue() error = %v", err)
	}
	node, _, _, err := scalarNodeAtPath(readPathsInventoryFixture(t), passPath)
	if err != nil {
		t.Fatalf("scalarNodeAtPath() error = %v", err)
	}
	doc, err := spliceCiphertext(readPathsInventoryFixture(t), node, encrypted)
	if err != nil {
		t.Fatalf("spliceCiphertext() error = %v", err)
	}

	// The premise: decrypting this value into the document is the thing that cannot be done.
	if _, err := DecryptAtPath(doc, passPath, testKeyring); err == nil {
		t.Fatal("DecryptAtPath() error = nil, want a refusal to write a non-UTF-8 value back")
	}

	got, err := RekeyAtPath(doc, passPath, testKeyring, rekeyKeyring)
	if err != nil {
		t.Fatalf("RekeyAtPath() error = %v", err)
	}

	plain, err := DecryptValue(readPath(t, got, passPath), rekeyKeyring)
	if err != nil {
		t.Fatalf("DecryptValue() error = %v", err)
	}
	if plain != secret {
		t.Errorf("plaintext = %q, want %q", plain, secret)
	}
}

// TestEncryptConfigRewritesFlowStyle covers the spelling that used to stop encrypt-file dead: a
// credential inside a "{a: b}". A literal block written there is not YAML, so the rewrite produced
// a document this package could not parse on its very next path -- and reported that as a parse
// error against the operator's file, at a line and column the file did not have.
func TestEncryptConfigRewritesFlowStyle(t *testing.T) {
	fixture := readFlowInventoryFixture(t)

	got, changed, _, err := EncryptConfig(fixture, testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	want := []string{
		"$.spec.config.registries[0].auth.user",
		"$.spec.config.registries[0].auth.pass",
		"$.spec.config.registries[1].auth.user",
		"$.spec.config.registries[1].tls.ca",
		"$.spec.config.registries[2].auth.token",
	}
	if strings.Join(changed, ",") != strings.Join(want, ",") {
		t.Fatalf("changed = %v, want %v", changed, want)
	}
	for _, plaintext := range []string{"flow-user", "flow-pass", "nested-user", "nested-ca", "whole-token"} {
		if strings.Contains(string(got), plaintext) {
			t.Errorf("the credential %q is still in the clear:\n%s", plaintext, got)
		}
	}

	// The keys beside the rewritten ones are still where they were, which is what carrying the rest
	// of the flow collection over means.
	for _, marker := range []string{"insecureSkipVerify: false", "name: whole.test.local"} {
		if !strings.Contains(string(got), marker) {
			t.Errorf("the marker %q did not survive:\n%s", marker, got)
		}
	}

	// The encrypted document still loads, which is the whole of what went wrong before.
	if _, err := Parse(t.Context(), got); err != nil {
		t.Fatalf("the encrypted document does not decode: %v\n%s", err, got)
	}

	back, _, _, err := DecryptConfig(got, testKeyring)
	if err != nil {
		t.Fatalf("DecryptConfig() error = %v", err)
	}
	dis, err := Parse(t.Context(), back)
	if err != nil {
		t.Fatalf("the decrypted document does not decode: %v\n%s", err, back)
	}
	registries := dis.Spec.Config.Registries
	if len(registries) != 3 {
		t.Fatalf("registries = %d, want 3", len(registries))
	}
	for i, want := range []struct{ user, pass, token, ca string }{
		{user: "flow-user", pass: "flow-pass"},
		{user: "nested-user", ca: "nested-ca"},
		{token: "whole-token"},
	} {
		auth := registries[i].Authentication
		if auth.Username != want.user || auth.Password != want.pass || auth.Token != want.token {
			t.Errorf("registry %d auth = %+v, want user=%q pass=%q token=%q", i, auth, want.user, want.pass, want.token)
		}
		ca := ""
		if registries[i].TLS != nil {
			ca = registries[i].TLS.CA
		}
		if ca != want.ca {
			t.Errorf("registry %d ca = %q, want %q", i, ca, want.ca)
		}
	}
}

// TestEncryptAtPathKeepsFlowStyleOnOneLine pins how the value is written, which is the part that
// makes the document still parse: one quoted scalar, and whatever shared the collection with it
// still on the same line.
func TestEncryptAtPathKeepsFlowStyleOnOneLine(t *testing.T) {
	fixture := readFlowInventoryFixture(t)

	got, err := EncryptAtPath(fixture, ".spec.config.registries[0].auth.pass", testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptAtPath() error = %v", err)
	}

	line := ""
	for _, l := range strings.Split(string(got), "\n") {
		if strings.Contains(l, "user: flow-user") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("the flow mapping is gone:\n%s", got)
	}
	if !strings.HasPrefix(strings.TrimSpace(line), "auth: {user: flow-user, pass: \"$ANSIBLE_VAULT;1.1;AES256\\n") {
		t.Errorf("flow line = %q, want the pass quoted onto the same line", line)
	}
	if !strings.HasSuffix(line, "}") {
		t.Errorf("flow line = %q, want it to still close the mapping", line)
	}
}
