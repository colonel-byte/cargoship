// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/colonel-byte/cargoship/magefiles/pkg/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEscapeAngleBrackets(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no brackets",
			in:   "plain text",
			want: "plain text",
		},
		{
			name: "opening and closing brackets",
			in:   "node-role.kubernetes.io/<profile>",
			want: "node-role.kubernetes.io/&lt;profile&gt;",
		},
		{
			name: "multiple brackets",
			in:   "<a> <b> <c>",
			want: "&lt;a&gt; &lt;b&gt; &lt;c&gt;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, util.EscapeAngleBrackets(tt.in))
		})
	}
}

func TestRenderYAMLValue(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name:  "nil value",
			value: nil,
			want:  "null",
		},
		{
			name:  "empty string",
			value: "",
			want:  `""`,
		},
		{
			name:  "non-empty string",
			value: "hello",
			want:  "hello",
		},
		{
			name:  "boolean true",
			value: true,
			want:  "true",
		},
		{
			name:  "integer",
			value: 42,
			want:  "42",
		},
		{
			name:  "slice",
			value: []string{"a", "b"},
			want:  "- a\n- b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, util.RenderYAMLValue(tt.value))
		})
	}
}

func TestPaddedTable(t *testing.T) {
	header := []string{"Name", "Role", "Description"}
	rows := [][]string{
		{"node-01", "controller", "Primary controller node"},
		{"node-02", "worker", "Worker node with <special> tag"},
		{"node-03", "worker", "UTF-8 test: 🚀 launch"},
	}

	table := util.PaddedTable(header, rows)
	require.NotEmpty(t, table)

	// All lines in the padded table must have the exact same character count.
	expectedLen := utf8.RuneCountInString(table[0])
	for i, line := range table {
		assert.Equal(t, expectedLen, utf8.RuneCountInString(line), "line %d length mismatch", i)
		assert.True(t, strings.HasPrefix(line, "|"), "line %d should start with pipe", i)
		assert.True(t, strings.HasSuffix(line, "|"), "line %d should end with pipe", i)
	}

	// Verify delimiter line format (index 1)
	assert.Contains(t, table[1], "---")
}

func TestGitCommit(t *testing.T) {
	commit := util.GitCommit()
	assert.NotEmpty(t, commit)
}

func TestCleanBuild(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	err := os.MkdirAll("build", 0o755)
	require.NoError(t, err)

	dummy := filepath.Join("build", "cargoship_linux_amd64")
	err = os.WriteFile(dummy, []byte("fake binary"), 0o755)
	require.NoError(t, err)

	err = util.CleanBuild()
	require.NoError(t, err)

	_, err = os.Stat(dummy)
	assert.True(t, os.IsNotExist(err), "expected artifact to be cleaned")
}
