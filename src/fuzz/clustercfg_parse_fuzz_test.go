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
	"context"
	"testing"

	"github.com/colonel-byte/cargoship/internal/clustercfg"
)

// FuzzClustercfgParseNoPanic asserts that clustercfg.Parse, the entry point a cluster
// configuration file is read through before any vault operation ever runs, never panics on
// arbitrary bytes.
//
// The other targets in this package build well-formed documents by hand and mutate one credential
// inside them; this is the one that hands the decoder bytes with no assumed structure at all,
// which is what the file actually is before an operator's edits or a vault operation have been
// proven to leave it well-formed.
func FuzzClustercfgParseNoPanic(f *testing.F) {
	f.Add([]byte(docTemplate))
	f.Add([]byte(""))
	f.Add([]byte("not yaml: [unterminated"))
	f.Add([]byte("apiVersion: zarf.dev/v1alpha1\nkind: ZarfCluster\n"))
	f.Add([]byte("- just\n- a\n- list\n"))
	f.Add([]byte("just a scalar"))
	f.Add([]byte("hosts: not-a-list\n"))
	f.Add([]byte("spec:\n  config:\n    registries: not-a-list\n"))
	f.Add([]byte("spec: &x\n  config: *x\n"))

	f.Fuzz(func(_ *testing.T, doc []byte) {
		_, _ = clustercfg.Parse(context.Background(), doc) //nolint:errcheck // no-panic fuzz test, malformed input is expected to error
	})
}
