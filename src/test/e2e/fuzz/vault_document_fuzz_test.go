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
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/internal/clustercfg"
	"github.com/stretchr/testify/require"
)

// FuzzSpliceAcrossDocumentShapes fuzzes the document around the credential rather than the
// credential itself.
//
// Every other document-level target writes into one fixed template, so the splice always meets the
// same indentation, the same block style and the same line endings. Those are the inputs to the
// offset arithmetic: the column a block scalar's continuation lines are written at is derived from
// the key's column, the end of the value being replaced is found by scanning lines, and a value
// inside a flow mapping has to be quoted onto one line because YAML has no block scalar there.
// A document an operator hand-wrote at one space of indentation, or one a Windows editor saved
// with CRLF, or one whose auth block is aliased somewhere else in the file, reaches arithmetic the
// fixed template never does.
//
// The shape knobs are fuzzed alongside the value so that a value which only breaks at a particular
// indentation is reachable, which it is not when the two are tested separately.
//
// Line endings are deliberately not among the knobs. A document saved with CRLF is one the splice
// mishandles today -- it refuses a multi-line value outright, and writes bare line feeds into the
// document when it does splice one -- so fuzzing that axis would report the same known defect on
// every input rather than finding a new one. Put the knob back once the splice carries the
// document's own line ending.
func FuzzSpliceAcrossDocumentShapes(f *testing.F) {
	f.Add("hunter2", uint8(2), false, false)
	f.Add("hunter2", uint8(1), false, false)
	f.Add("hunter2", uint8(4), false, false)
	f.Add("hunter2", uint8(2), true, false)
	f.Add("hunter2", uint8(2), false, true)
	f.Add(caPEM, uint8(3), false, true)
	f.Add("trailing space  \nsecond line", uint8(1), false, false)
	f.Add("looks: like a mapping", uint8(2), true, false)
	f.Add("control\x01character", uint8(2), true, true)
	f.Add("", uint8(1), true, true)

	f.Fuzz(func(t *testing.T, value string, indent uint8, flow, anchored bool) {
		if !utf8.ValidString(value) {
			// Covered by FuzzDecryptAtPathRoundTrip, which states what the refusal has to be. Here
			// it would only stop every shape at the same first step.
			return
		}
		shape := documentShape{indent: int(indent%4) + 1, flow: flow, anchored: anchored}

		for _, keyring := range encryptKeyrings() {
			encrypted, err := clustercfg.EncryptValue(value, keyring.ring)
			require.NoError(t, err, "%s", keyring.name)

			doc := shape.documentHolding(encrypted)
			decrypted, err := clustercfg.DecryptAtPath(doc, passPath, keyring.ring)
			require.NoError(t, err, "%s %s\ndocument:\n%s", keyring.name, shape, doc)

			require.Equal(t, value, credentialIn(t, decrypted),
				"%s %s: value did not survive the round trip\ndocument:\n%s", keyring.name, shape, decrypted)
			shape.requireNeighboursIntact(t, decrypted)

			if cluster.IsEncrypted(value) {
				continue
			}
			reencrypted, err := clustercfg.EncryptAtPath(decrypted, passPath, keyring.ring, false)
			require.NoError(t, err, "%s %s\ndocument:\n%s", keyring.name, shape, decrypted)
			again, err := clustercfg.DecryptAtPath(reencrypted, passPath, keyring.ring)
			require.NoError(t, err, "%s %s", keyring.name, shape)
			require.Equal(t, value, credentialIn(t, again),
				"%s %s: value changed on the second round trip\ndocument:\n%s", keyring.name, shape, again)
			shape.requireNeighboursIntact(t, again)
		}
	})
}

// FuzzEncryptAtPathArbitraryDocument feeds whole documents to the rewrite rather than values.
//
// The parse in front of the splice is go-yaml's, plus this package's own anchor resolution, and
// both run on a file an operator wrote by hand. What this states is that no such file gets a worse
// answer than an error: a malformed document, an alias with no anchor, a merge key pointing at a
// scalar, or a path that lands on a mapping rather than a scalar all have to be reported rather
// than panicked on or half-written.
//
// When the rewrite does succeed, the property asserted is that the document settles. Encrypting
// and decrypting once may legitimately change bytes -- the value comes back in whichever scalar
// style renderScalar chose, which need not be the style the operator typed -- but doing it a second
// time may not change anything further. A splice that drifts, eats a neighbouring byte, or
// re-indents a block a little more each pass fails here, and that is the failure a single round
// trip is blind to.
func FuzzEncryptAtPathArbitraryDocument(f *testing.F) {
	f.Add(string(docWithPlaintext("hunter2")))
	f.Add(string(documentShape{indent: 1}.documentHolding("PLACEHOLDER")))
	f.Add(string(documentShape{indent: 2, flow: true}.documentHolding("PLACEHOLDER")))
	f.Add(string(documentShape{indent: 2, anchored: true}.documentHolding("PLACEHOLDER")))
	f.Add("spec:\n  config:\n    registries:\n      - name: harbor\n        auth:\n          pass: hunter2\n")
	f.Add("spec:\n  config:\n    registries:\n      - name: harbor\n        auth:\n          pass:\n            nested: mapping\n")
	f.Add("spec: &a\n  config: *a\n")
	f.Add("spec:\n  config:\n    defaults: &d\n      pass: hunter2\n    registries:\n      - name: harbor\n        auth:\n          <<: *d\n")
	f.Add("- not a mapping at all\n")
	f.Add("\t tabs are not valid indentation\n")
	f.Add("")

	f.Fuzz(func(t *testing.T, document string) {
		src := []byte(document)

		encrypted, err := clustercfg.EncryptAtPath(src, passPath, vaultKeyring, false)
		if err != nil {
			return
		}
		require.True(t, cluster.IsEncrypted(rawCredentialIn(t, encrypted)),
			"the rewritten value carries no ciphertext header\ndocument:\n%s", encrypted)

		once, err := clustercfg.DecryptAtPath(encrypted, passPath, vaultKeyring)
		require.NoError(t, err, "a document written by EncryptAtPath cannot be decrypted\ndocument:\n%s", encrypted)

		reencrypted, err := clustercfg.EncryptAtPath(once, passPath, vaultKeyring, false)
		require.NoError(t, err, "a document written by DecryptAtPath cannot be encrypted again\ndocument:\n%s", once)
		twice, err := clustercfg.DecryptAtPath(reencrypted, passPath, vaultKeyring)
		require.NoError(t, err, "document:\n%s", reencrypted)

		require.Equal(t, string(once), string(twice),
			"the document does not settle: a second round trip changed it again\nfirst:\n%s\nsecond:\n%s", once, twice)
	})
}

// FuzzEncryptAtPathArbitraryPath fuzzes the path against a real document instead of against the
// parser alone.
//
// FuzzPathIsDecryptable covers what a path means; this covers what happens when one is used. The
// path an operator types reaches go-yaml's path parser, which reads past the end of an unterminated
// index -- "registries[0" -- and panics rather than reporting the syntax error, so the recover in
// parseYAMLPath is load-bearing and a fuzzer is the only thing that finds the next input like it.
//
// The property beyond "does not panic" is that a path which successfully rewrites a value is one
// that can be used again: the canonical spelling has to reach the same value, because that is the
// spelling every error message and every comparison between two paths uses.
func FuzzEncryptAtPathArbitraryPath(f *testing.F) {
	f.Add(passPath)
	f.Add(".spec.config.registries[0].auth.pass")
	f.Add("spec.config.registries[0].auth.pass")
	f.Add("$.spec.config.registries[0].auth")
	f.Add("$.spec.config.registries[0")
	f.Add("$.spec.config.registries[*].auth.pass")
	f.Add("$.spec['config'].registries[0].auth.pass")
	f.Add("$.spec.config.registries[999].auth.pass")
	f.Add("$..pass")
	f.Add("$")
	f.Add("")

	f.Fuzz(func(t *testing.T, yamlPath string) {
		src := docWithPlaintext("hunter2")

		encrypted, err := clustercfg.EncryptAtPath(src, yamlPath, vaultKeyring, false)
		if err != nil {
			return
		}

		// A path that rewrote a value has to canonicalise, because every message about that value
		// quotes the canonical spelling rather than the one that was typed.
		canonical, err := clustercfg.CanonicalYAMLPath(yamlPath)
		require.NoError(t, err, "%q rewrote a value but does not canonicalise", yamlPath)

		decrypted, err := clustercfg.DecryptAtPath(encrypted, canonical, vaultKeyring)
		require.NoError(t, err, "%q rewrote a value that its canonical spelling %q does not reach", yamlPath, canonical)
		require.Equal(t, "hunter2", credentialIn(t, decrypted),
			"%q did not rewrite the value it read\ndocument:\n%s", yamlPath, decrypted)
	})
}

// documentShape is the set of choices a hand-written configuration makes that the splice has to
// cope with. Everything here is legal YAML that an operator's file can actually look like.
type documentShape struct {
	// indent is the number of spaces one level of nesting adds, from 1 to 4. YAML puts no lower
	// bound on it, and the block scalar holding ciphertext is written relative to it.
	indent int
	// flow writes the auth mapping as "{user: admin, pass: ...}". A value there cannot be a block
	// scalar, so the splice has to quote it onto one line instead.
	flow bool
	// anchored gives the auth mapping an anchor and aliases it from a second registry, so that the
	// node being rewritten is one this package's anchor resolution has already visited.
	anchored bool
}

// String names the shape for a failure message.
func (s documentShape) String() string {
	parts := []string{"indent=" + strconv.Itoa(s.indent)}
	for _, flag := range []struct {
		name string
		set  bool
	}{{"flow", s.flow}, {"anchored", s.anchored}} {
		if flag.set {
			parts = append(parts, flag.name)
		}
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// documentHolding returns a configuration in this shape with value written at passPath.
//
// The value is written as a literal block scalar, or quoted onto one line in a flow mapping, which
// is what EncryptAtPath itself would have produced. Writing it here rather than calling the tool is
// what lets the target start from ciphertext the tool has not touched.
func (s documentShape) documentHolding(value string) []byte {
	unit := strings.Repeat(" ", s.indent)
	// A sequence entry's keys line up two columns past the dash, whatever the indent unit is.
	item := strings.Repeat(unit, 3) + "  "

	var b strings.Builder
	b.WriteString("apiVersion: zarf.dev/v1alpha1\n")
	b.WriteString("kind: ZarfCluster\n")
	b.WriteString("metadata:\n")
	b.WriteString(unit + "name: fuzz\n")
	b.WriteString("spec:\n")
	b.WriteString(unit + "# keep this comment exactly where it is\n")
	b.WriteString(unit + "config:\n")
	b.WriteString(strings.Repeat(unit, 2) + "loadbalancer: lb.example.com\n")
	b.WriteString(strings.Repeat(unit, 2) + "registries:\n")
	b.WriteString(strings.Repeat(unit, 3) + "- name: harbor # a trailing comment\n")

	anchor := ""
	if s.anchored {
		anchor = " &creds"
	}
	if s.flow {
		b.WriteString(item + "auth:" + anchor + " {user: admin, pass: " + quoteForFlow(value) + ", token: \"tok #1\"}\n")
	} else {
		b.WriteString(item + "auth:" + anchor + "\n")
		b.WriteString(item + unit + "user: admin\n")
		b.WriteString(item + unit + "pass: " + blockScalar(value, item+unit+unit, "-") + "\n")
		b.WriteString(item + unit + "token: \"tok #1\"\n")
	}

	b.WriteString(item + "tls:\n")
	b.WriteString(item + unit + "ca: " + blockScalar(caPEM, item+unit+unit, "") + "\n")
	b.WriteString(item + unit + "insecureSkipVerify: false\n")
	if s.anchored {
		b.WriteString(strings.Repeat(unit, 3) + "- name: mirror\n")
		b.WriteString(item + "auth: *creds\n")
	}
	b.WriteString(unit + "hosts:\n")
	b.WriteString(strings.Repeat(unit, 2) + "- name: node1\n")

	return []byte(b.String())
}

// requireNeighboursIntact checks the parts of a shaped document the rewrite had no business
// touching. It is documentShape's own because the aliased registry only exists in some shapes, and
// because the comments have to still be where they were whatever the indentation was.
func (s documentShape) requireNeighboursIntact(t *testing.T, doc []byte) {
	t.Helper()

	parsed := decode(t, doc)
	registry := parsed.Spec.Config.Registries[0]
	require.Equal(t, "lb.example.com", parsed.Spec.Config.LoadBalancer, "%s", s)
	require.EqualValues(t, "harbor", registry.Name, "%s", s)
	require.Equal(t, "admin", registry.Authentication.Username, "%s", s)
	require.Equal(t, "tok #1", registry.Authentication.Token, "%s", s)
	require.NotNil(t, registry.TLS, "%s", s)
	require.Equal(t, caPEM, registry.TLS.CA, "%s", s)
	require.False(t, registry.TLS.InsecureSkipVerify, "%s", s)

	text := string(doc)
	require.Contains(t, text, "# keep this comment exactly where it is", "%s", s)
	require.Contains(t, text, "- name: harbor # a trailing comment", "%s", s)
	require.Contains(t, text, "- name: node1", "%s", s)
}

// blockScalar renders value as a literal block scalar whose continuation lines sit at indent.
//
// chomp is the indicator that follows the "|": "-" strips the trailing newline, which is what
// EncryptAtPath writes for ciphertext, and "" keeps one, which is how a PEM certificate is stored.
// A value with no line break still goes through a block, because that is the shape ciphertext takes
// and the shape the splice has to measure.
func blockScalar(value, indent, chomp string) string {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	return "|" + chomp + "\n" + indent + strings.Join(lines, "\n"+indent)
}

// quoteForFlow renders value as a double-quoted YAML scalar for use inside a flow mapping, where a
// block scalar is not available. Go's quoting escapes the same characters YAML does for the inputs
// that reach here -- ciphertext, which is ASCII and line breaks.
func quoteForFlow(value string) string {
	return strconv.Quote(value)
}

// docWithPlaintext returns the fixed template holding value as a plain scalar, for the targets that
// start from a document nothing has encrypted yet.
func docWithPlaintext(value string) []byte {
	return documentShape{indent: 2}.documentHolding(value)
}

// rawCredentialIn returns the credential at passPath without asserting anything about it, for the
// targets that need to look at a value the document may hold in either state.
func rawCredentialIn(t *testing.T, doc []byte) string {
	t.Helper()

	return decode(t, doc).Spec.Config.Registries[0].Authentication.Password
}
