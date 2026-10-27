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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommittedAssetsLookupAndStore(t *testing.T) {
	c := &exampleAssets{
		assets: map[string][]string{},
	}

	url := "https://example.com/assets.txt"
	lines := []string{"asset1:v1", "asset2:v2"}

	_, ok := c.lookup(url)
	require.False(t, ok)

	c.store(url, lines)
	require.True(t, c.dirty)

	got, ok := c.lookup(url)
	require.True(t, ok)
	require.Equal(t, lines, got)

	// storing unchanged lines does not mark dirty again
	c.dirty = false
	c.store(url, lines)
	require.False(t, c.dirty)
}

func TestCommittedAssetsSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "assets.json")

	c := &exampleAssets{
		assets: map[string][]string{
			"https://example.com/b.txt": {"b:1"},
			"https://example.com/a.txt": {"a:1", "a:2"},
		},
		dirty: true,
	}

	require.NoError(t, c.saveTo(path))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "https://example.com/a.txt")

	loaded := loadCommittedAssetsFrom(path)
	gotA, ok := loaded.lookup("https://example.com/a.txt")
	require.True(t, ok)
	require.Equal(t, []string{"a:1", "a:2"}, gotA)
}

func TestMissingFrom(t *testing.T) {
	tests := []struct {
		name string
		have []string
		from []string
		want []string
	}{
		{
			name: "identical lists",
			have: []string{"a", "b"},
			from: []string{"a", "b"},
			want: nil,
		},
		{
			name: "from has an entry have does not",
			have: []string{"a"},
			from: []string{"a", "b"},
			want: []string{"b"},
		},
		{
			name: "keeps from's order rather than sorting",
			have: []string{"b"},
			from: []string{"c", "b", "a"},
			want: []string{"c", "a"},
		},
		{
			name: "reordered lists have nothing missing either way",
			have: []string{"b", "a"},
			from: []string{"a", "b"},
			want: nil,
		},
		{
			name: "empty have reports all of from",
			have: nil,
			from: []string{"a"},
			want: []string{"a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, missingFrom(tt.have, tt.from))
		})
	}
}
