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

package examples

import (
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/colonel-byte/cargoship/pkg/helmvalues"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// repoRoot reaches the repository root from this package's directory, which is where the
// template paths in exampleDistros are resolved from. Four levels: magefiles/pkg/gen/examples.
const repoRoot = "../../../.."

// TestValuesTemplatesMatchTheirSchemas renders every flavor's values.yaml and
// values.schema.json templates and checks the pair against each other with the same validator
// cargoship uses when it builds a package. A values file that sets a key the schema does not
// describe, or a schema that reaches for $ref, fails here rather than at build time on an
// air-gapped management node -- which is the rule magefiles/AGENTS.md states in prose.
func TestValuesTemplatesMatchTheirSchemas(t *testing.T) {
	for _, spec := range exampleDistros {
		for _, f := range spec.flavors {
			if len(f.values) == 0 {
				continue
			}
			t.Run(spec.name+"-"+f.flavorName(), func(t *testing.T) {
				data := map[string]any{
					"Name":          f.flavorName(),
					"CNI":           f.cni,
					"CloudProvider": f.cloudProvider,
					"Minor":         "v1.35",
					"Addons":        []string{"rke2-coredns"},
				}

				var schema helmvalues.Schema
				values := map[string]any{}
				for _, path := range f.values {
					rendered := renderValuesTemplate(t, filepath.Join(repoRoot, path), data)
					if strings.HasSuffix(path, ".json.tmpl") {
						s, err := helmvalues.ParseSchema(filepath.Base(path), []byte(rendered))
						require.NoError(t, err, "schema %s does not parse", path)
						schema = s
						continue
					}
					require.NoError(t, yaml.Unmarshal([]byte(rendered), &values), "values %s do not parse", path)
				}

				require.NoError(t, schema.Validate(values), "rendered values do not satisfy the rendered schema")
			})
		}
	}
}

func renderValuesTemplate(t *testing.T, path string, data map[string]any) string {
	t.Helper()
	tmpl, err := template.ParseFiles(path)
	require.NoError(t, err)

	var out strings.Builder
	require.NoError(t, tmpl.Execute(&out, data))
	return out.String()
}
