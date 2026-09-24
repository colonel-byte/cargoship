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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKindsAreServable(t *testing.T) {
	kinds := Kinds()
	require.Equal(t, []string{"config", "inventory", "package"}, kinds)

	for _, k := range kinds {
		t.Run(k, func(t *testing.T) {
			doc, err := Load(Kind(k))
			require.NoError(t, err)
			require.NotEmpty(t, doc["$defs"])
			require.NotEmpty(t, doc["properties"])
		})
	}
}

func TestUnknownKind(t *testing.T) {
	_, err := Raw("nonsense")
	require.ErrorContains(t, err, "unknown schema")
	require.ErrorContains(t, err, "inventory", "the error names what is available")
}

// TestEmbeddedMatchesRepository is the guard on the duplication. go:embed cannot reach the
// repository's schema/ directory, so mage generate:schema writes both copies; if this fails, the
// binary is serving a schema that is not the one cargoship publishes.
func TestEmbeddedMatchesRepository(t *testing.T) {
	for _, k := range Kinds() {
		name, err := FileName(Kind(k))
		require.NoError(t, err)

		published, err := os.ReadFile(filepath.Join("..", "..", "..", "schema", name))
		require.NoError(t, err, "run: mage generate:schema")

		embeddedCopy, err := Raw(Kind(k))
		require.NoError(t, err)
		require.Equal(t, string(published), string(embeddedCopy),
			"%s differs from schema/%s -- run: mage generate:schema", k, name)
	}
}
