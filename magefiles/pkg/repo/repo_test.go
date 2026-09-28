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

package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRootIsTheModuleRoot checks the walk lands on the directory holding this module's go.mod
// rather than on some other go.mod above it.
func TestRootIsTheModuleRoot(t *testing.T) {
	root, err := Root()
	require.NoError(t, err)

	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(mod), "module github.com/colonel-byte/cargoship")

	// The paths the generators address are relative to this directory, so a root that resolves
	// them is the only useful answer.
	require.DirExists(t, filepath.Join(root, "magefiles", "templates"))
	require.DirExists(t, filepath.Join(root, "docs"))
}

// TestChdirRestoresTheWorkingDirectory checks the cleanup runs, since a test that left the process
// in the repository root would make the next one pass for the wrong reason.
func TestChdirRestoresTheWorkingDirectory(t *testing.T) {
	before, err := os.Getwd()
	require.NoError(t, err)

	t.Run("inside", func(t *testing.T) {
		root := Chdir(t)

		wd, err := os.Getwd()
		require.NoError(t, err)
		// Compared through EvalSymlinks: on macOS the root resolves through /private.
		require.Equal(t, evalSymlinks(t, root), evalSymlinks(t, wd))

		// A path a target would use resolves from here.
		require.FileExists(t, "magefiles/templates/k3s-distro.yaml.tmpl")
	})

	after, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func evalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)
	return resolved
}
