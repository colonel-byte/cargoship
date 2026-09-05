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
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
)

// pushBlob writes bytes into the store and returns the descriptor pointing at them.
func pushBlob(ctx context.Context, t *testing.T, store *oci.Store, mediaType string, b []byte) ocispec.Descriptor {
	t.Helper()

	desc := ocispec.Descriptor{
		MediaType: mediaType,
		Digest:    digest.FromBytes(b),
		Size:      int64(len(b)),
	}
	if err := store.Push(ctx, desc, bytes.NewReader(b)); err != nil {
		t.Fatalf("failed to push %s: %v", mediaType, err)
	}
	return desc
}

// pushManifest writes a layerless image manifest for arch into the store and returns its descriptor
// without a platform, which is what a registry serving a bare manifest gives us.
func pushManifest(ctx context.Context, t *testing.T, store *oci.Store, arch string) ocispec.Descriptor {
	t.Helper()

	configBytes, err := json.Marshal(ocispec.Image{
		Platform: ocispec.Platform{OS: "linux", Architecture: arch},
	})
	if err != nil {
		t.Fatalf("failed to marshal the image config: %v", err)
	}
	configDesc := pushBlob(ctx, t, store, ocispec.MediaTypeImageConfig, configBytes)

	manifestBytes, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ocispec.Descriptor{},
	})
	if err != nil {
		t.Fatalf("failed to marshal the image manifest: %v", err)
	}
	return pushBlob(ctx, t, store, ocispec.MediaTypeImageManifest, manifestBytes)
}

func newStore(ctx context.Context, t *testing.T) *oci.Store {
	t.Helper()

	store, err := oci.NewWithContext(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("failed to create the store: %v", err)
	}
	return store
}

func TestPlatformsForArches(t *testing.T) {
	tests := []struct {
		name   string
		arches []string
		want   []ocispec.Platform
	}{
		{
			name: "no architectures keeps a single unset platform",
			want: []ocispec.Platform{{OS: "linux"}},
		},
		{
			name:   "one architecture",
			arches: []string{"amd64"},
			want:   []ocispec.Platform{{OS: "linux", Architecture: "amd64"}},
		},
		{
			name:   "several architectures keep their order",
			arches: []string{"arm64", "amd64"},
			want: []ocispec.Platform{
				{OS: "linux", Architecture: "arm64"},
				{OS: "linux", Architecture: "amd64"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := platformsForArches(tt.arches)
			if len(got) != len(tt.want) {
				t.Fatalf("platformsForArches(%v) returned %d platforms, want %d", tt.arches, len(got), len(tt.want))
			}
			for i := range got {
				if !reflect.DeepEqual(got[i], tt.want[i]) {
					t.Errorf("platform %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestTagImageSingleManifestTagsTheManifest(t *testing.T) {
	ctx := context.Background()
	store := newStore(ctx, t)
	manifestDesc := pushManifest(ctx, t, store, "amd64")

	if err := tagImage(ctx, store, "registry.example.com/pause:3.10", []ocispec.Descriptor{manifestDesc}); err != nil {
		t.Fatalf("tagImage() unexpected error: %v", err)
	}

	resolved, err := store.Resolve(ctx, "registry.example.com/pause:3.10")
	if err != nil {
		t.Fatalf("failed to resolve the tag: %v", err)
	}
	if resolved.MediaType != ocispec.MediaTypeImageManifest {
		t.Errorf("resolved media type = %q, want %q", resolved.MediaType, ocispec.MediaTypeImageManifest)
	}
	if resolved.Digest != manifestDesc.Digest {
		t.Errorf("resolved digest = %q, want %q", resolved.Digest, manifestDesc.Digest)
	}
}

func TestTagImageSeveralManifestsTagsAnIndex(t *testing.T) {
	ctx := context.Background()
	store := newStore(ctx, t)
	// arm64 is pushed first so the ordering in the index cannot come from the pull order.
	arm64Desc := pushManifest(ctx, t, store, "arm64")
	amd64Desc := pushManifest(ctx, t, store, "amd64")

	const ref = "registry.example.com/pause:3.10"
	if err := tagImage(ctx, store, ref, []ocispec.Descriptor{arm64Desc, amd64Desc}); err != nil {
		t.Fatalf("tagImage() unexpected error: %v", err)
	}

	resolved, err := store.Resolve(ctx, ref)
	if err != nil {
		t.Fatalf("failed to resolve the tag: %v", err)
	}
	if resolved.MediaType != ocispec.MediaTypeImageIndex {
		t.Fatalf("resolved media type = %q, want %q", resolved.MediaType, ocispec.MediaTypeImageIndex)
	}
	if resolved.Annotations[ocispec.AnnotationBaseImageName] != ref {
		t.Errorf("index annotation = %q, want %q", resolved.Annotations[ocispec.AnnotationBaseImageName], ref)
	}

	indexBytes, err := content.FetchAll(ctx, store, resolved)
	if err != nil {
		t.Fatalf("failed to read the index: %v", err)
	}
	var index ocispec.Index
	if err := json.Unmarshal(indexBytes, &index); err != nil {
		t.Fatalf("failed to parse the index: %v", err)
	}
	if len(index.Manifests) != 2 {
		t.Fatalf("index holds %d manifests, want 2", len(index.Manifests))
	}
	if index.Manifests[0].Digest != amd64Desc.Digest {
		t.Errorf("first manifest = %q, want the amd64 manifest %q", index.Manifests[0].Digest, amd64Desc.Digest)
	}
	for _, manifest := range index.Manifests {
		if manifest.Platform == nil {
			t.Fatalf("manifest %s has no platform", manifest.Digest)
		}
		if manifest.Platform.OS != "linux" {
			t.Errorf("manifest %s has os %q, want linux", manifest.Digest, manifest.Platform.OS)
		}
	}
	if index.Manifests[1].Platform.Architecture != "arm64" {
		t.Errorf("second manifest architecture = %q, want arm64", index.Manifests[1].Platform.Architecture)
	}
}

func TestTagImageIndexIsReproducible(t *testing.T) {
	ctx := context.Background()
	const ref = "registry.example.com/pause:3.10"

	digests := make([]digest.Digest, 2)
	for i := range digests {
		store := newStore(ctx, t)
		amd64Desc := pushManifest(ctx, t, store, "amd64")
		arm64Desc := pushManifest(ctx, t, store, "arm64")

		descs := []ocispec.Descriptor{amd64Desc, arm64Desc}
		if i == 1 {
			// The second package pulls the same two manifests in the other order.
			descs = []ocispec.Descriptor{arm64Desc, amd64Desc}
		}
		if err := tagImage(ctx, store, ref, descs); err != nil {
			t.Fatalf("tagImage() unexpected error: %v", err)
		}
		resolved, err := store.Resolve(ctx, ref)
		if err != nil {
			t.Fatalf("failed to resolve the tag: %v", err)
		}
		digests[i] = resolved.Digest
	}

	if digests[0] != digests[1] {
		t.Errorf("index digests differ by pull order: %q and %q", digests[0], digests[1])
	}
}

func TestTagImageWithoutManifestsErrors(t *testing.T) {
	ctx := context.Background()
	store := newStore(ctx, t)

	if err := tagImage(ctx, store, "registry.example.com/pause:3.10", nil); err == nil {
		t.Error("tagImage() with no manifests returned no error, want one")
	}
}

func TestEnsurePlatform(t *testing.T) {
	ctx := context.Background()
	store := newStore(ctx, t)
	manifestDesc := pushManifest(ctx, t, store, "arm64")

	t.Run("a descriptor without a platform is filled in from the config", func(t *testing.T) {
		got, err := ensurePlatform(ctx, store, manifestDesc)
		if err != nil {
			t.Fatalf("ensurePlatform() unexpected error: %v", err)
		}
		if got.Platform == nil {
			t.Fatal("ensurePlatform() returned no platform")
		}
		if got.Platform.Architecture != "arm64" || got.Platform.OS != "linux" {
			t.Errorf("platform = %+v, want linux/arm64", *got.Platform)
		}
	})

	t.Run("a descriptor that already has a platform is left alone", func(t *testing.T) {
		desc := manifestDesc
		desc.Platform = &ocispec.Platform{OS: "linux", Architecture: "riscv64"}

		got, err := ensurePlatform(ctx, store, desc)
		if err != nil {
			t.Fatalf("ensurePlatform() unexpected error: %v", err)
		}
		if got.Platform.Architecture != "riscv64" {
			t.Errorf("platform architecture = %q, want riscv64", got.Platform.Architecture)
		}
	})
}

func TestPlatformKey(t *testing.T) {
	tests := []struct {
		name     string
		platform *ocispec.Platform
		want     string
	}{
		{
			name: "no platform",
			want: "",
		},
		{
			name:     "os and architecture",
			platform: &ocispec.Platform{OS: "linux", Architecture: "amd64"},
			want:     "linux/amd64/",
		},
		{
			name:     "a variant orders after the plain architecture",
			platform: &ocispec.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"},
			want:     "linux/arm64/v8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := platformKey(tt.platform); got != tt.want {
				t.Errorf("platformKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

// tagAll pushes one manifest per architecture into a fresh store in dir and tags each one, in the
// order given, which is how a pull leaves the layout on disk.
func tagAll(ctx context.Context, t *testing.T, dir string, arches []string) {
	t.Helper()

	store, err := oci.NewWithContext(ctx, dir)
	if err != nil {
		t.Fatalf("failed to create the store: %v", err)
	}
	for _, arch := range arches {
		desc := pushManifest(ctx, t, store, arch)
		if err := store.Tag(ctx, desc, "example.com/image:"+arch); err != nil {
			t.Fatalf("failed to tag %s: %v", arch, err)
		}
	}
}

func TestSortIndexFileOrdersEntriesByDigest(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	tagAll(ctx, t, dir, []string{"amd64", "arm64", "s390x"})

	path := filepath.Join(dir, ocispec.ImageIndexFile)
	if err := sortIndexFile(path); err != nil {
		t.Fatalf("sortIndexFile() unexpected error: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read the index: %v", err)
	}
	var index ocispec.Index
	if err := json.Unmarshal(b, &index); err != nil {
		t.Fatalf("failed to unmarshal the index: %v", err)
	}

	if index.SchemaVersion != 2 {
		t.Errorf("schemaVersion = %d, want 2", index.SchemaVersion)
	}
	if index.MediaType != ocispec.MediaTypeImageIndex {
		t.Errorf("mediaType = %q, want %q", index.MediaType, ocispec.MediaTypeImageIndex)
	}
	// Oras records every manifest twice, once under its tag and once under its digest, but only the
	// tagged entry keeps the reference annotation.
	if len(index.Manifests) != 3 {
		t.Fatalf("index holds %d manifests, want 3", len(index.Manifests))
	}
	for i := 1; i < len(index.Manifests); i++ {
		if index.Manifests[i-1].Digest > index.Manifests[i].Digest {
			t.Errorf("manifest %d has digest %s, which sorts before %s", i, index.Manifests[i].Digest, index.Manifests[i-1].Digest)
		}
	}
	for _, desc := range index.Manifests {
		if desc.Annotations[ocispec.AnnotationRefName] == "" {
			t.Errorf("manifest %s lost its reference annotation", desc.Digest)
		}
	}
}

func TestSortIndexFileIsIndependentOfTagOrder(t *testing.T) {
	ctx := context.Background()
	read := func(arches []string) []byte {
		dir := t.TempDir()
		tagAll(ctx, t, dir, arches)
		path := filepath.Join(dir, ocispec.ImageIndexFile)
		if err := sortIndexFile(path); err != nil {
			t.Fatalf("sortIndexFile() unexpected error: %v", err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read the index: %v", err)
		}
		return b
	}

	first := read([]string{"amd64", "arm64", "s390x"})
	second := read([]string{"s390x", "amd64", "arm64"})
	if !bytes.Equal(first, second) {
		t.Errorf("index.json differs between two pulls of the same images:\n%s\n%s", first, second)
	}
}

func TestSortIndexFileWithoutAnIndexErrors(t *testing.T) {
	if err := sortIndexFile(filepath.Join(t.TempDir(), ocispec.ImageIndexFile)); err == nil {
		t.Error("sortIndexFile() = nil error, want one for a missing index")
	}
}
