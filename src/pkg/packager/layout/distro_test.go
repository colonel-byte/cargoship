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
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/src/api"
	v1alpha1 "github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
)

func TestDistroLayoutFileName(t *testing.T) {
	tests := []struct {
		name    string
		distro  v1alpha1.ZarfDistro
		want    string
		wantErr string
	}{
		{
			name: "single architecture from the scalar field",
			distro: v1alpha1.ZarfDistro{
				Metadata: v1alpha1.ZarfDistroMetadata{Name: "rancher-rke2", Version: "1.0.0"},
				Build:    v1alpha1.ZarfDistroBuildData{Architecture: api.ArchAMD64},
			},
			want: "cargoship-rancher-rke2-amd64-1.0.0.tar.zst",
		},
		{
			name: "single architecture from the list",
			distro: v1alpha1.ZarfDistro{
				Metadata: v1alpha1.ZarfDistroMetadata{Name: "rancher-rke2", Version: "1.0.0"},
				Build:    v1alpha1.ZarfDistroBuildData{Architectures: api.Arches{api.ArchARM64}},
			},
			want: "cargoship-rancher-rke2-arm64-1.0.0.tar.zst",
		},
		{
			name: "several architectures become multi",
			distro: v1alpha1.ZarfDistro{
				Metadata: v1alpha1.ZarfDistroMetadata{Name: "rancher-rke2", Version: "1.0.0"},
				Build:    v1alpha1.ZarfDistroBuildData{Architectures: api.Arches{api.ArchAMD64, api.ArchARM64}},
			},
			want: "cargoship-rancher-rke2-multi-1.0.0.tar.zst",
		},
		{
			name: "uncompressed multi architecture package",
			distro: v1alpha1.ZarfDistro{
				Metadata: v1alpha1.ZarfDistroMetadata{Name: "rancher-rke2", Uncompressed: true},
				Build:    v1alpha1.ZarfDistroBuildData{Architectures: api.Arches{api.ArchAMD64, api.ArchARM64}},
			},
			want: "cargoship-rancher-rke2-multi.tar",
		},
		{
			name:    "no architecture at all is an error",
			distro:  v1alpha1.ZarfDistro{Metadata: v1alpha1.ZarfDistroMetadata{Name: "rancher-rke2"}},
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
