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
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/magefiles/pkg/repo"
	"github.com/k0sproject/rig/v2/protocol/openssh"
	"github.com/k0sproject/rig/v2/protocol/ssh"
	"github.com/k0sproject/rig/v2/protocol/winrm"
	"github.com/stretchr/testify/require"
)

// TestReflectMatchesTheCheckedInSchemas is the strongest check here: schema/*.json is committed
// and served to editors by URL, so reflecting the same struct again has to produce the same
// bytes. The trailing newline is the one thing Generate adds on top.
func TestReflectMatchesTheCheckedInSchemas(t *testing.T) {
	repo.Chdir(t)

	for _, target := range Targets() {
		t.Run(target.Path, func(t *testing.T) {
			got, err := Reflect(target.Struct, target.StructPath, target.Namer())
			require.NoError(t, err)

			want, err := os.ReadFile(filepath.Join(Dir, target.Path))
			require.NoError(t, err)
			require.Equal(t, string(want), string(append(got, '\n')))

			// The copy the binary serves out of go:embed is written by the same target, so it
			// cannot be allowed to drift from the published one.
			embedded, err := os.ReadFile(filepath.Join(EmbedDir, target.Path))
			require.NoError(t, err)
			require.Equal(t, string(want), string(embedded))
		})
	}
}

// TestTargetsAreDistinct guards the two ways a new target would quietly overwrite an existing
// one: the schema path is a published URL, and the doc file is a page under docs/schema.
func TestTargetsAreDistinct(t *testing.T) {
	paths := map[string]bool{}
	files := map[string]bool{}
	for _, target := range Targets() {
		require.NotNil(t, target.Struct)
		require.NotEmpty(t, target.StructPath)
		require.NotEmpty(t, target.DocTitle)

		require.False(t, paths[target.Path], "%s is claimed twice", target.Path)
		require.False(t, files[target.DocFile], "%s is claimed twice", target.DocFile)
		paths[target.Path] = true
		files[target.DocFile] = true
	}
}

func TestNamer(t *testing.T) {
	// A target with no namer of its own gets the lower-camel default.
	require.Equal(t, "someField", Target{}.Namer()("SomeField"))

	// The cluster schema spells two protocol names the way rig's YAML does, which lower-camel
	// would render as "openssh" and "winrm".
	var namer func(string) string
	for _, target := range Targets() {
		if target.Path == "zarf-v1alpha1-cluster-schema.json" {
			namer = target.Namer()
		}
	}
	require.NotNil(t, namer)
	require.Equal(t, "openSSH", namer("OpenSSH"))
	require.Equal(t, "winRM", namer("WinRM"))
	require.Equal(t, "someField", namer("SomeField"))
}

// TestRigConfigDefName covers the collision this exists to prevent: rig names all three of its
// connection configs "Config", and $defs is keyed on the bare type name.
func TestRigConfigDefName(t *testing.T) {
	require.Equal(t, "SSHConfig", rigConfigDefName(reflect.TypeOf(ssh.Config{})))
	require.Equal(t, "OpenSSHConfig", rigConfigDefName(reflect.TypeOf(openssh.Config{})))
	require.Equal(t, "WinRMConfig", rigConfigDefName(reflect.TypeOf(winrm.Config{})))

	// Anything else keeps jsonschema's own naming, which "" asks for.
	require.Empty(t, rigConfigDefName(reflect.TypeOf(cluster.ZarfCluster{})))
}

func TestAddYAMLExtensions(t *testing.T) {
	doc := map[string]any{
		PropertiesKey: map[string]any{
			"nested": map[string]any{
				PropertiesKey: map[string]any{"leaf": map[string]any{"type": "string"}},
			},
		},
		"oneOf": []any{
			map[string]any{PropertiesKey: map[string]any{"branch": map[string]any{}}},
		},
	}
	addYAMLExtensions(doc)

	// Every object with properties gains the x- escape hatch, at any depth and inside a list.
	require.Contains(t, doc, PatternPropertiesKey)
	nested := object(t, object(t, doc[PropertiesKey])["nested"])
	require.Contains(t, nested, PatternPropertiesKey)

	list, ok := doc["oneOf"].([]any)
	require.True(t, ok)
	require.Contains(t, object(t, list[0]), PatternPropertiesKey)

	// A leaf with no properties of its own is left alone.
	require.NotContains(t, object(t, object(t, nested[PropertiesKey])["leaf"]), PatternPropertiesKey)
}

// object asserts that a walked schema node is an object and returns it, so a test can descend
// without an unchecked type assertion.
func object(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	require.True(t, ok, "%v is not an object", v)
	return m
}

func TestAddYAMLExtensionsKeepsAnExistingPattern(t *testing.T) {
	mine := map[string]any{"^y-": map[string]any{}}
	doc := map[string]any{PropertiesKey: map[string]any{}, PatternPropertiesKey: mine}
	addYAMLExtensions(doc)
	require.Equal(t, mine, doc[PatternPropertiesKey])
}

// TestFlattenRigHostConfig covers the patch that makes ZarfHost match the YAML rig actually
// loads: the inline CompositeConfig promoted to top level, the two artefacts of jsonschema not
// understanding a yaml-only inline tag removed, and only "role" left required.
func TestFlattenRigHostConfig(t *testing.T) {
	doc := map[string]any{"$defs": map[string]any{
		"ZarfHost": map[string]any{
			PropertiesKey: map[string]any{
				"role":             map[string]any{"type": "string"},
				"connectionConfig": map[string]any{"$ref": "#/$defs/CompositeConfig"},
				"runner":           map[string]any{},
			},
			"required": []string{"role", "connectionConfig"},
		},
		"CompositeConfig": map[string]any{
			PropertiesKey: map[string]any{
				"ssh":       map[string]any{"$ref": "#/$defs/SSHConfig"},
				"localhost": map[string]any{"type": "boolean"},
			},
		},
	}}
	flattenRigHostConfig(doc)

	defs := object(t, doc["$defs"])
	require.NotContains(t, defs, "CompositeConfig", "it is inlined, so it has no section of its own")

	host := object(t, defs["ZarfHost"])
	props := object(t, host[PropertiesKey])
	require.Contains(t, props, "ssh")
	require.Contains(t, props, "localhost")
	require.NotContains(t, props, "connectionConfig")
	require.NotContains(t, props, "runner")

	// A host sets exactly one connection, so none of them can be required.
	require.Equal(t, []string{"role"}, host["required"])
}

// TestFlattenRigHostConfigOnAnUnrelatedSchema leaves a document with no ZarfHost untouched,
// which is what lets the config schema go through the same code path.
func TestFlattenRigHostConfigOnAnUnrelatedSchema(t *testing.T) {
	for _, doc := range []map[string]any{
		{},
		{"$defs": map[string]any{"Other": map[string]any{}}},
	} {
		before := len(doc)
		flattenRigHostConfig(doc)
		require.Len(t, doc, before)
	}
}
