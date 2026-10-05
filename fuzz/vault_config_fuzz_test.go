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

	"github.com/colonel-byte/cargoship/internal/clustercfg"
)

// FuzzEncryptDecryptConfigNoPanic asserts that EncryptConfig, DecryptConfig and RekeyConfig never
// panic on arbitrary documents.
//
// Every other vault target in this package operates on one credential at a known path.
// EncryptConfig and its siblings instead auto-discover every credential path in the document --
// walking the registries list, formatting each as "spec.config.registries[N].auth.pass" and the
// like, and probing whether a value already there is ciphertext -- which is logic none of the
// path-scoped targets exercise. That discovery runs over a whole configuration file before any of
// its other validation, so what it meets is whatever shape the file happens to be in.
func FuzzEncryptDecryptConfigNoPanic(f *testing.F) {
	f.Add(string(docWithPlaintext("hunter2")))
	f.Add(string(docWithPlaintext("")))
	f.Add("apiVersion: zarf.dev/v1alpha1\nkind: ZarfCluster\n")
	f.Add("")
	f.Add("spec:\n  config:\n    registries: not-a-list\n")
	f.Add("spec:\n  config:\n    registries:\n      - auth: {pass: hunter2}\n")
	f.Add("spec:\n  config:\n    registries:\n      - {}\n      - {}\n      - {}\n")
	f.Add("spec: &x\n  config: *x\n")
	f.Add("not yaml: [unterminated")
	f.Add("- just\n- a\n- list\n")

	f.Fuzz(func(_ *testing.T, document string) {
		src := []byte(document)

		for _, keyring := range encryptKeyrings() {
			encrypted, _, _, err := clustercfg.EncryptConfig(src, keyring.ring, false)
			if err != nil {
				continue
			}
			_, _, _, _ = clustercfg.DecryptConfig(encrypted, keyring.ring) //nolint:errcheck // no-panic fuzz test, malformed input is expected to error
		}

		for _, pair := range rekeyPairs() {
			_, _, _, _ = clustercfg.RekeyConfig(src, pair.from, pair.to) //nolint:errcheck // no-panic fuzz test, malformed input is expected to error
		}
	})
}
