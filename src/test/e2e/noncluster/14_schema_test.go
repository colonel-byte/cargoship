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

package noncluster

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// valuesSchemaDistroDir is testdata/minimal with a values schema, for the composing tests.
const valuesSchemaDistroDir = "src/test/e2e/noncluster/testdata/values-schema"

// TestCargoshipSchema exercises the schema command against the real binary: the three kinds it
// serves out of itself, and the two package shapes --package accepts. The point of the command is
// that an air-gapped operator gets editor support without reaching GitHub, so every case here runs
// without a network.
func TestCargoshipSchema(t *testing.T) {
	t.Run("serves each kind on stdout", func(t *testing.T) {
		for _, kind := range []string{"inventory", "package", "config"} {
			stdout, _, err := e2e.Cargoship(t, "schema", kind)
			require.NoError(t, err)

			var doc map[string]any
			require.NoError(t, json.Unmarshal([]byte(stdout), &doc), "%s schema is not valid JSON", kind)
			require.NotEmpty(t, doc["properties"])
			require.NotEmpty(t, doc["$defs"])
		}
	})

	t.Run("the served schema is the published one", func(t *testing.T) {
		// The binary carries its own copy, since go:embed cannot reach the repository's
		// schema/ directory. A difference here means the two have drifted.
		stdout, _, err := e2e.Cargoship(t, "schema", "inventory")
		require.NoError(t, err)

		published, err := os.ReadFile(filepath.Join("schema", "zarf-v1alpha1-cluster-schema.json"))
		require.NoError(t, err)
		require.Equal(t, string(published), stdout, "run: mage generate:schema")
	})

	t.Run("writes to a file with -o", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "inventory.schema.json")
		stdout, _, err := e2e.Cargoship(t, "schema", "inventory", "-o", path)
		require.NoError(t, err)
		require.Empty(t, stdout)

		b, err := os.ReadFile(path)
		require.NoError(t, err)
		var doc map[string]any
		require.NoError(t, json.Unmarshal(b, &doc))
	})

	t.Run("rejects an unknown kind", func(t *testing.T) {
		_, stderr, err := e2e.Cargoship(t, "schema", "cluster")
		require.Error(t, err)
		require.Contains(t, stderr, "unknown schema")
	})

	t.Run("composes a built package's values", func(t *testing.T) {
		outDir := t.TempDir()
		_, _, err := e2e.Cargoship(t, "create", valuesSchemaDistroDir, "-o", outDir)
		require.NoError(t, err)

		matches, err := filepath.Glob(filepath.Join(outDir, "*.tar.zst"))
		require.NoError(t, err)
		require.Len(t, matches, 1)

		stdout, _, err := e2e.Cargoship(t, "schema", "inventory", "--package", matches[0])
		require.NoError(t, err)

		values := schemaValuesNode(t, stdout)
		require.Equal(t, false, values["additionalProperties"])
		require.Contains(t, values, "properties", "the block an operator overrides is no longer untyped")
		require.NotContains(t, values, "required",
			"an inventory carries overrides on top of the values the package ships")
		require.Contains(t, values["description"], "e2e-values-schema")
	})

	t.Run("composes a package source directory", func(t *testing.T) {
		// Reading a definition that has never been built is what makes this usable while a
		// package is still being written.
		stdout, _, err := e2e.Cargoship(t, "schema", "inventory", "--package", valuesSchemaDistroDir)
		require.NoError(t, err)

		values := schemaValuesNode(t, stdout)
		require.Contains(t, values["properties"], "replicas")
	})

	t.Run("warns when a package declares no values schema", func(t *testing.T) {
		stdout, stderr, err := e2e.Cargoship(t, "schema", "inventory", "--package", minimalDistroDir)
		require.NoError(t, err)
		require.Contains(t, stderr, "declares no values schema")

		values := schemaValuesNode(t, stdout)
		require.NotContains(t, values, "properties")
	})

	t.Run("rejects --package for the other kinds", func(t *testing.T) {
		_, stderr, err := e2e.Cargoship(t, "schema", "package", "--package", minimalDistroDir)
		require.Error(t, err)
		require.Contains(t, stderr, "only applies to the inventory schema")
	})
}

// schemaValuesNode pulls out the node a package's values schema is grafted onto.
func schemaValuesNode(t *testing.T, out string) map[string]any {
	t.Helper()

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &doc))

	cur := doc
	for _, k := range []string{"$defs", "ZarfClusterConfig", "properties", "values"} {
		next, ok := cur[k].(map[string]any)
		require.Truef(t, ok, "expected a map at %q", k)
		cur = next
	}
	return cur
}
