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
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestPackageImportCommand(t *testing.T) {
	cmd := newPackageImportCommand()
	require.Equal(t, "import [ARCHIVE]", cmd.Use)
	require.NotNil(t, cmd.Flags().Lookup("output"))
	require.NotNil(t, cmd.Flags().Lookup("format"))
	require.NotNil(t, cmd.Flags().Lookup("key"))
	require.NotNil(t, cmd.Flags().Lookup("verify"))

	// Verify completion function is registered for format
	fn, found := cmd.GetFlagCompletionFunc("format")
	require.True(t, found)
	require.NotNil(t, fn)
	completions, directive := fn(cmd, nil, "")
	require.NotEmpty(t, completions)
	require.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

	// ExactArgs(1)
	require.Error(t, cmd.Args(cmd, []string{}))
	require.NoError(t, cmd.Args(cmd, []string{"my-package.tar"}))
	require.Error(t, cmd.Args(cmd, []string{"one", "two"}))
}
