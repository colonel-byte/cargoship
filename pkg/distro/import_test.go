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
	"archive/tar"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	orasoci "oras.land/oras-go/v2/content/oci"
)

func TestImport(t *testing.T) {
	ctx := context.Background()

	distroYAMLContent := []byte(`
apiVersion: colonel-byte.dev/v1alpha1
kind: Distro
metadata:
  name: test-distro
  version: 1.0.0
  architecture: amd64
  aggregateChecksum: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
build:
  architecture: amd64
spec:
  type: k3s
  version: 1.0.0
`)
	checksumsContent := []byte("")

	// Create OCI blobs
	blobDistroDigest := digest.FromBytes(distroYAMLContent)
	blobChecksumsDigest := digest.FromBytes(checksumsContent)

	manifest := ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config: ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageConfig,
			Digest:    digest.FromString("{}"),
			Size:      2,
		},
		Layers: []ocispec.Descriptor{
			{
				MediaType: "application/vnd.cargoship.layer.v1.blob",
				Digest:    blobDistroDigest,
				Size:      int64(len(distroYAMLContent)),
				Annotations: map[string]string{
					ocispec.AnnotationTitle: config.DistroYAML,
				},
			},
			{
				MediaType: "application/vnd.cargoship.layer.v1.blob",
				Digest:    blobChecksumsDigest,
				Size:      int64(len(checksumsContent)),
				Annotations: map[string]string{
					ocispec.AnnotationTitle: config.Checksums,
				},
			},
		},
	}
	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)
	manifestDigest := digest.FromBytes(manifestBytes)

	index := ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: []ocispec.Descriptor{
			{
				MediaType: ocispec.MediaTypeImageManifest,
				Digest:    manifestDigest,
				Size:      int64(len(manifestBytes)),
				Platform: &ocispec.Platform{
					Architecture: "amd64",
					OS:           "linux",
				},
			},
		},
	}
	indexBytes, err := json.Marshal(index)
	require.NoError(t, err)

	ociLayoutBytes := []byte(`{"imageLayoutVersion":"1.0.0"}`)

	t.Run("imports from directory layout", func(t *testing.T) {
		ociDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(ociDir, ocispec.ImageLayoutFile), ociLayoutBytes, 0644))
		require.NoError(t, os.WriteFile(filepath.Join(ociDir, ocispec.ImageIndexFile), indexBytes, 0644))

		blobsDir := filepath.Join(ociDir, "blobs", "sha256")
		require.NoError(t, os.MkdirAll(blobsDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(blobsDir, digest.FromString("{}").Encoded()), []byte("{}"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(blobsDir, blobDistroDigest.Encoded()), distroYAMLContent, 0644))
		require.NoError(t, os.WriteFile(filepath.Join(blobsDir, blobChecksumsDigest.Encoded()), checksumsContent, 0644))
		require.NoError(t, os.WriteFile(filepath.Join(blobsDir, manifestDigest.Encoded()), manifestBytes, 0644))

		outDir := t.TempDir()
		pkgPath, err := Import(ctx, ociDir, ImportOptions{
			Architecture:         "amd64",
			Output:               outDir,
			VerificationStrategy: layout.VerifyNever,
		})
		require.NoError(t, err)
		require.FileExists(t, pkgPath)

		// Verify that the created package can be loaded
		disLayout, err := layout.LoadFromTar(ctx, pkgPath, layout.DistroLayoutOptions{
			VerificationStrategy: layout.VerifyNever,
		})
		require.NoError(t, err)
		defer disLayout.Cleanup() //nolint:errcheck
		require.Equal(t, "test-distro", disLayout.Distro.Metadata.Name)
	})

	t.Run("imports from tar archive", func(t *testing.T) {
		tarPath := filepath.Join(t.TempDir(), "skopeo-archive.tar")
		tarFile, err := os.Create(tarPath)
		require.NoError(t, err)
		tw := tar.NewWriter(tarFile)

		writeFile := func(name string, data []byte) {
			hdr := &tar.Header{
				Name: name,
				Mode: 0644,
				Size: int64(len(data)),
			}
			require.NoError(t, tw.WriteHeader(hdr))
			_, err := tw.Write(data)
			require.NoError(t, err)
		}

		writeFile(ocispec.ImageLayoutFile, ociLayoutBytes)
		writeFile(ocispec.ImageIndexFile, indexBytes)
		writeFile("blobs/sha256/"+digest.FromString("{}").Encoded(), []byte("{}"))
		writeFile("blobs/sha256/"+blobDistroDigest.Encoded(), distroYAMLContent)
		writeFile("blobs/sha256/"+blobChecksumsDigest.Encoded(), checksumsContent)
		writeFile("blobs/sha256/"+manifestDigest.Encoded(), manifestBytes)

		require.NoError(t, tw.Close())
		require.NoError(t, tarFile.Close())

		outDir := t.TempDir()
		pkgPath, err := Import(ctx, tarPath, ImportOptions{
			Architecture:         "amd64",
			Output:               outDir,
			VerificationStrategy: layout.VerifyNever,
		})
		require.NoError(t, err)
		require.FileExists(t, pkgPath)

		disLayout, err := layout.LoadFromTar(ctx, pkgPath, layout.DistroLayoutOptions{
			VerificationStrategy: layout.VerifyNever,
		})
		require.NoError(t, err)
		defer disLayout.Cleanup() //nolint:errcheck
		require.Equal(t, "test-distro", disLayout.Distro.Metadata.Name)
	})
}

func TestImportRejectsBadInput(t *testing.T) {
	ctx := context.Background()

	_, err := Import(ctx, "", ImportOptions{Output: t.TempDir()})
	require.ErrorContains(t, err, "must provide an archive source path")

	_, err = Import(ctx, "some-package.tar", ImportOptions{})
	require.ErrorContains(t, err, "no output directory specified")

	_, err = Import(ctx, filepath.Join(t.TempDir(), "missing.tar"), ImportOptions{Output: t.TempDir()})
	require.ErrorContains(t, err, "unable to read source")

	// A directory that is not an OCI layout names the file it is missing.
	_, err = Import(ctx, t.TempDir(), ImportOptions{Output: t.TempDir()})
	require.ErrorContains(t, err, "is not an OCI Image Layout")
}

// TestExtractPackageRejectsAnEscapingLayerTitle covers the guard on the one hop where a
// string from the archive becomes a path on this machine.
//
// AssembleLayers does not hand this function a free-form title today - it locates layers by
// paths cargoship computes, from PackageAlwaysPull and from the package's own validated file
// list - so this is the check that keeps that true for a future caller that assembles layers
// some other way. The guard returns before any blob is fetched, so the store is empty.
func TestExtractPackageRejectsAnEscapingLayerTitle(t *testing.T) {
	ctx := context.Background()

	store, err := orasoci.New(t.TempDir())
	require.NoError(t, err)

	pkgDir := t.TempDir()
	hostile := []ocispec.Descriptor{
		{
			MediaType: "application/vnd.cargoship.layer.v1.blob",
			Digest:    digest.FromString("payload"),
			Annotations: map[string]string{
				ocispec.AnnotationTitle: "../../escaped",
			},
		},
	}

	_, err = extractPackage(ctx, store, hostile, pkgDir, ImportOptions{
		VerificationStrategy: layout.VerifyNever,
	})
	require.ErrorContains(t, err, "does not stay inside the package")

	// The guard ran before the write, so nothing landed beside the package directory.
	siblings, err := filepath.Glob(filepath.Join(filepath.Dir(pkgDir), "escaped"))
	require.NoError(t, err)
	require.Empty(t, siblings)
}
