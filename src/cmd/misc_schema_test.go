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

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// runSchema builds the command the way the root does and runs it, returning stdout and stderr
// separately: the schema goes to one and everything else to the other, and that separation is what
// makes the output redirectable.
func runSchema(t *testing.T, args ...string) (stdout string, stderr string, err error) {
	t.Helper()

	cmd := newSchemaCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err = cmd.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

func TestSchemaWritesEachKindToStdout(t *testing.T) {
	for _, kind := range []string{"inventory", "package", "config"} {
		t.Run(kind, func(t *testing.T) {
			stdout, _, err := runSchema(t, kind)
			require.NoError(t, err)

			var doc map[string]any
			require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
			require.NotEmpty(t, doc["properties"])
		})
	}
}

func TestSchemaRejectsUnknownKind(t *testing.T) {
	_, _, err := runSchema(t, "cluster")
	require.ErrorContains(t, err, "unknown schema")
}

func TestSchemaRejectsPackageForOtherKinds(t *testing.T) {
	for _, kind := range []string{"package", "config"} {
		t.Run(kind, func(t *testing.T) {
			_, _, err := runSchema(t, kind, "--package", "./whatever")
			require.ErrorContains(t, err, "--package only applies to the inventory schema")
		})
	}
}

func TestSchemaOutputFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory.schema.json")

	stdout, stderr, err := runSchema(t, "inventory", "-o", path)
	require.NoError(t, err)
	require.Empty(t, stdout, "the schema goes to the file, not to stdout as well")
	require.Contains(t, stderr, path, "where it was written is a note, so it belongs on stderr")

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(b, &doc))
}

// TestSchemaComposesFromSourceDirectory covers the path a package author takes: the definition is
// on disk and has never been built, and its values schema still composes.
func TestSchemaComposesFromSourceDirectory(t *testing.T) {
	dir := t.TempDir()
	writeTestPackage(t, dir)

	stdout, _, err := runSchema(t, "inventory", "--package", dir)
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))

	values := schemaNode(t, doc, "$defs", "ZarfClusterConfig", "properties", "values")
	require.Equal(t, false, values["additionalProperties"])
	require.Contains(t, schemaNode(t, values, "properties"), "replicas")
	require.Contains(t, values["description"], "test-package")
}

// TestSchemaComposesFromManifestPath is the same source, named by its distro.yaml rather than by
// the directory holding it.
func TestSchemaComposesFromManifestPath(t *testing.T) {
	dir := t.TempDir()
	writeTestPackage(t, dir)

	stdout, _, err := runSchema(t, "inventory", "--package", filepath.Join(dir, "distro.yaml"))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	require.Contains(t,
		schemaNode(t, doc, "$defs", "ZarfClusterConfig", "properties", "values", "properties"),
		"replicas")
}

func TestSchemaPackageWithoutValuesSchema(t *testing.T) {
	dir := t.TempDir()
	writeTestPackage(t, dir)
	// A package that declares no schema is not an error: it accepts values without describing
	// them, and the plain inventory schema is the honest answer.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "distro.yaml"), []byte(`kind: ZarfDistro
metadata:
  name: test-package
  version: "1.0.0"
spec:
  type: rke2
`), 0600))

	stdout, stderr, err := runSchema(t, "inventory", "--package", dir)
	require.NoError(t, err)
	require.Contains(t, stderr, "declares no values schema")

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	require.NotContains(t, schemaNode(t, doc, "$defs", "ZarfClusterConfig", "properties", "values"), "properties")
}

func TestSchemaMissingPackage(t *testing.T) {
	_, _, err := runSchema(t, "inventory", "--package", filepath.Join(t.TempDir(), "absent"))
	require.Error(t, err)
}

func TestSchemaTakesExactlyOneKind(t *testing.T) {
	cmd := newSchemaCommand()
	require.Error(t, cmd.Args(&cobra.Command{}, []string{}))
	require.Error(t, cmd.Args(&cobra.Command{}, []string{"inventory", "package"}))
	require.NoError(t, cmd.Args(&cobra.Command{}, []string{"inventory"}))
}

func writeTestPackage(t *testing.T, dir string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "distro.yaml"), []byte(`kind: ZarfDistro
metadata:
  name: test-package
  version: "1.0.0"
spec:
  type: rke2
  values:
    files:
      - values.yaml
    schema: values.schema.json
`), 0600))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "values.schema.json"), []byte(`{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "required": ["replicas"],
  "properties": {
    "replicas": {"type": "integer", "minimum": 1}
  }
}`), 0600))
}

func schemaNode(t *testing.T, doc map[string]any, path ...string) map[string]any {
	t.Helper()
	cur := doc
	for _, k := range path {
		next, ok := cur[k].(map[string]any)
		require.Truef(t, ok, "expected a map at %q", k)
		cur = next
	}
	return cur
}
