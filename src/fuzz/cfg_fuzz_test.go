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
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/src/internal/cfg"
)

// minimalDistroDoc is a package definition with nothing but what Parse needs to succeed: an
// apiVersion, a kind and the fields Spec requires. It is repeated and mutated by the targets below
// to build multi-document input.
const minimalDistroDoc = `apiVersion: zarf.dev/v1alpha1
kind: ZarfDistro
metadata:
  name: fuzz
spec:
  version: "1.0.0"
`

// FuzzCfgParseNoPanic asserts that cfg.Parse, the entry point a package's own zarf.yaml is read
// through, never panics on arbitrary bytes.
//
// The function chains three untrusted-input-facing steps before a schema-typed decode ever runs:
// the goccy/go-yaml parser splitting the document, a probe decode that reads just the apiVersion
// field out of a node of unknown shape, and a version lookup against a short table. Each of those
// has its own opinion about a node that is not a mapping, a document that is empty, or an
// apiVersion field that is itself a mapping or a list rather than a scalar.
func FuzzCfgParseNoPanic(f *testing.F) {
	f.Add([]byte(minimalDistroDoc))
	f.Add([]byte(""))
	f.Add([]byte("# just a comment\n"))
	f.Add([]byte("not yaml: [unterminated"))
	f.Add([]byte("apiVersion: zarf.dev/v1alpha1\n"))
	f.Add([]byte("apiVersion:\n  - a\n  - b\n"))
	f.Add([]byte("apiVersion: {a: b}\n"))
	f.Add([]byte("apiVersion: unknown/v9\nkind: ZarfDistro\n"))
	f.Add([]byte("- just\n- a\n- list\n"))
	f.Add([]byte("just a scalar"))
	f.Add([]byte(minimalDistroDoc + "---\n" + minimalDistroDoc))
	f.Add([]byte("---\n---\n"))

	f.Fuzz(func(_ *testing.T, doc []byte) {
		_, _ = cfg.Parse(context.Background(), doc) //nolint:errcheck // no-panic fuzz test, malformed input is expected to error
	})
}

// FuzzCfgParseMultiDocNoPanic asserts the same for ParseMultiDoc, the entry point an already-built
// package's multi-document zarf.yaml is read through.
//
// This is the more involved of the two: it walks every document, tracks which apiVersions have
// been seen to reject a duplicate, and picks the highest-priority handler among the ones it
// recognises. A fuzzer assembling documents out of the corpus below reaches combinations a
// hand-written table test would not think to enumerate -- many copies of the same version, a known
// version next to unknown ones, documents that are empty or malformed interleaved with valid ones.
func FuzzCfgParseMultiDocNoPanic(f *testing.F) {
	f.Add([]byte(minimalDistroDoc))
	f.Add([]byte(""))
	f.Add([]byte(minimalDistroDoc + "---\n" + minimalDistroDoc))
	f.Add([]byte(minimalDistroDoc + "---\napiVersion: unknown/v9\nkind: ZarfDistro\n"))
	f.Add([]byte("---\n" + minimalDistroDoc + "---\n---\n"))
	f.Add([]byte("apiVersion: unknown/v1\n---\napiVersion: unknown/v2\n"))
	f.Add([]byte(strings.Repeat(minimalDistroDoc+"---\n", 5)))
	f.Add([]byte("not yaml: [unterminated"))

	f.Fuzz(func(_ *testing.T, doc []byte) {
		_, _ = cfg.ParseMultiDoc(context.Background(), doc) //nolint:errcheck // no-panic fuzz test, malformed input is expected to error
	})
}
