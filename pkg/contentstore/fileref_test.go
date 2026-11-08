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

package contentstore

import (
	"testing"

	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry"
)

// TestFileRef pins the rule a mirror's writer and its reader both apply. Every case here is one
// the other spelling of the rule -- keying on the basename -- gets wrong.
func TestFileRef(t *testing.T) {
	tests := []struct {
		name   string
		target string
		source string
		want   string
	}{
		{
			name:   "a plain URL takes its basename and a digest of the whole source",
			target: "harbor.internal",
			source: "https://get.rke2.io/install.sh",
			want:   "harbor.internal/files/install-sh:d4b7535d9ac2",
		},
		{
			name:   "a repository prefix is kept",
			target: "harbor.internal/cargoship",
			source: "https://get.rke2.io/install.sh",
			want:   "harbor.internal/cargoship/files/install-sh:d4b7535d9ac2",
		},
		{
			name:   "a trailing slash on the target is not doubled",
			target: "harbor.internal/cargoship/",
			source: "https://get.rke2.io/install.sh",
			want:   "harbor.internal/cargoship/files/install-sh:d4b7535d9ac2",
		},
		{
			name:   "an uppercase basename is lowercased, which a repository name requires",
			target: "harbor.internal",
			source: "https://example.com/Install.SH",
			want:   "harbor.internal/files/install-sh:e99030c3f754",
		},
		{
			name:   "a query string is not part of the name",
			target: "harbor.internal",
			source: "https://example.com/get.sh?token=abc",
			want:   "harbor.internal/files/get-sh:9155ee3001ea",
		},
		{
			name:   "a basename that slugs away to nothing still gets a reference",
			target: "harbor.internal",
			source: "https://example.com/___",
			want:   "harbor.internal/files/file:31acca785e5c",
		},
		{
			name:   "a local path works the same way",
			target: "harbor.internal",
			source: "/srv/staged/rke2-images.linux-amd64.tar.zst",
			want:   "harbor.internal/files/rke2-images-linux-amd64-tar-zst:95f1256ff1e8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FileRef(tt.target, tt.source)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)

			// Whatever the rule produces has to be something a registry will accept, or the
			// push fails at the far end of a build nobody wants to repeat.
			_, err = registry.ParseReference(got)
			require.NoError(t, err, "the reference must be valid for a registry")
		})
	}
}

// TestFileRefIsStableAndCollisionFree covers the two properties the rule exists for. A writer and
// a reader run in different processes, so stability is correctness, not tidiness.
func TestFileRefIsStableAndCollisionFree(t *testing.T) {
	t.Run("the same source always gives the same reference", func(t *testing.T) {
		first, err := FileRef("harbor.internal", "https://get.rke2.io/install.sh")
		require.NoError(t, err)
		second, err := FileRef("harbor.internal", "https://get.rke2.io/install.sh")
		require.NoError(t, err)
		require.Equal(t, first, second)
	})

	t.Run("two releases sharing a basename do not collide", func(t *testing.T) {
		// This is the case that rules out keying on the basename. Both of these are named
		// rke2.linux-amd64.tar.gz, and serving one for the other would install the wrong
		// version with no error anywhere.
		v1, err := FileRef("harbor.internal", "https://github.com/rancher/rke2/releases/download/v1.37.1%2Brke2r1/rke2.linux-amd64.tar.gz")
		require.NoError(t, err)
		v2, err := FileRef("harbor.internal", "https://github.com/rancher/rke2/releases/download/v1.38.0%2Brke2r1/rke2.linux-amd64.tar.gz")
		require.NoError(t, err)

		require.NotEqual(t, v1, v2, "two versions of one filename must not share a reference")
	})

	t.Run("a different target moves the reference and nothing else", func(t *testing.T) {
		a, err := FileRef("harbor.internal", "https://get.rke2.io/install.sh")
		require.NoError(t, err)
		b, err := FileRef("zot.internal", "https://get.rke2.io/install.sh")
		require.NoError(t, err)

		require.Equal(t, "harbor.internal/files/install-sh:d4b7535d9ac2", a)
		require.Equal(t, "zot.internal/files/install-sh:d4b7535d9ac2", b)
	})
}

func TestFileRefRejectsEmptyInput(t *testing.T) {
	_, err := FileRef("", "https://get.rke2.io/install.sh")
	require.ErrorContains(t, err, "target registry is required")

	_, err = FileRef("harbor.internal", "")
	require.ErrorContains(t, err, "no source")
}

// A bundle records a registry reference bare and a definition has to write oci:// in front of
// it, so the two spellings of one artifact differ by exactly that prefix. Everything else is
// part of what the bundle recorded and is left alone.
func TestCanonicalFileRef(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "an oci reference loses the scheme a definition must write",
			src:  "oci://registry.example.com/vendor/engine:v1.37.1",
			want: "registry.example.com/vendor/engine:v1.37.1",
		},
		{
			name: "a reference pinned by digest",
			src:  "oci://registry.example.com/vendor/engine@sha256:abc",
			want: "registry.example.com/vendor/engine@sha256:abc",
		},
		{
			name: "a reference a bundle already recorded bare",
			src:  "registry.example.com/vendor/engine:v1.37.1",
			want: "registry.example.com/vendor/engine:v1.37.1",
		},
		{
			name: "an https URL keeps its scheme, which the bundle recorded",
			src:  "https://get.rke2.io/install.sh",
			want: "https://get.rke2.io/install.sh",
		},
		{
			name: "a local path",
			src:  "./rke2.linux-amd64.tar.gz",
			want: "./rke2.linux-amd64.tar.gz",
		},
		{
			name: "no source at all",
			src:  "",
			want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanonicalFileRef(tc.src); got != tc.want {
				t.Errorf("CanonicalFileRef(%q) = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}
