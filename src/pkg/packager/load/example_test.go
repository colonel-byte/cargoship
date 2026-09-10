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

package load

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// exampleRoot is the committed example tree, relative to this package.
const exampleRoot = "../../../../example"

// TestExampleDefinitionsLoad loads every committed example. The examples are what a reader copies
// from, and most of them are generated, so a template change that renders a definition cargoship
// will not accept should fail here rather than the first time somebody builds one. Loading covers
// parsing and validation, including that no file selects an architecture its package does not
// declare.
func TestExampleDefinitionsLoad(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	paths := examplePaths(t)
	require.NotEmpty(t, paths, "no examples found under %s", exampleRoot)

	for _, path := range paths {
		t.Run(exampleName(path), func(t *testing.T) {
			t.Parallel()

			dis, err := DistroDefinition(ctx, filepath.Dir(path), DefinitionOptions{})
			require.NoError(t, err)
			require.NotEmpty(t, dis.Metadata.Name)
		})
	}
}

// examplePaths is every example distro.yaml on disk.
func examplePaths(t *testing.T) []string {
	t.Helper()

	var paths []string
	err := filepath.WalkDir(exampleRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "distro.yaml" {
			paths = append(paths, path)
		}
		return nil
	})
	require.NoError(t, err)

	return paths
}

// exampleName turns an example's path into a subtest name: k3s-multi/v1_36/v1.36.4-k3s1.
func exampleName(path string) string {
	rel, err := filepath.Rel(exampleRoot, filepath.Dir(path))
	if err != nil {
		return path
	}
	return strings.ReplaceAll(rel, string(os.PathSeparator), "/")
}
