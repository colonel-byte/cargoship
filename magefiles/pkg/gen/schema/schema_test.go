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

// Package schema_test tests schema reflection and documentation generation.
package schema_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/schema"
	strcase "github.com/stoewer/go-strcase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateV1Alpha1Schema(t *testing.T) {
	t.Chdir("../../../..")

	b, err := schema.GenerateV1Alpha1Schema(&cluster.ZarfCluster{}, []string{"api", "zarf.dev", "v1alpha1", "cluster"}, strcase.LowerCamelCase)
	require.NoError(t, err)

	var parsed map[string]any
	err = json.Unmarshal(b, &parsed)
	require.NoError(t, err)
	assert.NotEmpty(t, parsed["$defs"])
}

func TestSchemaTargets(t *testing.T) {
	targets := schema.Targets()
	require.Len(t, targets, 3)

	for _, target := range targets {
		assert.NotEmpty(t, target.SchemaPath)
		assert.NotEmpty(t, target.DocTitle)
		assert.NotEmpty(t, target.DocFile)
		assert.NotEmpty(t, target.StructPath)
	}
}

func TestGenerateDocs(t *testing.T) {
	t.Chdir("../../../..")

	tmpDir := t.TempDir()
	origDocsDir := schema.SchemaDocsDir
	schema.SchemaDocsDir = tmpDir
	defer func() {
		schema.SchemaDocsDir = origDocsDir
	}()

	err := schema.GenerateDocs()
	require.NoError(t, err)

	for _, target := range schema.Targets() {
		docPath := filepath.Join(tmpDir, target.DocFile)
		data, err := os.ReadFile(docPath)
		require.NoError(t, err, "expected doc file to exist: %s", target.DocFile)
		assert.NotEmpty(t, data)
	}
}

func TestGenerateSchemas(t *testing.T) {
	t.Chdir("../../../..")

	tmpDir := t.TempDir()
	origSchemaDir := schema.SchemaDir
	origEmbedDir := schema.SchemaEmbedDir
	schema.SchemaDir = filepath.Join(tmpDir, "schema")
	schema.SchemaEmbedDir = filepath.Join(tmpDir, "embed")
	defer func() {
		schema.SchemaDir = origSchemaDir
		schema.SchemaEmbedDir = origEmbedDir
	}()

	err := schema.GenerateSchemas()
	require.NoError(t, err)

	for _, target := range schema.Targets() {
		schemaFile := filepath.Join(schema.SchemaDir, target.SchemaPath)
		embedFile := filepath.Join(schema.SchemaEmbedDir, target.SchemaPath)

		sData, err := os.ReadFile(schemaFile)
		require.NoError(t, err)
		assert.NotEmpty(t, sData)

		eData, err := os.ReadFile(embedFile)
		require.NoError(t, err)
		assert.Equal(t, sData, eData)
	}
}

// TestSensitivePropertiesAreMarked pins the x-sensitive marking on the properties whose value can
// be a credential rather than a path to one. The OpenTofu provider reads this marking to decide
// which attributes to declare sensitive, so a field losing the tag is a secret landing in tofu
// state in plaintext, which no other test would notice.
func TestSensitivePropertiesAreMarked(t *testing.T) {
	t.Chdir("../../../..")

	cases := []struct {
		name       string
		target     string
		def        string
		properties []string
	}{
		{
			name:   "registry credentials",
			target: "zarf-v1alpha1-cluster-schema.json",
			def:    "ZarfClusterRegistryAuth",
			properties: []string{
				"pass",
				"token",
				"user",
			},
		},
		{
			name:   "package signing key password",
			target: "zarf-config-distro-schema.json",
			def:    "DistroPublishOptions",
			properties: []string{
				"signing_key_password",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var target schema.Target
			for _, candidate := range schema.Targets() {
				if candidate.SchemaPath == tc.target {
					target = candidate
				}
			}
			require.NotEmpty(t, target.SchemaPath, "no schema target named %s", tc.target)

			b, err := schema.GenerateV1Alpha1Schema(target.SchemaStruct, target.StructPath, target.Namer())
			require.NoError(t, err)

			var parsed map[string]any
			require.NoError(t, json.Unmarshal(b, &parsed))

			defs, ok := parsed["$defs"].(map[string]any)
			require.True(t, ok, "schema has no $defs")
			def, ok := defs[tc.def].(map[string]any)
			require.True(t, ok, "schema has no $defs.%s", tc.def)
			props, ok := def["properties"].(map[string]any)
			require.True(t, ok, "%s has no properties", tc.def)

			for _, name := range tc.properties {
				prop, ok := props[name].(map[string]any)
				require.True(t, ok, "%s has no property %s", tc.def, name)
				assert.Equal(t, true, prop["x-sensitive"], "%s.%s is not marked x-sensitive", tc.def, name)
			}
		})
	}
}
