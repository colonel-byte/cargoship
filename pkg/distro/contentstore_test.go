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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

var specsVersioned = specs.Versioned{SchemaVersion: 2}

// writeLayoutBlob stores data in an OCI layout's blob directory and returns its descriptor.
func writeLayoutBlob(t *testing.T, layoutDir, mediaType string, v any) ocispec.Descriptor {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])
	blobDir := filepath.Join(layoutDir, "blobs", "sha256")
	require.NoError(t, os.MkdirAll(blobDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(blobDir, hexSum), data, 0o644))
	return ocispec.Descriptor{
		MediaType: mediaType,
		Digest:    digest.NewDigestFromEncoded(digest.SHA256, hexSum),
		Size:      int64(len(data)),
	}
}

// writeOCIIndex writes an index.json holding one image manifest annotated with ref.
func writeOCIIndex(t *testing.T, layoutDir, ref string) {
	t.Helper()
	config := writeLayoutBlob(t, layoutDir, ocispec.MediaTypeImageConfig, map[string]string{})
	manifest := writeLayoutBlob(t, layoutDir, ocispec.MediaTypeImageManifest, ocispec.Manifest{
		Versioned: specsVersioned,
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    config,
	})
	manifest.Annotations = map[string]string{
		// Enough for either reader: hauler keys on its own annotation, zarf on the OCI one.
		"hauler.dev/original-ref":       ref,
		ocispec.AnnotationRefName:       ref,
		ocispec.AnnotationBaseImageName: ref,
	}
	index := ocispec.Index{
		Versioned: specsVersioned,
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: []ocispec.Descriptor{manifest},
	}
	data, err := json.Marshal(index)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(layoutDir, ocispec.ImageIndexFile), data, 0o644))
}

// A content store's format comes from what the bundle holds, not from what it is called, so
// each of these is named in a way that would mislead a reader going by the path.
func TestOpenContentStoreRoutesByLayout(t *testing.T) {
	ctx := context.Background()
	const ref = "registry.k8s.io/pause:3.10"

	t.Run("zarf package", func(t *testing.T) {
		// Deliberately named like a haul: a zarf package also carries images/index.json, so a
		// reader that tested for the hauler layout first would read this as an empty haul.
		dir := filepath.Join(t.TempDir(), "haul.tar.zst")
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "images"), 0o755))
		writeOCIIndex(t, filepath.Join(dir, "images"), ref)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "zarf.yaml"),
			[]byte("kind: ZarfPackageConfig\nmetadata:\n  name: fixture\ncomponents: []\n"), 0o644))

		store, cleanup, err := openContentStore(ctx, dir)
		require.NoError(t, err)
		t.Cleanup(func() { _ = cleanup() }) //nolint:errcheck // the temp dir is the test runner's to lose
		require.Contains(t, store.Describe(), "zarf package")
		require.True(t, store.HasImage(ref))
	})

	t.Run("hauler store", func(t *testing.T) {
		// And named like a zarf package, for the same reason in reverse.
		dir := filepath.Join(t.TempDir(), "zarf-package-foo-amd64.tar.zst")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		writeOCIIndex(t, dir, ref)

		store, cleanup, err := openContentStore(ctx, dir)
		require.NoError(t, err)
		t.Cleanup(func() { _ = cleanup() }) //nolint:errcheck // the temp dir is the test runner's to lose
		require.Contains(t, store.Describe(), "hauler store")
		require.True(t, store.HasImage(ref))
	})

	t.Run("neither", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "README"), []byte("nothing here"), 0o644))

		_, _, err := openContentStore(ctx, dir)
		require.Error(t, err)
		// The error has to name both layouts, because the operator cannot otherwise tell which
		// of the two cargoship was looking for.
		require.Contains(t, err.Error(), "zarf.yaml")
		require.Contains(t, err.Error(), ocispec.ImageIndexFile)
	})

	t.Run("missing path", func(t *testing.T) {
		_, _, err := openContentStore(ctx, filepath.Join(t.TempDir(), "absent"))
		require.Error(t, err)
	})

	t.Run("split archive", func(t *testing.T) {
		part := filepath.Join(t.TempDir(), "zarf-package-foo-amd64.tar.zst.part000")
		require.NoError(t, os.WriteFile(part, []byte("partial"), 0o644))

		_, _, err := openContentStore(ctx, part)
		require.Error(t, err)
		require.Contains(t, err.Error(), "split zarf package", err.Error())
	})
}

// A directory the caller passed is read in place and must survive being closed: it is the
// operator's own store, not something cargoship unpacked.
func TestOpenContentStoreLeavesADirectoryAlone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	writeOCIIndex(t, dir, "registry.k8s.io/pause:3.10")

	_, cleanup, err := openContentStore(context.Background(), dir)
	require.NoError(t, err)
	require.NoError(t, cleanup())
	require.FileExists(t, filepath.Join(dir, ocispec.ImageIndexFile))
}
