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
		assert.NoError(t, err, "expected doc file to exist: %s", target.DocFile)
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
		assert.NoError(t, err)
		assert.NotEmpty(t, sData)

		eData, err := os.ReadFile(embedFile)
		assert.NoError(t, err)
		assert.Equal(t, sData, eData)
	}
}
