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

package coci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/config"
	"github.com/defenseunicorns/pkg/oci"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	orascontent "oras.land/oras-go/v2/content"
)

// fakeFetcher is an in-memory content.Fetcher backed by a digest keyed blob store, so the layer
// assembly functions can be exercised without a real OCI registry.
type fakeFetcher struct {
	blobs map[digest.Digest][]byte
}

func newFakeFetcher() *fakeFetcher {
	return &fakeFetcher{blobs: map[digest.Digest][]byte{}}
}

func (f *fakeFetcher) Fetch(_ context.Context, desc ocispec.Descriptor) (io.ReadCloser, error) {
	b, ok := f.blobs[desc.Digest]
	if !ok {
		return nil, fmt.Errorf("fake fetcher: no content for digest %s", desc.Digest)
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

// addLayer registers content as a layer of root at path, and as a fetchable blob, returning its
// descriptor so callers can wire up digests elsewhere (e.g. an index entry).
func (f *fakeFetcher) addLayer(root *oci.Manifest, path string, content []byte) ocispec.Descriptor {
	desc := orascontent.NewDescriptorFromBytes("application/octet-stream", content)
	desc.Annotations = map[string]string{ocispec.AnnotationTitle: path}
	f.blobs[desc.Digest] = content
	root.Layers = append(root.Layers, desc)
	return desc
}

func TestFetchDistroYAMLFunc(t *testing.T) {
	t.Run("fetches and parses the distro.yaml layer", func(t *testing.T) {
		root := &oci.Manifest{}
		fetcher := newFakeFetcher()
		fetcher.addLayer(root, config.DistroYAML, []byte("metadata:\n  name: test-package\n"))

		dis, err := FetchDistroYAML(context.Background(), root, fetcher)
		require.NoError(t, err)
		require.Equal(t, "test-package", dis.Metadata.Name)
	})

	t.Run("errors when distro.yaml is not in the manifest", func(t *testing.T) {
		root := &oci.Manifest{}
		fetcher := newFakeFetcher()
		_, err := FetchDistroYAML(context.Background(), root, fetcher)
		require.ErrorContains(t, err, "unable to find")
	})
}

func TestLayersFromFiles(t *testing.T) {
	root := &oci.Manifest{}
	fetcher := newFakeFetcher()
	fetcher.addLayer(root, filepath.Join(config.FilesDir, "0", "one.txt"), []byte("one"))
	// Index 1 has no corresponding layer, so it should be skipped rather than error.

	files := v1alpha1.ZarfFiles{
		{Target: "/some/one.txt"},
		{Target: "/some/two.txt"},
	}
	got, err := LayersFromFiles(context.Background(), root, files, config.FilesDir)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, filepath.Join(config.FilesDir, "0", "one.txt"), got[0].Annotations[ocispec.AnnotationTitle])
}

// addBlobDescriptor registers content as a fetchable blob titled at its digest-based path under
// config.ImagesBlobsDir, and appends the corresponding layer to root, returning its descriptor.
func addBlobDescriptor(root *oci.Manifest, fetcher *fakeFetcher, mediaType string, content []byte) ocispec.Descriptor {
	desc := orascontent.NewDescriptorFromBytes(mediaType, content)
	fetcher.blobs[desc.Digest] = content
	root.Layers = append(root.Layers, ocispec.Descriptor{
		Digest:      desc.Digest,
		Size:        desc.Size,
		Annotations: map[string]string{ocispec.AnnotationTitle: filepath.Join(config.ImagesBlobsDir, desc.Digest.Encoded())},
	})
	return desc
}

func TestLayersFromManifestChildren(t *testing.T) {
	root := &oci.Manifest{}
	fetcher := newFakeFetcher()

	configDesc := addBlobDescriptor(root, fetcher, "application/vnd.oci.image.config.v1+json", []byte(`{"config":true}`))
	layerDesc := addBlobDescriptor(root, fetcher, "application/vnd.oci.image.layer.v1.tar", []byte("layer-bytes"))

	manifestJSON, err := json.Marshal(ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ocispec.Descriptor{layerDesc},
	})
	require.NoError(t, err)
	manifestDesc := addBlobDescriptor(root, fetcher, ocispec.MediaTypeImageManifest, manifestJSON)

	got, err := layersFromManifestChildren(context.Background(), root, fetcher, manifestDesc)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, filepath.Join(config.ImagesBlobsDir, configDesc.Digest.Encoded()), got[0].Annotations[ocispec.AnnotationTitle])
	require.Equal(t, filepath.Join(config.ImagesBlobsDir, layerDesc.Digest.Encoded()), got[1].Annotations[ocispec.AnnotationTitle])
}

func TestLayersFromIndexChildren(t *testing.T) {
	root := &oci.Manifest{}
	fetcher := newFakeFetcher()

	configDesc := addBlobDescriptor(root, fetcher, "application/vnd.oci.image.config.v1+json", []byte(`{"config":true}`))

	manifestJSON, err := json.Marshal(ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ocispec.Descriptor{},
	})
	require.NoError(t, err)
	manifestDesc := addBlobDescriptor(root, fetcher, ocispec.MediaTypeImageManifest, manifestJSON)

	indexJSON, err := json.Marshal(ocispec.Index{Manifests: []ocispec.Descriptor{manifestDesc}})
	require.NoError(t, err)
	indexDesc := addBlobDescriptor(root, fetcher, ocispec.MediaTypeImageIndex, indexJSON)

	got, err := layersFromIndexChildren(context.Background(), root, fetcher, indexDesc)
	require.NoError(t, err)
	// One entry for the manifest child itself, plus its config layer.
	require.Len(t, got, 2)
}

func TestLayersFromImages(t *testing.T) {
	t.Run("image not found in index errors", func(t *testing.T) {
		root := &oci.Manifest{}
		fetcher := newFakeFetcher()
		index := ocispec.Index{Manifests: []ocispec.Descriptor{}}
		b, err := json.Marshal(index)
		require.NoError(t, err)
		fetcher.addLayer(root, config.IndexPath, b)

		_, err = LayersFromImages(context.Background(), root, fetcher, []string{"example.com/nginx:1.25"})
		require.ErrorContains(t, err, "not found in package index")
	})

	t.Run("resolves a single-arch manifest image", func(t *testing.T) {
		root := &oci.Manifest{}
		fetcher := newFakeFetcher()

		configDesc := addBlobDescriptor(root, fetcher, "application/vnd.oci.image.config.v1+json", []byte(`{"config":true}`))
		layerDesc := addBlobDescriptor(root, fetcher, "application/vnd.oci.image.layer.v1.tar", []byte("layer-bytes"))

		manifestJSON, err := json.Marshal(ocispec.Manifest{
			MediaType: ocispec.MediaTypeImageManifest,
			Config:    configDesc,
			Layers:    []ocispec.Descriptor{layerDesc},
		})
		require.NoError(t, err)
		manifestDesc := addBlobDescriptor(root, fetcher, ocispec.MediaTypeImageManifest, manifestJSON)
		manifestDesc.Annotations = map[string]string{ocispec.AnnotationBaseImageName: "example.com/nginx:1.25"}

		index := ocispec.Index{Manifests: []ocispec.Descriptor{manifestDesc}}
		indexJSON, err := json.Marshal(index)
		require.NoError(t, err)
		fetcher.addLayer(root, config.IndexPath, indexJSON)
		fetcher.addLayer(root, config.OCILayoutPath, []byte(`{"imageLayoutVersion":"1.0.0"}`))

		got, err := LayersFromImages(context.Background(), root, fetcher, []string{"example.com/nginx:1.25"})
		require.NoError(t, err)

		var titles []string
		for _, d := range got {
			titles = append(titles, d.Annotations[ocispec.AnnotationTitle])
		}
		require.Contains(t, titles, filepath.Join(config.ImagesBlobsDir, manifestDesc.Digest.Encoded()))
		require.Contains(t, titles, filepath.Join(config.ImagesBlobsDir, configDesc.Digest.Encoded()))
		require.Contains(t, titles, filepath.Join(config.ImagesBlobsDir, layerDesc.Digest.Encoded()))
	})
}

func TestAssembleLayersMetadataOnly(t *testing.T) {
	root := &oci.Manifest{}
	fetcher := newFakeFetcher()
	fetcher.addLayer(root, config.DistroYAML, []byte("metadata:\n  name: test-package\n"))
	fetcher.addLayer(root, config.Checksums, []byte("deadbeef  distro.yaml\n"))

	got, err := AssembleLayers(context.Background(), root, fetcher)
	require.NoError(t, err)

	var titles []string
	for _, d := range got {
		titles = append(titles, d.Annotations[ocispec.AnnotationTitle])
	}
	require.ElementsMatch(t, []string{config.DistroYAML, config.Checksums}, titles)
}
