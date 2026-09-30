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

package engineconfig_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/engineconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTagVersion(t *testing.T) {
	tests := []struct {
		tag     string
		want    [3]int
		wantErr bool
	}{
		{
			tag:     "v1.35.8+k3s1",
			want:    [3]int{1, 35, 8},
			wantErr: false,
		},
		{
			tag:     "v1.36.1-rke2r1",
			want:    [3]int{1, 36, 1},
			wantErr: false,
		},
		{
			tag:     "invalid",
			want:    [3]int{0, 0, 0},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		got, err := engineconfig.TagVersion(tt.tag)
		if tt.wantErr {
			assert.Error(t, err)
		} else {
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		}
	}
}

func TestTagMinor(t *testing.T) {
	minor, err := engineconfig.TagMinor("v1.35.8+k3s1")
	require.NoError(t, err)
	assert.Equal(t, "v1_35", minor)
}

func TestPinDistroSetTagAndSort(t *testing.T) {
	d := engineconfig.PinDistro{
		Name:  "k3s",
		Repo:  "https://github.com/k3s-io/k3s",
		Files: []string{"cmd/server/main.go"},
		Tags: []string{
			"v1.34.1+k3s1",
			"v1.35.0+k3s1",
		},
	}

	// Update existing minor line
	prev, changed, err := d.SetTag("v1.35.2+k3s1")
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, "v1.35.0+k3s1", prev)

	// Idempotent update
	prev, changed, err = d.SetTag("v1.35.2+k3s1")
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, "v1.35.2+k3s1", prev)

	// Add new minor line (should sort newest first)
	prev, changed, err = d.SetTag("v1.36.0+k3s1")
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Empty(t, prev)
	assert.Equal(t, "v1.36.0+k3s1", d.Tags[0])
}

func TestEnginePinsLookupAndPulls(t *testing.T) {
	pins := engineconfig.EnginePins{
		Distros: []engineconfig.PinDistro{
			{
				Name:  "k3s",
				Repo:  "https://github.com/k3s-io/k3s",
				Files: []string{"file1.go"},
				Tags:  []string{"v1.35.1+k3s1"},
			},
		},
	}

	d, err := pins.Distro("k3s")
	require.NoError(t, err)
	assert.Equal(t, "k3s", d.Name)

	_, err = pins.Distro("nonexistent")
	require.Error(t, err)

	pulls, err := pins.Pulls()
	require.NoError(t, err)
	require.Len(t, pulls, 1)
	assert.Equal(t, "v1.35.1+k3s1", pulls[0].Tag)
	assert.Equal(t, filepath.Join(engineconfig.ThirdpartySrcDir, "k3s", "v1_35"), pulls[0].DestDir)
}

func TestReadEnginePinsFromDisk(t *testing.T) {
	t.Chdir("../../../..")

	pins, err := engineconfig.ReadEnginePins()
	require.NoError(t, err)
	assert.NotEmpty(t, pins.Distros)
}

func TestEnginePinsWrite(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	err := os.MkdirAll(filepath.Dir(engineconfig.EnginePinsPath), 0o755)
	require.NoError(t, err)

	pins := engineconfig.EnginePins{
		Distros: []engineconfig.PinDistro{
			{
				Name:  "test-distro",
				Repo:  "https://example.com/repo",
				Tags:  []string{"v1.35.0+test1"},
				Files: []string{"main.go"},
			},
		},
	}

	err = pins.Write()
	require.NoError(t, err)

	readBack, err := engineconfig.ReadEnginePins()
	require.NoError(t, err)
	require.Len(t, readBack.Distros, 1)
	assert.Equal(t, "test-distro", readBack.Distros[0].Name)
}
