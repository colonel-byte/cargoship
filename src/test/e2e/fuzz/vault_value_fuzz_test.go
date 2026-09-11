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
	"testing"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/internal/clustercfg"
	"github.com/stretchr/testify/require"
)

// FuzzEncryptValueRoundTrip asserts that any value EncryptValue accepts comes back byte-identical
// from DecryptValue. That is the promise the pair makes to an operator vaulting a credential, and
// it is the property most likely to break quietly: a value is written once and read back much
// later, so a byte lost in the round trip surfaces as a login failure on a host rather than as an
// error at the time it was stored.
func FuzzEncryptValueRoundTrip(f *testing.F) {
	// The seeds name the shapes a credential actually takes, plus the ones most likely to be
	// mishandled: an empty value, whitespace a trim would eat, a PEM block, control characters,
	// and bytes that are not UTF-8.
	f.Add("hunter2")
	f.Add("")
	f.Add("-----BEGIN CERTIFICATE-----\naGVsbG8gd29ybGQ=\n-----END CERTIFICATE-----\n")
	f.Add("trailing space \nsecond line")
	f.Add("tab\tand\rcarriage return")
	f.Add("control\x01character")
	f.Add("héllo wörld \U0001f680")
	f.Add("\xff\xfe not valid utf-8")

	f.Fuzz(func(t *testing.T, value string) {
		encrypted, err := clustercfg.EncryptValue(value, fuzzPassword)
		require.NoError(t, err)

		decrypted, err := clustercfg.DecryptValue(encrypted, fuzzPassword)
		require.NoError(t, err)
		require.Equal(t, value, decrypted, "value did not survive the round trip")
	})
}

// FuzzDecryptValueRejectsGarbage asserts that no input makes DecryptValue do anything worse than
// return an error. The ciphertext it parses is handled by a third-party library and comes from a
// file an operator edited by hand, so malformed input is the expected case rather than the exotic
// one: a truncated paste or a stray edit has to produce an error the operator can read.
//
// A panic fails the target on its own. What the body states is the one thing a caller relies on
// when the call succeeds: DecryptValue reports ErrNotEncrypted for anything without the vault
// header, so a value it decrypted has to have carried one.
func FuzzDecryptValueRejectsGarbage(f *testing.F) {
	valid, err := clustercfg.EncryptValue("hunter2", fuzzPassword)
	require.NoError(f, err)

	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	f.Add("$ANSIBLE_VAULT;1.1;AES256\n3336")
	f.Add("$ANSIBLE_VAULT;1.1;AES256\n")
	f.Add("$ANSIBLE_VAULT;9.9;ROT13\n61626364")
	f.Add("$ANSIBLE_VAULT;1.1;AES256\nnot hexadecimal at all")
	f.Add("not ciphertext")
	f.Add("")

	f.Fuzz(func(t *testing.T, blob string) {
		if _, err := clustercfg.DecryptValue(blob, fuzzPassword); err == nil {
			require.True(t, cluster.IsVaultEncrypted(blob), "decrypted a value carrying no vault header: %q", blob)
		}
	})
}
