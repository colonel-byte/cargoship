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

	"github.com/colonel-byte/cargoship/api"
	"github.com/stretchr/testify/require"
)

func TestTarballSuffix(t *testing.T) {
	for _, tt := range []struct {
		name        string
		compression string
		want        string
	}{
		{
			name:        "unset defaults to uncompressed",
			compression: "",
			want:        ".tar",
		},
		{
			name:        "none",
			compression: CompressionNone,
			want:        ".tar",
		},
		{
			name:        "gzip",
			compression: CompressionGzip,
			want:        ".tar.gz",
		},
		{
			name:        "zstd",
			compression: CompressionZstd,
			want:        ".tar.zst",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ImageConfig{Compression: tt.compression}.TarballSuffix()
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTarballSuffixUnsupported(t *testing.T) {
	_, err := ImageConfig{Compression: "bzip2"}.TarballSuffix()
	require.ErrorContains(t, err, `unsupported image compression "bzip2"`)
}

func TestMetadataArches(t *testing.T) {
	tests := []struct {
		name string
		meta Metadata
		want api.Arches
	}{
		{
			name: "list wins over scalar",
			meta: Metadata{Architecture: "amd64", Architectures: api.Arches{"arm64"}},
			want: api.Arches{"arm64"},
		},
		{
			name: "falls back to scalar",
			meta: Metadata{Architecture: "amd64"},
			want: api.Arches{"amd64"},
		},
		{
			name: "empty when neither is set",
			meta: Metadata{},
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
		build BuildData
		want  api.Arches
	}{
		{
			name:  "list wins over scalar",
			build: BuildData{Architecture: "amd64", Architectures: api.Arches{"amd64", "arm64"}},
			want:  api.Arches{"amd64", "arm64"},
		},
		{
			name:  "falls back to scalar",
			build: BuildData{Architecture: "arm64"},
			want:  api.Arches{"arm64"},
		},
		{
			name:  "empty when neither is set",
			build: BuildData{},
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

func TestDistroArches(t *testing.T) {
	tests := []struct {
		name   string
		distro Distro
		want   api.Arches
	}{
		{
			name: "build wins over metadata",
			distro: Distro{
				Metadata: Metadata{Architectures: api.Arches{"amd64", "arm64"}},
				Build:    BuildData{Architectures: api.Arches{"arm64"}},
			},
			want: api.Arches{"arm64"},
		},
		{
			name:   "falls back to metadata when the package is not built yet",
			distro: Distro{Metadata: Metadata{Architecture: "amd64"}},
			want:   api.Arches{"amd64"},
		},
		{
			name:   "empty when neither is set",
			distro: Distro{},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.distro.Arches(); !slices.Equal(got, tt.want) {
				t.Errorf("Arches() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestZarfDistroSELinuxIsZero pins the gate the SELinux phase runs off. Any one of the three
// lists being populated has to make the phase run, and an untouched field has to keep it off.
func TestZarfDistroSELinuxIsZero(t *testing.T) {
	for _, tt := range []struct {
		name    string
		selinux SELinux
		want    bool
	}{
		{
			name:    "nothing configured",
			selinux: SELinux{},
			want:    true,
		},
		{
			name: "a module",
			selinux: SELinux{
				Modules: []SELinuxModule{
					{
						Name: "cargoship-engine",
						CIL:  "(allow kernel_t self (capability (sys_admin)))",
					},
				},
			},
			want: false,
		},
		{
			name: "a boolean",
			selinux: SELinux{
				Booleans: map[string]bool{"container_manage_cgroup": true},
			},
			want: false,
		},
		{
			name: "a file context",
			selinux: SELinux{
				FileContexts: []SELinuxFileContext{
					{
						Path: "/var/lib/rancher(/.*)?",
						Type: "container_var_lib_t",
					},
				},
			},
			want: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.selinux.IsZero())
		})
	}
}
