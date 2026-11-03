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

package noncluster

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/pkg/haulerstore"
	"github.com/colonel-byte/cargoship/test"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"oras.land/oras-go/v2"
	orasoci "oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"
)

// TestCargoshipCreateFromHaulerStore builds a package with --hauler-store and checks the
// images came out of the store rather than the registry.
//
// The store is assembled here rather than by the hauler CLI, which the suite cannot assume is
// installed: hauler writes a plain OCI Image Layout whose index entries carry the annotations
// recording what each artifact was collected from, and that is what is reproduced below. The
// format itself is covered against real hauler output by TestRealHaul in pkg/haulerstore.
//
// What this adds over the unit tests is the built binary and a real definition: a package
// assembled from a store has to carry the same image layers, tags and annotations as one
// assembled from a registry, or it builds here and fails to be read back later.
func TestCargoshipCreateFromHaulerStore(t *testing.T) {
	addr := test.SetupInMemoryRegistry(t)
	pushTestImage(t, addr, pauseImageTag)
	pushTestImage(t, addr, ciliumImageTag)

	// The definition names the registry, and those same references are what the store
	// records as each image's origin -- that is how the two are matched.
	store := haulFromRegistry(t, addr, []string{pauseImageTag, ciliumImageTag})

	defDir := copyDistroDefinition(t, imageDistroDir)
	defPath := filepath.Join(defDir, "distro.yaml")
	data, err := os.ReadFile(defPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(defPath, []byte(strings.ReplaceAll(string(data), "__REGISTRY__", addr)), 0o600))

	outDir := t.TempDir()
	stdout, stderr, err := e2e.Cargoship(t, "create", defDir, "--hauler-store", store, "-o", outDir)
	require.NoError(t, err)

	// The log is the only place the build says which source it used, and a silent fallback to
	// the registry would otherwise look exactly like a success.
	output := stdout + stderr
	require.Contains(t, output, "resolving package content from a hauler store")
	require.Contains(t, output, "source=hauler store")
	require.NotContains(t, output, "not held by the local image source",
		"every declared image is in the store, so nothing should have fallen back to the registry")

	pkg := requireSinglePackage(t, outDir)
	requireHaulImagesInPackage(t, pkg, addr)
}

// requireHaulImagesInPackage unpacks a package and checks its image layout holds both declared
// images, tagged and annotated the way a registry pull would leave them. The base-name
// annotation is what locates an image when the package is read back, so a store-sourced build
// that skipped it would produce a package nothing could publish.
func requireHaulImagesInPackage(t *testing.T, pkg string, addr string) {
	t.Helper()

	unpacked := filepath.Join(t.TempDir(), "pkg")
	require.NoError(t, os.MkdirAll(unpacked, 0o755))
	require.NoError(t, archive.Decompress(context.Background(), pkg, unpacked, archive.DecompressOpts{}))

	indexBytes, err := os.ReadFile(filepath.Join(unpacked, "images", ocispec.ImageIndexFile))
	require.NoError(t, err)
	var index ocispec.Index
	require.NoError(t, json.Unmarshal(indexBytes, &index))

	for _, tag := range []string{pauseImageTag, ciliumImageTag} {
		ref := addr + "/" + tag
		var found bool
		for _, m := range index.Manifests {
			if m.Annotations[ocispec.AnnotationRefName] == ref {
				found = true
				require.Equal(t, ref, m.Annotations["org.opencontainers.image.base.name"],
					"%s carries no base-name annotation, so the package could not be read back", ref)
			}
		}
		require.True(t, found, "the package's image layout does not hold %s", ref)
	}
}

// haulFromRegistry builds a hauler-shaped store holding each tag, copied out of the in-memory
// registry and annotated as hauler annotates what it collects. The returned path is a store
// directory, deliberately without the oci-layout marker a live hauler store also lacks.
func haulFromRegistry(t *testing.T, addr string, tags []string) string {
	t.Helper()
	ctx := context.Background()

	storeDir := filepath.Join(t.TempDir(), "store")
	store, err := orasoci.New(storeDir)
	require.NoError(t, err)

	entries := make([]ocispec.Descriptor, 0, len(tags))
	for _, tag := range tags {
		ref := addr + "/" + tag
		repo, err := remote.NewRepository(ref)
		require.NoError(t, err)
		repo.PlainHTTP = true

		desc, err := oras.Copy(ctx, repo, ref, store, ref, oras.DefaultCopyOptions)
		require.NoError(t, err)

		desc.Annotations = map[string]string{
			haulerstore.AnnotationOriginalRef: ref,
			haulerstore.AnnotationKind:        "dev.hauler/image",
			ocispec.AnnotationRefName:         tag,
		}
		entries = append(entries, desc)
	}

	// Replace the index oras wrote with one shaped like hauler's: the annotations are what
	// cargoship matches on, and oras does not write them.
	indexBytes, err := json.Marshal(ocispec.Index{
		Versioned: ocispec.Index{}.Versioned,
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: entries,
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(storeDir, ocispec.ImageIndexFile), indexBytes, 0o644))

	// A live hauler store has no oci-layout marker; removing the one oras wrote keeps this
	// fixture honest about the case the reader has to handle.
	require.NoError(t, os.Remove(filepath.Join(storeDir, ocispec.ImageLayoutFile)))

	return storeDir
}
