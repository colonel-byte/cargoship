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

package layout

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/api"
	v1alpha1 "github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/stretchr/testify/require"
)

func TestDistroLayoutFileName(t *testing.T) {
	tests := []struct {
		name    string
		distro  v1alpha1.Distro
		want    string
		wantErr string
	}{
		{
			name: "single architecture from the scalar field",
			distro: v1alpha1.Distro{
				Metadata: v1alpha1.Metadata{Name: "rancher-rke2", Version: "1.0.0"},
				Build:    v1alpha1.BuildData{Architecture: api.ArchAMD64},
			},
			want: "cargoship-rancher-rke2-amd64-1.0.0.tar.zst",
		},
		{
			name: "single architecture from the list",
			distro: v1alpha1.Distro{
				Metadata: v1alpha1.Metadata{Name: "rancher-rke2", Version: "1.0.0"},
				Build:    v1alpha1.BuildData{Architectures: api.Arches{api.ArchARM64}},
			},
			want: "cargoship-rancher-rke2-arm64-1.0.0.tar.zst",
		},
		{
			name: "several architectures become multi",
			distro: v1alpha1.Distro{
				Metadata: v1alpha1.Metadata{Name: "rancher-rke2", Version: "1.0.0"},
				Build:    v1alpha1.BuildData{Architectures: api.Arches{api.ArchAMD64, api.ArchARM64}},
			},
			want: "cargoship-rancher-rke2-multi-1.0.0.tar.zst",
		},
		{
			name: "uncompressed multi architecture package",
			distro: v1alpha1.Distro{
				Metadata: v1alpha1.Metadata{Name: "rancher-rke2", Uncompressed: true},
				Build:    v1alpha1.BuildData{Architectures: api.Arches{api.ArchAMD64, api.ArchARM64}},
			},
			want: "cargoship-rancher-rke2-multi.tar",
		},
		{
			name:    "no architecture at all is an error",
			distro:  v1alpha1.Distro{Metadata: v1alpha1.Metadata{Name: "rancher-rke2"}},
			wantErr: "must include a build architecture",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewDistroLayout(t.TempDir(), tt.distro).FileName()
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("FileName() = %q, want an error containing %q", got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("FileName() error = %q, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("FileName() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("FileName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestArchiveRemovesANonEmptyIngestDirectory covers the one state ORAS actually leaves this
// directory in. Archive used to call os.Remove on it, which fails on a directory that is not
// empty -- so a create whose image pull left a staged blob behind failed at its last step, after
// every image had been pulled, naming a path and no remedy.
func TestArchiveRemovesANonEmptyIngestDirectory(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		files []string
	}{
		{
			name:  "a blob ORAS staged and did not collect",
			files: []string{"sha256_8bdcb365d0e5cb67fbc76c29e6e0ff69800f862b13694c9d99ef99b9a997025e"},
		},
		{
			name:  "several, from an interrupted pull",
			files: []string{"one", "two", "three"},
		},
		{
			name:  "empty, which always worked",
			files: nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			l := NewDistroLayout(dir, v1alpha1.Distro{
				Metadata: v1alpha1.Metadata{Name: "rancher-rke2", Version: "1.0.0"},
				Build:    v1alpha1.BuildData{Architecture: api.ArchAMD64},
			})

			ingest := filepath.Join(l.GetImageDirPath(), ingestDir)
			require.NoError(t, os.MkdirAll(ingest, 0o750))
			for _, f := range tt.files {
				require.NoError(t, os.WriteFile(filepath.Join(ingest, f), []byte("staged"), 0o600))
			}

			_, err := l.Archive(context.Background(), t.TempDir(), 0)
			require.NoError(t, err)
			require.NoDirExists(t, ingest, "the ingest directory should be gone, blobs and all")
		})
	}
}
