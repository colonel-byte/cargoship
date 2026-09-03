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
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/src/api"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
	distro "github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
)

func TestResolveArchitectures(t *testing.T) {
	tests := []struct {
		name      string
		metadata  distro.ZarfDistroMetadata
		requested string
		want      api.Arches
		wantErr   string
	}{
		{
			name:     "scalar architecture is left alone",
			metadata: distro.ZarfDistroMetadata{Architecture: "arm64"},
			want:     api.Arches{"arm64"},
		},
		{
			name:     "unset architecture falls back to the host",
			metadata: distro.ZarfDistroMetadata{},
			want:     api.Arches{api.Arch(runtime.GOARCH)},
		},
		{
			name:      "a request fills in an unset architecture",
			metadata:  distro.ZarfDistroMetadata{},
			requested: "arm64",
			want:      api.Arches{"arm64"},
		},
		{
			name:      "a request matching the scalar architecture is accepted",
			metadata:  distro.ZarfDistroMetadata{Architecture: "arm64"},
			requested: "arm64",
			want:      api.Arches{"arm64"},
		},
		{
			name:      "a request conflicting with the scalar architecture is an error",
			metadata:  distro.ZarfDistroMetadata{Architecture: "amd64"},
			requested: "arm64",
			wantErr:   "not targeted by this package, which targets amd64",
		},
		{
			name:      "an unsupported request is rejected for a scalar architecture",
			metadata:  distro.ZarfDistroMetadata{Architecture: "amd64"},
			requested: "x86_64",
			wantErr:   "invalid platform operating system architecture",
		},
		{
			name:     "a list is taken at its word",
			metadata: distro.ZarfDistroMetadata{Architectures: api.Arches{"amd64", "arm64"}},
			want:     api.Arches{"amd64", "arm64"},
		},
		{
			name:      "a request narrows a list",
			metadata:  distro.ZarfDistroMetadata{Architectures: api.Arches{"amd64", "arm64"}},
			requested: "arm64",
			want:      api.Arches{"arm64"},
		},
		{
			name:      "an unsupported request is rejected",
			metadata:  distro.ZarfDistroMetadata{Architectures: api.Arches{"amd64", "arm64"}},
			requested: "x86_64",
			wantErr:   "invalid platform operating system architecture",
		},
		{
			name:      "a request outside the list is an error",
			metadata:  distro.ZarfDistroMetadata{Architectures: api.Arches{"amd64"}},
			requested: "arm64",
			wantErr:   "not targeted by this package",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveArchitectures(distro.ZarfDistro{Metadata: tt.metadata}, tt.requested)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("resolveArchitectures() = nil error, want one containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveArchitectures() error = %q, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveArchitectures() unexpected error: %v", err)
			}
			if arches := got.Metadata.Arches(); !slices.Equal(arches, tt.want) {
				t.Errorf("architectures = %v, want %v", arches, tt.want)
			}
		})
	}
}

func TestValidateArchitectures(t *testing.T) {
	withFiles := func(metadata distro.ZarfDistroMetadata, files, osFiles v1alpha1.ZarfFiles) distro.ZarfDistro {
		return distro.ZarfDistro{
			Metadata: metadata,
			Spec: distro.ZarfDistroSpec{
				Config: distro.ZarfDistroConfig{
					Files: files,
					OS:    distro.ZarfDistroOS{Files: osFiles},
				},
			},
		}
	}
	archFile := func(target string, arch ...api.Arch) *v1alpha1.ZarfFile {
		return &v1alpha1.ZarfFile{Target: target, Selector: v1alpha1.BinarySelector{Arch: arch}}
	}

	tests := []struct {
		name    string
		distro  distro.ZarfDistro
		wantErr string
	}{
		{
			name: "a multi architecture package with matching selectors",
			distro: withFiles(
				distro.ZarfDistroMetadata{Architectures: api.Arches{"amd64", "arm64"}},
				v1alpha1.ZarfFiles{archFile("/opt/a", "amd64"), archFile("/opt/b")},
				v1alpha1.ZarfFiles{archFile("/opt/c", "arm64")},
			),
		},
		{
			name:   "a single architecture package",
			distro: withFiles(distro.ZarfDistroMetadata{Architecture: "amd64"}, nil, nil),
		},
		{
			name:    "no architecture at all",
			distro:  withFiles(distro.ZarfDistroMetadata{}, nil, nil),
			wantErr: "at least one architecture",
		},
		{
			name:    "an unsupported architecture",
			distro:  withFiles(distro.ZarfDistroMetadata{Architectures: api.Arches{"x86_64"}}, nil, nil),
			wantErr: "invalid platform operating system architecture",
		},
		{
			name:    "a duplicated architecture",
			distro:  withFiles(distro.ZarfDistroMetadata{Architectures: api.Arches{"amd64", "amd64"}}, nil, nil),
			wantErr: "listed more than once",
		},
		{
			name: "a file selecting an architecture the package does not target",
			distro: withFiles(
				distro.ZarfDistroMetadata{Architectures: api.Arches{"amd64"}},
				v1alpha1.ZarfFiles{archFile("/opt/a", "aarch64")},
				nil,
			),
			wantErr: "but the package targets amd64",
		},
		{
			name: "an os file selecting an architecture the package does not target",
			distro: withFiles(
				distro.ZarfDistroMetadata{Architectures: api.Arches{"amd64"}},
				nil,
				v1alpha1.ZarfFiles{archFile("/opt/c", "arm64")},
			),
			wantErr: "but the package targets amd64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateArchitectures(tt.distro)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateArchitectures() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateArchitectures() = nil error, want one containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("validateArchitectures() error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
