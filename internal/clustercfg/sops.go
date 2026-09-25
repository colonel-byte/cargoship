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
	"fmt"

	"github.com/getsops/sops/v3/decrypt"
	goyaml "github.com/goccy/go-yaml"
)

// IsSopsEncrypted reports whether b is a whole-file sops document rather than a plain cluster
// configuration, by checking for the top-level "sops" metadata key sops appends -- the same signal
// every other sops-consuming tool (Flux, helm-secrets) uses to tell the two apart.
//
// Malformed YAML reports false rather than an error, so that the caller's own parse produces the
// error the operator actually needs to see.
func IsSopsEncrypted(b []byte) bool {
	var doc map[string]any
	if err := goyaml.Unmarshal(b, &doc); err != nil {
		return false
	}
	_, ok := doc["sops"]
	return ok
}

// DecryptSops decrypts a whole-file sops document, returning the cleartext cluster configuration
// sops's own metadata block described.
//
// Only decrypt.Data is called here: it is the one package sops guarantees API stability for.
// Everything else -- which key type decrypts it, where that key comes from (SOPS_AGE_KEY_FILE, a
// KMS credential chain, ...) -- is sops's own decision, not cargoship's, so there is no keyring or
// flag surface here to mirror the vault/age one.
func DecryptSops(b []byte) ([]byte, error) {
	cleartext, err := decrypt.Data(b, "yaml")
	if err != nil {
		return nil, fmt.Errorf("decrypting sops document: %w", err)
	}
	return cleartext, nil
}
