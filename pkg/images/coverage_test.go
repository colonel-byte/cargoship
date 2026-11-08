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

package images

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content/oci"
)

// pushIndexOf pushes an index over one child per platform given, and returns its descriptor
// and bytes -- which is what resolving a reference against a store hands back.
func pushIndexOf(ctx context.Context, t *testing.T, store *oci.Store, platforms ...ocispec.Platform) (ocispec.Descriptor, []byte) {
	t.Helper()

	children := make([]ocispec.Descriptor, 0, len(platforms))
	for _, p := range platforms {
		child := pushManifest(ctx, t, store, p.Architecture)
		platform := p
		child.Platform = &platform
		children = append(children, child)
	}
	indexBytes, err := json.Marshal(ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: children,
	})
	if err != nil {
		t.Fatalf("failed to marshal the index: %v", err)
	}
	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageIndex,
		Digest:    digest.FromBytes(indexBytes),
		Size:      int64(len(indexBytes)),
	}
	if err := store.Push(ctx, desc, bytes.NewReader(indexBytes)); err != nil {
		t.Fatalf("failed to push the index: %v", err)
	}
	return desc, indexBytes
}

func TestCoverSource(t *testing.T) {
	ctx := context.Background()
	linux := func(arch string) ocispec.Platform {
		return ocispec.Platform{OS: "linux", Architecture: arch}
	}

	for _, tc := range []struct {
		name        string
		build       func(*oci.Store) (ocispec.Descriptor, []byte)
		want        []ocispec.Platform
		complete    bool
		wantMissing []string
		wantHeld    string
	}{
		{
			name: "an index covering everything asked for",
			build: func(s *oci.Store) (ocispec.Descriptor, []byte) {
				return pushIndexOf(ctx, t, s, linux("amd64"), linux("arm64"))
			},
			want:     []ocispec.Platform{linux("amd64"), linux("arm64")},
			complete: true,
		},
		{
			name: "an index missing one architecture",
			build: func(s *oci.Store) (ocispec.Descriptor, []byte) {
				return pushIndexOf(ctx, t, s, linux("amd64"))
			},
			want:        []ocispec.Platform{linux("amd64"), linux("arm64")},
			wantMissing: []string{"arm64"},
			wantHeld:    "amd64",
		},
		{
			name: "a single manifest asked for two architectures",
			build: func(s *oci.Store) (ocispec.Descriptor, []byte) {
				desc := pushManifest(ctx, t, s, "amd64")
				return desc, nil
			},
			want:        []ocispec.Platform{linux("amd64"), linux("arm64")},
			wantMissing: []string{"arm64"},
			wantHeld:    "amd64",
		},
		{
			name: "a single manifest asked only for what it is",
			build: func(s *oci.Store) (ocispec.Descriptor, []byte) {
				desc := pushManifest(ctx, t, s, "arm64")
				return desc, nil
			},
			want:     []ocispec.Platform{linux("arm64")},
			complete: true,
		},
		{
			name: "a child for an operating system the pull never asks for",
			build: func(s *oci.Store) (ocispec.Descriptor, []byte) {
				return pushIndexOf(ctx, t, s, ocispec.Platform{OS: "windows", Architecture: "amd64"})
			},
			want:        []ocispec.Platform{linux("amd64")},
			wantMissing: []string{"amd64"},
			wantHeld:    "no architecture",
		},
		{
			name: "an artifact naming no architecture covers whatever is asked",
			build: func(s *oci.Store) (ocispec.Descriptor, []byte) {
				return pushArtifactWithoutArchitecture(ctx, t, s), nil
			},
			want:     []ocispec.Platform{linux("amd64"), linux("arm64")},
			complete: true,
		},
		{
			name: "a build naming no architecture at all",
			build: func(s *oci.Store) (ocispec.Descriptor, []byte) {
				desc := pushManifest(ctx, t, s, "amd64")
				return desc, nil
			},
			want:     platformsForArches(nil),
			complete: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newStore(ctx, t)
			desc, body := tc.build(store)

			coverage, err := coverSource(ctx, store, desc, body, tc.want)
			if err != nil {
				t.Fatalf("coverSource: %v", err)
			}
			if coverage.complete() != tc.complete {
				t.Fatalf("complete() = %v, want %v (missing %v)", coverage.complete(), tc.complete, coverage.missing)
			}
			if tc.complete {
				return
			}
			if len(coverage.missing) != len(tc.wantMissing) {
				t.Fatalf("missing = %v, want %v", coverage.missing, tc.wantMissing)
			}
			for i := range coverage.missing {
				if coverage.missing[i] != tc.wantMissing[i] {
					t.Errorf("missing[%d] = %q, want %q", i, coverage.missing[i], tc.wantMissing[i])
				}
			}
			if got := coverage.describe(); got != tc.wantHeld {
				t.Errorf("describe() = %q, want %q", got, tc.wantHeld)
			}
		})
	}
}
