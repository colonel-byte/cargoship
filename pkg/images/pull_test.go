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
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/test"
	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/transform"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"
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

// stubSource is a local image source backed by an OCI layout, standing in for a hauler store.
type stubSource struct {
	store *oci.Store
	refs  map[string]bool
}

func (s stubSource) HasImage(ref string) bool    { return s.refs[ref] }
func (s stubSource) Target() oras.ReadOnlyTarget { return s.store }
func (s stubSource) Describe() string            { return "stub source" }

// A pull served entirely by a local source must touch no registry. The reference here points at
// a host that does not resolve, so the test fails if anything reaches for the network.
func TestPullFromLocalSourceNeedsNoRegistry(t *testing.T) {
	ctx := context.Background()
	const ref = "registry.invalid/pause:3.10"

	src := newStore(ctx, t)
	manifestDesc := pushManifest(ctx, t, src, "amd64")
	if err := src.Tag(ctx, manifestDesc, ref); err != nil {
		t.Fatalf("failed to tag the source image: %v", err)
	}

	refInfo, err := transform.ParseImageRef(ref)
	if err != nil {
		t.Fatalf("failed to parse %s: %v", ref, err)
	}

	dest := t.TempDir()
	got, err := Pull(ctx, []transform.Image{refInfo}, dest, PullOptions{
		Arches:         []string{"amd64"},
		CacheDirectory: t.TempDir(),
		Source:         stubSource{store: src, refs: map[string]bool{ref: true}},
	})
	if err != nil {
		t.Fatalf("Pull from a local source: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Pull returned %d images, want 1", len(got))
	}

	// The image must be in the package layout, findable under the reference it was declared
	// with -- that tagging is what lets the package be read back later.
	pkgStore, err := oci.NewWithContext(ctx, dest)
	if err != nil {
		t.Fatalf("failed to open the destination layout: %v", err)
	}
	resolved, err := pkgStore.Resolve(ctx, ref)
	if err != nil {
		t.Fatalf("destination does not hold %s: %v", ref, err)
	}
	if resolved.Digest != manifestDesc.Digest {
		t.Errorf("destination holds %s, want %s", resolved.Digest, manifestDesc.Digest)
	}
}

// A source that does not hold the image falls back to its registry, which is the branch this
// covers -- TestPullErrorsWhenAnImageCannotBeResolved covers the same failure with no source
// configured at all.
func TestPullErrorsWhenNeitherSourceNorRegistryHasTheImage(t *testing.T) {
	ctx := context.Background()
	const ref = "registry.invalid/pause:3.10"

	refInfo, err := transform.ParseImageRef(ref)
	if err != nil {
		t.Fatalf("failed to parse %s: %v", ref, err)
	}

	_, err = Pull(ctx, []transform.Image{refInfo}, t.TempDir(), PullOptions{
		Arches:         []string{"amd64"},
		CacheDirectory: t.TempDir(),
		Source:         stubSource{store: newStore(ctx, t), refs: map[string]bool{}},
	})
	if err == nil {
		t.Fatal("Pull of an image held by neither the source nor a reachable registry returned no error, so the package would be built without it")
	}
	if !strings.Contains(err.Error(), ref) {
		t.Errorf("error %q does not name the missing image", err)
	}
}

// An image that cannot be resolved has to fail the pull. It used to be collected into a slice
// nothing read, so the goroutine reported success and the image was dropped: Pull returned a
// shorter list with no error, create exited 0, and the gap did not surface until the package
// was published or installed. See #983.
func TestPullErrorsWhenAnImageCannotBeResolved(t *testing.T) {
	ctx := context.Background()
	const ref = "registry.invalid/pause:3.10"

	refInfo, err := transform.ParseImageRef(ref)
	if err != nil {
		t.Fatalf("failed to parse %s: %v", ref, err)
	}

	got, err := Pull(ctx, []transform.Image{refInfo}, t.TempDir(), PullOptions{
		Arches:         []string{"amd64"},
		CacheDirectory: t.TempDir(),
	})
	if err == nil {
		t.Fatalf("Pull of an unresolvable image returned no error and %d images, so a package would be built without it", len(got))
	}
	if !strings.Contains(err.Error(), ref) {
		t.Errorf("error %q does not name the image that could not be resolved", err)
	}
}

// A source holding one architecture of a multi-architecture build must not stand in for the
// registry. It used to: the reference was held, so the store answered for it, the single
// manifest it returned was taken as "what every requested architecture gets", and the
// architectures it could not serve were dropped with no warning and no error. A zarf package
// carries one metadata.architecture by construction, which made that routine.
func TestPullFallsBackWhenTheSourceHoldsOneArchitecture(t *testing.T) {
	ctx := context.Background()
	const ref = "registry.invalid/pause:3.10"

	src := newStore(ctx, t)
	manifestDesc := pushManifest(ctx, t, src, "amd64")
	if err := src.Tag(ctx, manifestDesc, ref); err != nil {
		t.Fatalf("failed to tag the source image: %v", err)
	}

	refInfo, err := transform.ParseImageRef(ref)
	if err != nil {
		t.Fatalf("failed to parse %s: %v", ref, err)
	}

	// registry.invalid does not resolve, so reaching the registry is the failure. That is the
	// assertion: the build must not quietly accept the source's single architecture.
	_, err = Pull(ctx, []transform.Image{refInfo}, t.TempDir(), PullOptions{
		Arches:         []string{"amd64", "arm64"},
		CacheDirectory: t.TempDir(),
		Source:         stubSource{store: src, refs: map[string]bool{ref: true}},
	})
	if err == nil {
		t.Fatal("Pull succeeded with a source holding only amd64, so arm64 was dropped silently")
	}
	if !strings.Contains(err.Error(), ref) {
		t.Errorf("error %q does not name the image that could not be completed", err)
	}
}

// The same source serving a build that asks only for the architecture it holds is still used,
// and never reaches the registry.
func TestPullUsesASourceThatCoversEveryArchitecture(t *testing.T) {
	ctx := context.Background()
	const ref = "registry.invalid/pause:3.10"

	src := newStore(ctx, t)
	manifestDesc := pushManifest(ctx, t, src, "amd64")
	if err := src.Tag(ctx, manifestDesc, ref); err != nil {
		t.Fatalf("failed to tag the source image: %v", err)
	}

	refInfo, err := transform.ParseImageRef(ref)
	if err != nil {
		t.Fatalf("failed to parse %s: %v", ref, err)
	}

	got, err := Pull(ctx, []transform.Image{refInfo}, t.TempDir(), PullOptions{
		Arches:         []string{"amd64"},
		CacheDirectory: t.TempDir(),
		Source:         stubSource{store: src, refs: map[string]bool{ref: true}},
	})
	if err != nil {
		t.Fatalf("Pull from a source covering the one requested architecture: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Pull returned %d images, want 1", len(got))
	}
}

// An artifact that names no architecture at all -- a signature, an SBOM, a file artifact
// stored as an image -- is not multi-platform, so one copy answers whatever is asked and the
// source still stands in for the registry.
func TestPullUsesASourceForAnArtifactWithNoArchitecture(t *testing.T) {
	ctx := context.Background()
	const ref = "registry.invalid/artifact:1.0"

	src := newStore(ctx, t)
	desc := pushArtifactWithoutArchitecture(ctx, t, src)
	if err := src.Tag(ctx, desc, ref); err != nil {
		t.Fatalf("failed to tag the source artifact: %v", err)
	}

	refInfo, err := transform.ParseImageRef(ref)
	if err != nil {
		t.Fatalf("failed to parse %s: %v", ref, err)
	}

	got, err := Pull(ctx, []transform.Image{refInfo}, t.TempDir(), PullOptions{
		Arches:         []string{"amd64", "arm64"},
		CacheDirectory: t.TempDir(),
		Source:         stubSource{store: src, refs: map[string]bool{ref: true}},
	})
	if err != nil {
		t.Fatalf("Pull of an artifact naming no architecture: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Pull returned %d artifacts, want 1", len(got))
	}
}

// pushArtifactWithoutArchitecture pushes a manifest whose config names no architecture.
func pushArtifactWithoutArchitecture(ctx context.Context, t *testing.T, store *oci.Store) ocispec.Descriptor {
	t.Helper()

	configBytes := []byte(`{}`)
	configDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageConfig,
		Digest:    digest.FromBytes(configBytes),
		Size:      int64(len(configBytes)),
	}
	if err := store.Push(ctx, configDesc, bytes.NewReader(configBytes)); err != nil {
		t.Fatalf("failed to push the config: %v", err)
	}
	manifestBytes, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ocispec.Descriptor{},
	})
	if err != nil {
		t.Fatalf("failed to marshal the manifest: %v", err)
	}
	manifestDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    digest.FromBytes(manifestBytes),
		Size:      int64(len(manifestBytes)),
	}
	if err := store.Push(ctx, manifestDesc, bytes.NewReader(manifestBytes)); err != nil {
		t.Fatalf("failed to push the manifest: %v", err)
	}
	return manifestDesc
}

// The fallback has to carry into the copy, not just the metadata read. A source holding one
// architecture is passed over for the whole image, and the copy that follows must go to the
// registry too -- asking the source a second time sent it back to the bundle that had just
// been rejected, which failed with "platform in manifest does not match target platform".
//
// This needs a registry that answers, which is what distinguishes it from
// TestPullFallsBackWhenTheSourceHoldsOneArchitecture: that one only reaches the decision, this
// one follows it all the way into the layout.
func TestPullCompletesTheFallbackFromAPartlyCoveringSource(t *testing.T) {
	ctx := context.Background()
	addr := test.SetupInMemoryRegistry(t)
	ref := addr + "/library/pause:3.10"

	// The registry holds both architectures.
	pushIndexToRegistry(ctx, t, addr, "library/pause", "3.10", "amd64", "arm64")

	// The source holds only amd64, under the same reference.
	src := newStore(ctx, t)
	amd := pushManifest(ctx, t, src, "amd64")
	if err := src.Tag(ctx, amd, ref); err != nil {
		t.Fatalf("failed to tag the source image: %v", err)
	}

	refInfo, err := transform.ParseImageRef(ref)
	if err != nil {
		t.Fatalf("failed to parse %s: %v", ref, err)
	}

	dest := t.TempDir()
	got, err := Pull(ctx, []transform.Image{refInfo}, dest, PullOptions{
		Arches:         []string{"amd64", "arm64"},
		CacheDirectory: t.TempDir(),
		PlainHTTP:      true,
		Source:         stubSource{store: src, refs: map[string]bool{ref: true}},
	})
	if err != nil {
		t.Fatalf("Pull after falling back to a registry that has both architectures: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Pull returned %d manifests, want 2 -- one per architecture", len(got))
	}

	// Both architectures have to be in the written layout, not merely resolved: the copy is
	// the step the stale decision broke.
	arches := map[string]bool{}
	for _, m := range got {
		arches[m.Manifest.Config.Digest.String()] = true
	}
	if len(arches) != 2 {
		t.Errorf("layout holds %d distinct image configs, want 2 -- one per architecture", len(arches))
	}
	layout, err := oci.NewWithContext(ctx, dest)
	if err != nil {
		t.Fatalf("failed to open the destination layout: %v", err)
	}
	if _, err := layout.Resolve(ctx, ref); err != nil {
		t.Fatalf("destination does not hold %s: %v", ref, err)
	}
}

// pushIndexToRegistry publishes an index over one manifest per architecture, so a fallback has
// a registry that can actually answer.
func pushIndexToRegistry(ctx context.Context, t *testing.T, addr, repoName, tag string, arches ...string) {
	t.Helper()

	repo, err := remote.NewRepository(addr + "/" + repoName)
	if err != nil {
		t.Fatalf("failed to open the registry repository: %v", err)
	}
	repo.PlainHTTP = true

	push := func(mediaType string, body []byte) ocispec.Descriptor {
		desc := ocispec.Descriptor{
			MediaType: mediaType,
			Digest:    digest.FromBytes(body),
			Size:      int64(len(body)),
		}
		if err := repo.Push(ctx, desc, bytes.NewReader(body)); err != nil {
			t.Fatalf("failed to push %s: %v", mediaType, err)
		}
		return desc
	}

	children := make([]ocispec.Descriptor, 0, len(arches))
	for _, arch := range arches {
		configBytes, err := json.Marshal(ocispec.Image{
			Platform: ocispec.Platform{OS: "linux", Architecture: arch},
		})
		if err != nil {
			t.Fatalf("failed to marshal the config: %v", err)
		}
		configDesc := push(ocispec.MediaTypeImageConfig, configBytes)
		manifestBytes, err := json.Marshal(ocispec.Manifest{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: ocispec.MediaTypeImageManifest,
			Config:    configDesc,
		})
		if err != nil {
			t.Fatalf("failed to marshal the manifest: %v", err)
		}
		child := push(ocispec.MediaTypeImageManifest, manifestBytes)
		child.Platform = &ocispec.Platform{OS: "linux", Architecture: arch}
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
	indexDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageIndex,
		Digest:    digest.FromBytes(indexBytes),
		Size:      int64(len(indexBytes)),
	}
	if err := repo.PushReference(ctx, indexDesc, bytes.NewReader(indexBytes), tag); err != nil {
		t.Fatalf("failed to push the index: %v", err)
	}
}
