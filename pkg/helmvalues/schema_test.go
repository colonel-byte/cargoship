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

package helmvalues

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const testSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["replicas"],
  "properties": {
    "replicas": {"type": "integer", "minimum": 1},
    "image": {
      "type": "object",
      "properties": {"tag": {"type": "string"}}
    }
  }
}`

func TestParseSchema(t *testing.T) {
	t.Run("valid schema", func(t *testing.T) {
		got, err := ParseSchema("test", []byte(testSchema))
		require.NoError(t, err)
		require.Equal(t, "object", got["type"])
	})

	t.Run("malformed JSON is rejected", func(t *testing.T) {
		_, err := ParseSchema("test", []byte("{"))
		require.ErrorContains(t, err, "parsing values schema")
	})

	t.Run("empty document is rejected", func(t *testing.T) {
		_, err := ParseSchema("test", []byte("null"))
		require.ErrorContains(t, err, "is empty")
	})

	t.Run("invalid schema is rejected", func(t *testing.T) {
		_, err := ParseSchema("test", []byte(`{"type": "nonsense"}`))
		require.ErrorContains(t, err, "invalid values schema")
	})

	// A schema travels inside a signed package and is validated on an air-gapped
	// host, so a reference to another document must not be resolvable.
	t.Run("references are rejected", func(t *testing.T) {
		for _, doc := range []string{
			`{"type":"object","properties":{"a":{"$ref":"https://example.com/s.json"}}}`,
			`{"type":"object","properties":{"a":{"$ref":"#/$defs/b"}},"$defs":{"b":{"type":"string"}}}`,
			`{"type":"object","allOf":[{"$ref":"other.json"}]}`,
		} {
			_, err := ParseSchema("test", []byte(doc))
			require.ErrorContains(t, err, "not supported")
		}
	})
}

func TestLoadSchema(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "schema.json", testSchema)

	got, err := LoadSchema(p)
	require.NoError(t, err)
	require.Equal(t, "object", got["type"])

	_, err = LoadSchema(filepath.Join(dir, "absent.json"))
	require.ErrorContains(t, err, "reading values schema")
}

func TestSchemaValidate(t *testing.T) {
	schema, err := ParseSchema("test", []byte(testSchema))
	require.NoError(t, err)

	t.Run("valid values pass", func(t *testing.T) {
		require.NoError(t, schema.Validate(map[string]any{
			"replicas": 2,
			"image":    map[string]any{"tag": "v1"},
		}))
	})

	t.Run("every problem is reported", func(t *testing.T) {
		err := schema.Validate(map[string]any{
			"replicas": 0,
			"image":    map[string]any{"tag": 5},
		})
		var vErr *ValidationError
		require.ErrorAs(t, err, &vErr)
		require.Len(t, vErr.Problems, 2)
		require.Contains(t, err.Error(), "replicas")
		require.Contains(t, err.Error(), "image.tag")
	})

	t.Run("a missing required key is a problem", func(t *testing.T) {
		err := schema.Validate(map[string]any{})
		require.ErrorContains(t, err, "replicas is required")
	})

	t.Run("nil values are treated as empty", func(t *testing.T) {
		require.ErrorContains(t, schema.Validate(nil), "replicas is required")
	})

	t.Run("no schema validates anything", func(t *testing.T) {
		require.NoError(t, Schema(nil).Validate(map[string]any{"anything": true}))
		require.NoError(t, Schema{}.Validate(nil))
	})
}
