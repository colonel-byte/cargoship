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

package distro

import (
	"slices"
	"testing"

	"github.com/colonel-byte/cargoship/src/api"
)

func TestMetadataArches(t *testing.T) {
	tests := []struct {
		name string
		meta ZarfDistroMetadata
		want api.Arches
	}{
		{
			name: "list wins over scalar",
			meta: ZarfDistroMetadata{Architecture: "amd64", Architectures: api.Arches{"arm64"}},
			want: api.Arches{"arm64"},
		},
		{
			name: "falls back to scalar",
			meta: ZarfDistroMetadata{Architecture: "amd64"},
			want: api.Arches{"amd64"},
		},
		{
			name: "empty when neither is set",
			meta: ZarfDistroMetadata{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.meta.Arches(); !slices.Equal(got, tt.want) {
				t.Errorf("Arches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildDataArches(t *testing.T) {
	tests := []struct {
		name  string
		build ZarfDistroBuildData
		want  api.Arches
	}{
		{
			name:  "list wins over scalar",
			build: ZarfDistroBuildData{Architecture: "amd64", Architectures: api.Arches{"amd64", "arm64"}},
			want:  api.Arches{"amd64", "arm64"},
		},
		{
			name:  "falls back to scalar",
			build: ZarfDistroBuildData{Architecture: "arm64"},
			want:  api.Arches{"arm64"},
		},
		{
			name:  "empty when neither is set",
			build: ZarfDistroBuildData{},
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.build.Arches(); !slices.Equal(got, tt.want) {
				t.Errorf("Arches() = %v, want %v", got, tt.want)
			}
		})
	}
}
