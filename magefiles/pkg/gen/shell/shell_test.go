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

package shell

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/cmd"
	"github.com/stretchr/testify/require"
)

// TestCompletionTargetsAreDistinct guards the table itself: two entries sharing a path would
// silently leave one shell reading another's script, and the run would still succeed.
func TestCompletionTargetsAreDistinct(t *testing.T) {
	names := map[string]bool{}
	paths := map[string]bool{}

	for _, target := range completionTargets {
		require.NotEmpty(t, target.name)
		require.NotNil(t, target.gen, "%s has no generator", target.name)
		require.False(t, names[target.name], "duplicate shell %q", target.name)
		require.False(t, paths[target.path], "duplicate path %q", target.path)
		names[target.name], paths[target.path] = true, true

		// Every script is written under hack/, next to the other developer tooling, rather
		// than into the build tree that hack/shell-completion.sh uses at release time.
		require.Equal(t, "hack", filepath.Base(filepath.Dir(filepath.Dir(target.path))))
	}

	require.ElementsMatch(t, []string{"bash", "zsh", "fish", "powershell"}, keys(names))
}

// TestGenerateWritesEachScript runs every entry of the table against a temporary directory, so
// the real hack/completion tree is not rewritten by the test suite.
func TestGenerateWritesEachScript(t *testing.T) {
	cargo := cmd.NewCargoshipCommand()

	for _, target := range completionTargets {
		t.Run(target.name, func(t *testing.T) {
			// Into a nested directory that does not exist, which is also what proves
			// generate creates the tree rather than requiring it.
			target.path = filepath.Join(t.TempDir(), "completion", filepath.Base(target.path))

			require.NoError(t, generate(cargo, target))

			content, err := os.ReadFile(target.path)
			require.NoError(t, err)
			require.NotEmpty(t, content)

			// Every shell's script drives completion by shelling back out to the binary's
			// hidden __complete command, so its name is the one thing they all contain.
			require.Contains(t, string(content), "cargoship")
		})
	}
}

// TestGenerateReportsAnUncreatablePath checks the error names the file, since a completion run
// that fails halfway leaves some scripts stale and the path is what tells you which.
func TestGenerateReportsAnUncreatablePath(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blocker, []byte("file"), 0o644))

	target := completionTargets[0]
	target.path = filepath.Join(blocker, "cargoship.bash")

	err := generate(cmd.NewCargoshipCommand(), target)
	require.Error(t, err)
	require.Contains(t, err.Error(), target.path)
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
