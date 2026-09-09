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
	"testing"

	"github.com/colonel-byte/cargoship/src/api"
	"github.com/colonel-byte/cargoship/src/test"
	"github.com/defenseunicorns/pkg/oci"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	orascontent "oras.land/oras-go/v2/content"
)

// TestUpdateIndexForArches covers the index a publish tags, which is what makes a package
// resolvable per architecture. A package is one manifest however many architectures it covers,
// so the index lists that one manifest digest once per architecture.
func TestUpdateIndexForArches(t *testing.T) {
	t.Run("a package covering two architectures lists one manifest under both", func(t *testing.T) {
		ctx := context.Background()
		remote := testRemote(t, "0.0.1")
		desc := pushManifest(t, remote, "package")

		require.NoError(t, remote.updateIndexForArches(ctx, "0.0.1", desc, api.Arches{"amd64", "arm64"}))

		index := readIndex(t, remote, "0.0.1")
		require.Len(t, index.Manifests, 2)
		require.Equal(t, []string{"amd64", "arm64"}, architectures(index))
		for _, manifest := range index.Manifests {
			require.Equal(t, desc.Digest, manifest.Digest, "both entries must point at the one manifest")
			require.Equal(t, desc.Size, manifest.Size)
			require.Equal(t, oci.MultiOS, manifest.Platform.OS)
		}
	})

	t.Run("publishing a second architecture under the same tag keeps the first", func(t *testing.T) {
		ctx := context.Background()
		remote := testRemote(t, "0.0.1")
		amd64Desc := pushManifest(t, remote, "amd64 package")
		arm64Desc := pushManifest(t, remote, "arm64 package")

		require.NoError(t, remote.updateIndexForArches(ctx, "0.0.1", amd64Desc, api.Arches{"amd64"}))
		require.NoError(t, remote.updateIndexForArches(ctx, "0.0.1", arm64Desc, api.Arches{"arm64"}))

		index := readIndex(t, remote, "0.0.1")
		require.Len(t, index.Manifests, 2)
		require.Equal(t, []string{"amd64", "arm64"}, architectures(index))
		require.Equal(t, amd64Desc.Digest, index.Manifests[0].Digest)
		require.Equal(t, arm64Desc.Digest, index.Manifests[1].Digest)
	})

	t.Run("republishing an architecture replaces its entry", func(t *testing.T) {
		ctx := context.Background()
		remote := testRemote(t, "0.0.1")
		first := pushManifest(t, remote, "first build")
		second := pushManifest(t, remote, "second build")

		require.NoError(t, remote.updateIndexForArches(ctx, "0.0.1", first, api.Arches{"amd64"}))
		require.NoError(t, remote.updateIndexForArches(ctx, "0.0.1", second, api.Arches{"amd64"}))

		index := readIndex(t, remote, "0.0.1")
		require.Len(t, index.Manifests, 1)
		require.Equal(t, second.Digest, index.Manifests[0].Digest)
	})
}

// TestFetchIndexUntaggedVersion covers the first publish of a version, where there is no index to
// read and one has to be started from nothing.
func TestFetchIndexUntaggedVersion(t *testing.T) {
	remote := testRemote(t, "0.0.1")

	index, err := remote.fetchIndex(context.Background(), "0.0.1")
	require.NoError(t, err)
	require.Empty(t, index.Manifests)
	require.Equal(t, ocispec.MediaTypeImageIndex, index.MediaType)
	require.Equal(t, 2, index.SchemaVersion)
}

// testRemote returns a Remote against a fresh in-memory registry, tagged at version.
func testRemote(t *testing.T, version string) *Remote {
	t.Helper()

	addr := test.SetupInMemoryRegistry(t)
	remote, err := NewRemote(context.Background(), fmt.Sprintf("%s/test-package:%s", addr, version),
		oci.PlatformForArch("amd64"), oci.WithPlainHTTP(true))
	require.NoError(t, err)
	return remote
}

// pushManifest pushes a manifest holding a single config blob carrying body, and returns its
// descriptor. The registry rejects an index that references a manifest it does not hold, so the
// index tests need something real to point at, and body makes each one a distinct digest.
func pushManifest(t *testing.T, remote *Remote, body string) ocispec.Descriptor {
	t.Helper()

	return pushManifestTagged(t, remote, body, "")
}

// pushManifestTagged pushes a manifest as pushManifest does, tagged at tag. An empty tag pushes
// the manifest by digest alone.
func pushManifestTagged(t *testing.T, remote *Remote, body, tag string) ocispec.Descriptor {
	t.Helper()

	ctx := context.Background()
	configDesc := pushBlob(t, remote, CargoshipConfigMediaType, []byte(fmt.Sprintf("{%q:%q}", "body", body)))

	manifest := ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Versioned: specs.Versioned{SchemaVersion: 2},
		Config:    configDesc,
		Layers:    []ocispec.Descriptor{},
	}
	b, err := json.Marshal(manifest)
	require.NoError(t, err)

	desc := orascontent.NewDescriptorFromBytes(ocispec.MediaTypeImageManifest, b)
	if tag == "" {
		require.NoError(t, remote.Repo().Manifests().Push(ctx, desc, bytes.NewReader(b)))
		return desc
	}
	require.NoError(t, remote.Repo().Manifests().PushReference(ctx, desc, bytes.NewReader(b), tag))
	return desc
}

// pushBlob pushes b to the registry and returns its descriptor.
func pushBlob(t *testing.T, remote *Remote, mediaType string, b []byte) ocispec.Descriptor {
	t.Helper()

	desc := orascontent.NewDescriptorFromBytes(mediaType, b)
	require.NoError(t, remote.Repo().Push(context.Background(), desc, bytes.NewReader(b)))
	return desc
}

// readIndex reads back the index tagged at tag.
func readIndex(t *testing.T, remote *Remote, tag string) *ocispec.Index {
	t.Helper()

	index, err := remote.fetchIndex(context.Background(), tag)
	require.NoError(t, err)
	return index
}

// architectures lists the architectures an index records, in index order.
func architectures(index *ocispec.Index) []string {
	arches := make([]string, 0, len(index.Manifests))
	for _, manifest := range index.Manifests {
		if manifest.Platform != nil {
			arches = append(arches, manifest.Platform.Architecture)
		}
	}
	return arches
}
