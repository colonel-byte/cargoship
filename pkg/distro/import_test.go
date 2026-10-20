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
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
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

	t.Run("imports from docker archive", func(t *testing.T) {
		dockerTarPath := filepath.Join(t.TempDir(), "docker-archive.tar")
		tarFile, err := os.Create(dockerTarPath)
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

		dockerManifestJSON, err := json.Marshal([]dockerManifestItem{
			{
				Config:   "config.json",
				RepoTags: []string{"test-package:latest"},
				Layers:   []string{"distro.tar", "checksums.tar"},
			},
		})
		require.NoError(t, err)

		makeSingleFileTar := func(innerName string, innerData []byte) []byte {
			buf := &strings.Builder{}
			twInner := tar.NewWriter(io.Writer(buf))
			require.NoError(t, twInner.WriteHeader(&tar.Header{
				Name: innerName,
				Mode: 0644,
				Size: int64(len(innerData)),
			}))
			_, err := twInner.Write(innerData)
			require.NoError(t, err)
			require.NoError(t, twInner.Close())
			return []byte(buf.String())
		}

		writeFile("manifest.json", dockerManifestJSON)
		writeFile("config.json", []byte("{}"))
		writeFile("distro.tar", makeSingleFileTar(config.DistroYAML, distroYAMLContent))
		writeFile("checksums.tar", makeSingleFileTar(config.Checksums, checksumsContent))

		require.NoError(t, tw.Close())
		require.NoError(t, tarFile.Close())

		outDir := t.TempDir()
		pkgPath, err := Import(ctx, dockerTarPath, ImportOptions{
			Format:               ImportFormatDockerArchive,
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
