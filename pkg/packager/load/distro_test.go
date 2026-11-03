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

	"github.com/colonel-byte/cargoship/api"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1"
	distro "github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
)

func TestResolveArchitectures(t *testing.T) {
	tests := []struct {
		name      string
		metadata  distro.Metadata
		requested string
		want      api.Arches
		// wantDeclared is what the definition listed before narrowing. Cases that leave it empty
		// do not check it.
		wantDeclared api.Arches
		wantErr      string
	}{
		{
			name:     "scalar architecture is left alone",
			metadata: distro.Metadata{Architecture: "arm64"},
			want:     api.Arches{"arm64"},
		},
		{
			name:     "unset architecture falls back to the host",
			metadata: distro.Metadata{},
			want:     api.Arches{api.Arch(runtime.GOARCH)},
		},
		{
			name:      "a request fills in an unset architecture",
			metadata:  distro.Metadata{},
			requested: "arm64",
			want:      api.Arches{"arm64"},
		},
		{
			name:      "a request matching the scalar architecture is accepted",
			metadata:  distro.Metadata{Architecture: "arm64"},
			requested: "arm64",
			want:      api.Arches{"arm64"},
		},
		{
			name:      "a request conflicting with the scalar architecture is an error",
			metadata:  distro.Metadata{Architecture: "amd64"},
			requested: "arm64",
			wantErr:   "not targeted by this package, which targets amd64",
		},
		{
			name:      "an unsupported request is rejected for a scalar architecture",
			metadata:  distro.Metadata{Architecture: "amd64"},
			requested: "x86_64",
			wantErr:   "invalid platform operating system architecture",
		},
		{
			name:     "a list is taken at its word",
			metadata: distro.Metadata{Architectures: api.Arches{"amd64", "arm64"}},
			want:     api.Arches{"amd64", "arm64"},
		},
		{
			name:         "a request narrows a list but keeps what was declared",
			metadata:     distro.Metadata{Architectures: api.Arches{"amd64", "arm64"}},
			requested:    "arm64",
			want:         api.Arches{"arm64"},
			wantDeclared: api.Arches{"amd64", "arm64"},
		},
		{
			name:      "an unsupported request is rejected",
			metadata:  distro.Metadata{Architectures: api.Arches{"amd64", "arm64"}},
			requested: "x86_64",
			wantErr:   "invalid platform operating system architecture",
		},
		{
			name:      "a request outside the list is an error",
			metadata:  distro.Metadata{Architectures: api.Arches{"amd64"}},
			requested: "arm64",
			wantErr:   "not targeted by this package",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, declared, err := resolveArchitectures(distro.Distro{Metadata: tt.metadata}, tt.requested)
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
			if tt.wantDeclared != nil && !slices.Equal(declared, tt.wantDeclared) {
				t.Errorf("declared architectures = %v, want %v", declared, tt.wantDeclared)
			}
		})
	}
}

func TestValidateArchitectures(t *testing.T) {
	withFiles := func(metadata distro.Metadata, files, osFiles v1alpha1.Files) distro.Distro {
		return distro.Distro{
			Metadata: metadata,
			Spec: distro.Spec{
				Config: distro.Config{
					Files: files,
					OS:    distro.OS{Files: osFiles},
				},
			},
		}
	}
	archFile := func(target string, arch ...api.Arch) *v1alpha1.File {
		return &v1alpha1.File{Target: target, Selector: v1alpha1.BinarySelector{Arch: arch}}
	}

	tests := []struct {
		name string
		// declared is what the definition listed before any narrowing. It defaults to what the
		// distro itself targets when a case leaves it empty.
		declared api.Arches
		distro   distro.Distro
		wantErr  string
	}{
		{
			name: "a multi architecture package with matching selectors",
			distro: withFiles(
				distro.Metadata{Architectures: api.Arches{"amd64", "arm64"}},
				v1alpha1.Files{archFile("/opt/a", "amd64"), archFile("/opt/b")},
				v1alpha1.Files{archFile("/opt/c", "arm64")},
			),
		},
		{
			name:   "a single architecture package",
			distro: withFiles(distro.Metadata{Architecture: "amd64"}, nil, nil),
		},
		{
			name:    "no architecture at all",
			distro:  withFiles(distro.Metadata{}, nil, nil),
			wantErr: "at least one architecture",
		},
		{
			name:    "an unsupported architecture",
			distro:  withFiles(distro.Metadata{Architectures: api.Arches{"x86_64"}}, nil, nil),
			wantErr: "invalid platform operating system architecture",
		},
		{
			name:    "a duplicated architecture",
			distro:  withFiles(distro.Metadata{Architectures: api.Arches{"amd64", "amd64"}}, nil, nil),
			wantErr: "listed more than once",
		},
		{
			name: "a file selecting an architecture the package does not target",
			distro: withFiles(
				distro.Metadata{Architectures: api.Arches{"amd64"}},
				v1alpha1.Files{archFile("/opt/a", "aarch64")},
				nil,
			),
			wantErr: "but the package declares amd64",
		},
		{
			name: "a package narrowed to one architecture keeps the other's selectors",
			distro: withFiles(
				distro.Metadata{Architectures: api.Arches{"amd64"}},
				v1alpha1.Files{archFile("/opt/a", "amd64"), archFile("/opt/b", "arm64")},
				nil,
			),
			declared: api.Arches{"amd64", "arm64"},
		},
		{
			name: "an os file selecting an architecture the package does not target",
			distro: withFiles(
				distro.Metadata{Architectures: api.Arches{"amd64"}},
				nil,
				v1alpha1.Files{archFile("/opt/c", "arm64")},
			),
			wantErr: "but the package declares amd64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			declared := tt.declared
			if declared == nil {
				declared = tt.distro.Metadata.Arches()
			}
			err := validateArchitectures(tt.distro, declared)
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
