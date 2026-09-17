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

package schema

import (
	"encoding/json"
	"testing"

	"github.com/colonel-byte/cargoship/src/pkg/helmvalues"
	"github.com/stretchr/testify/require"
	"github.com/xeipuuv/gojsonschema"
)

// valuesSchema is shaped like a real package's, plus the two things the graft has to be
// careful about: a required list, and a value legitimately named "required".
const valuesSchema = `{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "$id": "https://example.com/values.schema.json",
  "type": "object",
  "additionalProperties": false,
  "description": "What this package accepts.",
  "required": ["addons"],
  "properties": {
    "addons": {
      "type": "object",
      "additionalProperties": false,
      "required": ["disabled"],
      "properties": {
        "disabled": {
          "type": "array",
          "items": {
            "type": "string",
            "pattern": "^rke2-[a-z0-9-]+$",
            "examples": ["rke2-coredns", "rke2-traefik"]
          },
          "uniqueItems": true
        }
      }
    },
    "auth": {
      "type": "object",
      "properties": {
        "required": {"type": "boolean"}
      }
    },
    "mode": {
      "enum": ["ha", "single"],
      "default": {"required": true}
    }
  }
}`

func composed(t *testing.T, raw string, note string) map[string]any {
	t.Helper()
	values, err := helmvalues.ParseSchema("test", []byte(raw))
	require.NoError(t, err)
	doc, err := ComposeInventory(values, note)
	require.NoError(t, err)
	return doc
}

// node walks a decoded document, failing the test rather than panicking on a wrong shape.
func node(t *testing.T, doc map[string]any, path ...string) map[string]any {
	t.Helper()
	cur := doc
	for _, k := range path {
		next, ok := cur[k].(map[string]any)
		require.Truef(t, ok, "expected a map at %q", k)
		cur = next
	}
	return cur
}

func valuesOf(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	return node(t, doc, valuesPath...)
}

func TestComposeInventoryGrafts(t *testing.T) {
	doc := composed(t, valuesSchema, "from the test package, version 1.2.3")
	v := valuesOf(t, doc)

	require.Equal(t, false, v["additionalProperties"], "additionalProperties is correct on the override subtree and must survive")
	require.Equal(t, "object", v["type"])

	items := node(t, v, "properties", "addons", "properties", "disabled", "items")
	require.Equal(t, "^rke2-[a-z0-9-]+$", items["pattern"])
	require.Equal(t, []any{"rke2-coredns", "rke2-traefik"}, items["examples"],
		"examples are what an editor completes from, so they must reach the composed schema")
	require.Equal(t, []any{"ha", "single"}, node(t, v, "properties", "mode")["enum"])
}

func TestComposeInventoryStripsRequired(t *testing.T) {
	doc := composed(t, valuesSchema, "")
	v := valuesOf(t, doc)

	// An inventory carries overrides on top of the values the package already ships, so a key
	// the package's own values.yaml supplies is not required of the operator.
	require.NotContains(t, v, "required")
	require.NotContains(t, node(t, v, "properties", "addons"), "required")

	// A value that happens to be called "required" is a property name, not the keyword.
	require.Contains(t, node(t, v, "properties", "auth", "properties"), "required")

	// So is one inside instance data.
	require.Equal(t, map[string]any{"required": true}, node(t, v, "properties", "mode")["default"])
}

func TestComposeInventoryDropsStandaloneKeywords(t *testing.T) {
	v := valuesOf(t, composed(t, valuesSchema, ""))
	require.NotContains(t, v, "$schema", "a nested $schema is read as a dialect switch by some validators")
	require.NotContains(t, v, "$id")
}

func TestComposeInventoryDescription(t *testing.T) {
	v := valuesOf(t, composed(t, valuesSchema, "from the test package, version 1.2.3"))
	desc, ok := v["description"].(string)
	require.True(t, ok)
	require.Contains(t, desc, "overrides the values the distro package was built with",
		"the generated description explains what the block is for")
	require.Contains(t, desc, "What this package accepts.", "the package's own description is hover text too")
	require.Contains(t, desc, "from the test package, version 1.2.3")
}

func TestComposeInventoryLeavesTheRestAlone(t *testing.T) {
	plain, err := Load(KindInventory)
	require.NoError(t, err)
	got := composed(t, valuesSchema, "")

	// Everything outside the grafted node has to be untouched, or the composed schema stops
	// describing an inventory.
	delete(node(t, plain, "$defs", "ZarfClusterConfig", "properties"), "values")
	delete(node(t, got, "$defs", "ZarfClusterConfig", "properties"), "values")

	want, err := json.Marshal(plain)
	require.NoError(t, err)
	have, err := json.Marshal(got)
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(have))
}

func TestComposeInventoryWithoutValues(t *testing.T) {
	doc, err := ComposeInventory(nil, "ignored")
	require.NoError(t, err)

	v := valuesOf(t, doc)
	require.Equal(t, "object", v["type"])
	require.NotContains(t, v, "properties", "a package with no values schema leaves the block untyped")
	require.NotContains(t, v["description"], "ignored")
}

func TestComposeInventoryDoesNotMutateItsInput(t *testing.T) {
	values, err := helmvalues.ParseSchema("test", []byte(valuesSchema))
	require.NoError(t, err)
	before, err := json.Marshal(map[string]any(values))
	require.NoError(t, err)

	_, err = ComposeInventory(values, "note")
	require.NoError(t, err)

	after, err := json.Marshal(map[string]any(values))
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
}

// TestComposeInventoryIsValidSchema proves the composed document still compiles, which the graft
// could break by splicing in a subtree that collides with the host document.
func TestComposeInventoryIsValidSchema(t *testing.T) {
	doc := composed(t, valuesSchema, "note")
	b, err := json.Marshal(doc)
	require.NoError(t, err)

	// ParseSchema bans $ref, and the inventory schema is full of them, so it is compiled here
	// the way a validator would rather than through helmvalues.
	var round map[string]any
	require.NoError(t, json.Unmarshal(b, &round))
	require.NotEmpty(t, round["$defs"])
}

// TestComposedSchemaValidatesAnInventory is the behaviour an operator actually sees: the composed
// document is run against real inventory files the way an editor runs it, and has to accept a good
// one and reject the mistakes the graft exists to catch.
func TestComposedSchemaValidatesAnInventory(t *testing.T) {
	doc := composed(t, valuesSchema, "note")
	loader := gojsonschema.NewGoLoader(doc)

	inventory := func(values map[string]any) map[string]any {
		return map[string]any{
			"apiVersion": "zarf.dev/v1alpha1",
			"kind":       "ZarfCluster",
			"metadata":   map[string]any{"name": "bubbles"},
			"spec": map[string]any{
				"config": map[string]any{
					"loadbalancer": "bubbles-kc.test.com",
					"values":       values,
				},
				"hosts": []any{map[string]any{
					"hostname": "kc01",
					"role":     "controller",
					"ssh":      map[string]any{"address": "10.1.2.3", "user": "root"},
				}},
			},
		}
	}

	validate := func(t *testing.T, values map[string]any) *gojsonschema.Result {
		t.Helper()
		res, err := gojsonschema.Validate(loader, gojsonschema.NewGoLoader(inventory(values)))
		require.NoError(t, err)
		return res
	}

	t.Run("accepts a partial override", func(t *testing.T) {
		// "replicas" is required by the package schema and absent here, which is correct: the
		// package's own values.yaml supplies it, and the inventory only overrides addons.
		res := validate(t, map[string]any{
			"addons": map[string]any{"disabled": []any{"rke2-traefik"}},
		})
		require.True(t, res.Valid(), "%v", res.Errors())
	})

	t.Run("rejects a misspelled key", func(t *testing.T) {
		res := validate(t, map[string]any{"addonz": map[string]any{}})
		require.False(t, res.Valid())
	})

	t.Run("rejects a wrong type", func(t *testing.T) {
		res := validate(t, map[string]any{"replicas": "three"})
		require.False(t, res.Valid())
	})

	t.Run("rejects a name outside the pattern", func(t *testing.T) {
		res := validate(t, map[string]any{
			"addons": map[string]any{"disabled": []any{"traefik"}},
		})
		require.False(t, res.Valid())
	})

	t.Run("still checks the rest of the inventory", func(t *testing.T) {
		// The graft must not have broken the parts of the document it did not touch.
		res, err := gojsonschema.Validate(loader, gojsonschema.NewGoLoader(map[string]any{
			"kind": "ZarfCluster",
		}))
		require.NoError(t, err)
		require.False(t, res.Valid())
	})
}
