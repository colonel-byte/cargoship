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
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/src/pkg/helmvalues"
	"github.com/stretchr/testify/require"
)

// FuzzParseFileShape asserts that ParseFile either fails or returns a map whose nested maps are
// all map[string]any, whatever bytes a values file on disk holds.
//
// A values file is the one place ZEP-0021 restricts to YAML, but the operator or package author
// writing it may still produce anything a YAML decoder accepts: an anchor cycle, a document with a
// non-mapping top level, a key that is itself a mapping, a value larger than an int64, bytes that
// are not valid UTF-8 at all. normalizeMap's job downstream is to fold every one of those into
// map[string]any so a caller need not branch on map[any]any or dig.Mapping; a case it misses would
// surface later as a type assertion panic in code far from the parser.
func FuzzParseFileShape(f *testing.F) {
	f.Add([]byte("a: 1\nb:\n  c: 2\n"))
	f.Add([]byte(""))
	f.Add([]byte("# just a comment\n"))
	f.Add([]byte("- 1\n- 2\n"))
	f.Add([]byte("just a scalar"))
	f.Add([]byte("a: &x\n  y: 1\nb: *x\n"))
	f.Add([]byte("1: one\ntrue: yes\n"))
	f.Add([]byte("a: !!binary aGVsbG8=\n"))
	f.Add([]byte("a: 99999999999999999999999999999999\n"))
	f.Add([]byte("\xff\xfe not valid utf-8"))
	f.Add([]byte("a: {b: {c: {d: {e: 1}}}}\n"))

	f.Fuzz(func(t *testing.T, contents []byte) {
		dir := t.TempDir()
		path := filepath.Join(dir, "values.yaml")
		require.NoError(t, os.WriteFile(path, contents, 0o600))

		values, err := helmvalues.ParseFile(path)
		if err != nil {
			return
		}
		requireStringKeyedAllTheWayDown(t, values)
	})
}

// requireStringKeyedAllTheWayDown fails the test if v, or anything nested in it, is a map that is
// not map[string]any.
func requireStringKeyedAllTheWayDown(t *testing.T, v any) {
	t.Helper()

	switch val := v.(type) {
	case map[string]any:
		for _, item := range val {
			requireStringKeyedAllTheWayDown(t, item)
		}
	case []any:
		for _, item := range val {
			requireStringKeyedAllTheWayDown(t, item)
		}
	case map[any]any:
		t.Fatalf("a nested map escaped normalization as map[any]any: %v", val)
	}
}

// FuzzParseSchemaNoExternalRefs asserts that a schema ParseSchema accepts never contains a
// reference keyword anywhere in its tree, and that validating against an accepted schema never
// panics.
//
// A schema travels inside a signed package. checkNoExternalRefs is what stands between a package
// and reaching outside its own document during validation -- resolving $ref would let a validated
// package contact a host the operator never agreed to, which defeats an air-gapped deploy. The
// invariant is checked again here, independently of the internal walk, so a refactor that only
// checks the top level or stops at the first array would be caught by the fuzz corpus rather than
// shipping quietly.
func FuzzParseSchemaNoExternalRefs(f *testing.F) {
	f.Add([]byte(`{"type": "object"}`))
	f.Add([]byte(`{"$ref": "#/$defs/x"}`))
	f.Add([]byte(`{"properties": {"a": {"$ref": "other.json"}}}`))
	f.Add([]byte(`{"items": [{"$dynamicRef": "#x"}]}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"type": "object", "properties": {"a": {"type": "integer"}}, "required": ["a"]}`))
	f.Add([]byte(`not json`))
	f.Add([]byte(`{"$recursiveRef": "#"}`))
	f.Add([]byte(`[]`))

	f.Fuzz(func(t *testing.T, doc []byte) {
		schema, err := helmvalues.ParseSchema("fuzz", doc)
		if err != nil {
			return
		}

		require.False(t, containsRefKeyword(map[string]any(schema)),
			"ParseSchema accepted a schema that contains a reference keyword")

		// Validating must not panic, whatever the schema and whatever the values, including the
		// nil values a package with no set values validates against.
		_ = schema.Validate(nil)
		_ = schema.Validate(map[string]any{})
		_ = schema.Validate(map[string]any{"anything": []any{1, "two", nil}})
	})
}

// containsRefKeyword walks node the same way checkNoExternalRefs does, restated independently so
// the fuzz target is not just re-running the code under test against itself.
func containsRefKeyword(node any) bool {
	refKeywords := []string{"$ref", "$dynamicRef", "$recursiveRef"}
	switch n := node.(type) {
	case map[string]any:
		for _, kw := range refKeywords {
			if _, ok := n[kw]; ok {
				return true
			}
		}
		for _, v := range n {
			if containsRefKeyword(v) {
				return true
			}
		}
	case []any:
		for _, v := range n {
			if containsRefKeyword(v) {
				return true
			}
		}
	}
	return false
}
