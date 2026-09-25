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

package fuzz

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/colonel-byte/cargoship/internal/clustercfg"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	goyaml "github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

// passPath is the credential the document-level targets rewrite, and docTemplate is the document
// they rewrite it in. The template carries the neighbours a splice could damage: a comment on its
// own line and one trailing a value, a quoted scalar holding a "#", a multi-line block after the
// value being replaced, and a sequence after that.
const passPath = "$.spec.config.registries[0].auth.pass"

const docTemplate = `apiVersion: zarf.dev/v1alpha1
kind: ZarfCluster
metadata:
  name: fuzz
spec:
  # keep this comment exactly where it is
  config:
    loadbalancer: lb.example.com
    registries:
      - name: harbor # a trailing comment
        auth:
          user: admin
          pass: %s
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

// passIndent is the column the block scalar under "pass:" is written at, which is two deeper than
// the key. It has to match docTemplate.
const passIndent = "            "

// caPEM is the certificate docTemplate holds next to the credential, as the YAML decoder returns
// it. A splice that miscounts the end of the value it replaces eats into this block, so reading it
// back unchanged is the cheapest evidence the rewrite stayed inside its own span.
const caPEM = "-----BEGIN CERTIFICATE-----\naGVsbG8gd29ybGQ=\n-----END CERTIFICATE-----\n"

// FuzzDecryptAtPathRoundTrip asserts that a credential written into a configuration reads back
// unchanged, whatever it holds, and that nothing else in the document moves.
//
// This is where the encoding decisions live. DecryptAtPath has to choose a scalar style that holds
// the plaintext faithfully -- plain, literal block, or quoted -- and then splice it in by byte
// offset. Both halves are easy to get right for a password and hard to get right for a value with
// a trailing space, an embedded "#", a line that looks like a YAML key, or a control character.
// A wrong choice produces a document that still parses, which is why a table test can miss it and
// an operator finds out when the credential reaches a host mangled.
//
// Running both formats through the same body is what makes the ciphertext's own shape part of the
// input. Ansible Vault is one header line and hexadecimal; age is armor whose lines are base64 and
// whose last byte is a newline the splice has to strip. The plaintext side is identical, so any
// difference in the result is the splice reacting to the ciphertext rather than to the value.
func FuzzDecryptAtPathRoundTrip(f *testing.F) {
	f.Add("hunter2")
	f.Add("")
	f.Add(" leading and trailing ")
	f.Add(caPEM)
	f.Add("trailing space \nsecond line")
	f.Add("looks: like a mapping")
	f.Add("- looks like a sequence")
	f.Add("# looks like a comment")
	f.Add("123")
	f.Add("true")
	f.Add("null")
	f.Add("control\x01character")
	f.Add("tab\tand\rcarriage return")
	f.Add("h\u00e9llo w\u00f6rld \U0001f680")
	f.Add("\xff\xfe not valid utf-8")

	f.Fuzz(func(t *testing.T, value string) {
		for _, keyring := range encryptKeyrings() {
			encrypted, err := clustercfg.EncryptValue(value, keyring.ring)
			require.NoError(t, err, "%s", keyring.name)

			decrypted, err := clustercfg.DecryptAtPath(docWithCiphertext(encrypted), passPath, keyring.ring)
			if !utf8.ValidString(value) {
				// A YAML scalar holds text, so a value that is not UTF-8 is refused rather than
				// written back as something that no longer decodes to what went in.
				require.Error(t, err, "%s wrote invalid UTF-8 back into the document", keyring.name)
				continue
			}
			require.NoError(t, err, "%s", keyring.name)

			require.Equal(t, value, credentialIn(t, decrypted), "%s: value did not survive the round trip", keyring.name)
			requireNeighboursIntact(t, decrypted)

			// Encrypting the rewritten document and decrypting it again has to land on the same
			// value: the style DecryptAtPath chose must be one the tool can read back, not merely
			// one that parses. A value that is itself ciphertext is skipped because re-encrypting
			// it is refused by design, which is what ErrAlreadyEncrypted says. Either format's
			// header counts -- a keyring reads whichever one a document holds.
			if cluster.IsEncrypted(value) {
				continue
			}
			reencrypted, err := clustercfg.EncryptAtPath(decrypted, passPath, keyring.ring, false)
			require.NoError(t, err, "%s: a document written by DecryptAtPath cannot be encrypted again", keyring.name)
			again, err := clustercfg.DecryptAtPath(reencrypted, passPath, keyring.ring)
			require.NoError(t, err, "%s", keyring.name)
			require.Equal(t, value, credentialIn(t, again), "%s: value changed on the second round trip", keyring.name)
			requireNeighboursIntact(t, again)
		}
	})
}

// FuzzRekeyAtPathPreservesValue asserts that rekeying moves a credential onto a new key without
// touching the plaintext or the rest of the document -- including when the new key is in the other
// format, which is how a configuration migrates from Ansible Vault to age.
//
// The crossing cases are the reason this fuzzes all four combinations rather than one. A migration
// decrypts with one library and encrypts with the other in a single pass, and the ciphertext that
// comes out is a different length and shape from the one that went in, so the splice is being asked
// to replace a block with an unlike block -- the case where an offset that is off by one produces a
// document that still parses.
//
// Rekeying never renders the plaintext back into YAML, so it accepts values DecryptAtPath refuses
// -- one that is not UTF-8 rekeys without trouble. That difference is why this is its own target
// rather than a case in the one above.
func FuzzRekeyAtPathPreservesValue(f *testing.F) {
	f.Add("hunter2")
	f.Add("")
	f.Add(caPEM)
	f.Add("\xff\xfe not valid utf-8")
	f.Add("control\x01character")

	f.Fuzz(func(t *testing.T, value string) {
		for _, pair := range rekeyPairs() {
			encrypted, err := clustercfg.EncryptValue(value, pair.from)
			require.NoError(t, err, "%s", pair.name)

			rekeyed, err := clustercfg.RekeyAtPath(docWithCiphertext(encrypted), passPath, pair.from, pair.to)
			if cluster.IsEncrypted(value) {
				// A value whose plaintext is itself ciphertext cannot be rekeyed: the outer layer
				// would move to the new key and the inner one would not, leaving a value no single
				// key reads back.
				require.ErrorIs(t, err, clustercfg.ErrWrappedTwice, "%s", pair.name)
				continue
			}
			require.NoError(t, err, "%s", pair.name)
			requireNeighboursIntact(t, rekeyed)

			ciphertext := credentialIn(t, rekeyed)
			plain, err := clustercfg.DecryptValue(ciphertext, pair.to)
			require.NoError(t, err, "%s: the new key does not read the rekeyed value", pair.name)
			require.Equal(t, value, plain, "%s: rekeying changed the plaintext", pair.name)

			// Rotating onto the same vault password re-salts rather than rotates, so the old key
			// still reads the result and there is nothing further to assert.
			if pair.from == pair.to {
				continue
			}
			_, err = clustercfg.DecryptValue(ciphertext, pair.from)
			require.Error(t, err, "%s: the old key still reads the rekeyed value", pair.name)
		}
	})
}

// FuzzPathIsDecryptable asserts that every path the encryptor accepts is one the apply-time
// decryptor reads.
//
// PathIsDecryptable is what warns an operator that `vault encrypt-path` is about to vault a value
// nothing unwraps. If it accepts a path DecryptRegistryAuth never visits, the credential reaches
// the host as ciphertext and the warning that should have said so never fires -- so the invariant
// worth fuzzing is that an accepted path always canonicalises to one under the registries list and
// stays accepted in that canonical spelling.
func FuzzPathIsDecryptable(f *testing.F) {
	f.Add(passPath)
	f.Add(".spec.config.registries[0].auth.pass")
	f.Add("spec.config.registries[10].tls.ca")
	f.Add("$.spec.config.registries[0].auth.pass.extra")
	f.Add("$.spec.config.registries[*].auth.pass")
	f.Add("$.spec.config.loadbalancer")
	f.Add("$.spec.config.registries[0].auth.passx")
	f.Add("$.spec['config'].registries[0].auth.pass")
	f.Add("$.spec.config.registries[-1].auth.pass")
	f.Add("$.spec.config.registries[0")
	f.Add("")
	f.Add("$")

	f.Fuzz(func(t *testing.T, path string) {
		canonical, err := clustercfg.CanonicalYAMLPath(path)
		if err != nil {
			require.False(t, clustercfg.PathIsDecryptable(path), "a path that does not parse was accepted")
			return
		}

		// Canonicalising is how a caller tells whether two spellings name the same value, which
		// only works if a second pass is a no-op.
		again, err := clustercfg.CanonicalYAMLPath(canonical)
		require.NoError(t, err, "%q canonicalises to %q, which does not parse", path, canonical)
		require.Equal(t, canonical, again, "canonicalising %q is not idempotent", path)

		if !clustercfg.PathIsDecryptable(path) {
			return
		}
		require.True(t, strings.HasPrefix(canonical, "$.spec.config.registries["),
			"an accepted path points outside the registries list: %q", canonical)
		require.True(t, clustercfg.PathIsDecryptable(canonical),
			"a path is accepted as %q but rejected as %q", path, canonical)
	})
}

// FuzzCanonicalYAMLPathNoPanic asserts that CanonicalYAMLPath never panics on arbitrary strings,
// and that a path it accepts is idempotent under a second pass.
//
// The other targets in this file feed CanonicalYAMLPath paths shaped like a YAML path an operator
// might type. This one feeds it arbitrary text, because the recover in parseYAMLPath exists for
// exactly the input this misses: go-yaml's own parser reads past the end of an unterminated index
// and panics rather than reporting a syntax error, and nothing about that bug is specific to
// path-shaped strings.
func FuzzCanonicalYAMLPathNoPanic(f *testing.F) {
	f.Add("$.spec.config.registries[0")
	f.Add("[[[[[[[[[[")
	f.Add("]]]]]]]]]]")
	f.Add("$['")
	f.Add("$.a[")
	f.Add("$..")
	f.Add(strings.Repeat("[0]", 200))
	f.Add("\x00\x01\x02")
	f.Add("")
	f.Add("not a path at all, just text")

	f.Fuzz(func(t *testing.T, path string) {
		canonical, err := clustercfg.CanonicalYAMLPath(path)
		if err != nil {
			return
		}
		again, err := clustercfg.CanonicalYAMLPath(canonical)
		require.NoError(t, err, "%q canonicalises to %q, which does not parse", path, canonical)
		require.Equal(t, canonical, again, "canonicalising %q is not idempotent", path)
	})
}

// docWithCiphertext returns docTemplate with encrypted written under "pass:" as a literal block
// scalar, which is the form EncryptAtPath stores ciphertext in.
//
// The splice inside clustercfg is unexported, so this restates the part of it that matters: strip
// any trailing newline the vault library left, then indent each line to passIndent. Ciphertext is
// a header line followed by hexadecimal, so there is nothing in it a block scalar cannot carry.
func docWithCiphertext(encrypted string) []byte {
	lines := strings.Split(strings.TrimRight(encrypted, "\n"), "\n")
	block := "|-\n" + passIndent + strings.Join(lines, "\n"+passIndent)

	return fmt.Appendf(nil, docTemplate, block)
}

// credentialIn returns the credential at passPath as the configuration decoder hands it back,
// which is the route an apply takes to read it. Decoding the whole document rather than reading
// the one path also means a splice that damaged the document fails here rather than going unseen.
func credentialIn(t *testing.T, doc []byte) string {
	t.Helper()

	return decode(t, doc).Spec.Config.Registries[0].Authentication.Password
}

// decode unmarshals doc into the configuration type an apply uses.
func decode(t *testing.T, doc []byte) cluster.ZarfCluster {
	t.Helper()

	var parsed cluster.ZarfCluster
	require.NoError(t, goyaml.Unmarshal(doc, &parsed), "the rewritten document does not decode:\n%s", doc)

	return parsed
}

// requireNeighboursIntact checks the parts of the document the rewrite had no business touching:
// the values around the one being replaced, and the comments, which no YAML round trip preserves
// and which only a byte-level splice keeps where they were.
func requireNeighboursIntact(t *testing.T, doc []byte) {
	t.Helper()

	parsed := decode(t, doc)
	registry := parsed.Spec.Config.Registries[0]
	require.Equal(t, "lb.example.com", parsed.Spec.Config.LoadBalancer)
	require.EqualValues(t, "harbor", registry.Name)
	require.Equal(t, "admin", registry.Authentication.Username)
	require.Equal(t, "tok #1", registry.Authentication.Token)
	require.NotNil(t, registry.TLS)
	require.Equal(t, caPEM, registry.TLS.CA)
	require.False(t, registry.TLS.InsecureSkipVerify)

	text := string(doc)
	require.Contains(t, text, "# keep this comment exactly where it is")
	require.Contains(t, text, "- name: harbor # a trailing comment")
	require.Contains(t, text, "    - name: node1")
}
