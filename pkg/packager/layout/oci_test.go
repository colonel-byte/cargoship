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

package layout

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	godigest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/errdef"
)

func TestAnnotationsFromMetadata(t *testing.T) {
	t.Run("only required fields set", func(t *testing.T) {
		meta := distro.ZarfDistroMetadata{Name: "rke2", Description: "a distro"}
		got := AnnotationsFromMetadata(meta)
		require.Equal(t, map[string]string{
			ocispec.AnnotationTitle:       "rke2",
			ocispec.AnnotationDescription: "a distro",
		}, got)
	})

	t.Run("optional fields included when set", func(t *testing.T) {
		meta := distro.ZarfDistroMetadata{
			Name:          "rke2",
			Description:   "a distro",
			URL:           "https://example.com",
			Authors:       "someone",
			Documentation: "https://docs.example.com",
			Source:        "https://src.example.com",
			Vendor:        "acme",
		}
		got := AnnotationsFromMetadata(meta)
		require.Equal(t, "https://example.com", got[ocispec.AnnotationURL])
		require.Equal(t, "someone", got[ocispec.AnnotationAuthors])
		require.Equal(t, "https://docs.example.com", got[ocispec.AnnotationDocumentation])
		require.Equal(t, "https://src.example.com", got[ocispec.AnnotationSource])
		require.Equal(t, "acme", got[ocispec.AnnotationVendor])
	})

	t.Run("explicit annotations take precedence over legacy fields", func(t *testing.T) {
		meta := distro.ZarfDistroMetadata{
			Name:        "rke2",
			Vendor:      "acme",
			Annotations: map[string]string{ocispec.AnnotationVendor: "override"},
		}
		got := AnnotationsFromMetadata(meta)
		require.Equal(t, "override", got[ocispec.AnnotationVendor])
	})
}

func TestDistroLayoutExists(t *testing.T) {
	manifestDesc := ocispec.Descriptor{Digest: godigest.FromString("manifest")}
	configDigest := godigest.FromString("config")
	blobDigest := godigest.FromString("blob")

	d := &DistroLayout{cache: &manifestCache{
		desc:         manifestDesc,
		configDigest: configDigest,
		blobs:        map[godigest.Digest]string{blobDigest: "/some/path"},
	}}

	t.Run("no cache", func(t *testing.T) {
		nilCache := &DistroLayout{}
		ok, err := nilCache.Exists(context.Background(), manifestDesc)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("manifest digest", func(t *testing.T) {
		ok, err := d.Exists(context.Background(), manifestDesc)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("config digest", func(t *testing.T) {
		ok, err := d.Exists(context.Background(), ocispec.Descriptor{Digest: configDigest})
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("blob digest", func(t *testing.T) {
		ok, err := d.Exists(context.Background(), ocispec.Descriptor{Digest: blobDigest})
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("unknown digest", func(t *testing.T) {
		ok, err := d.Exists(context.Background(), ocispec.Descriptor{Digest: godigest.FromString("unknown")})
		require.NoError(t, err)
		require.False(t, ok)
	})
}

func TestDistroLayoutFetch(t *testing.T) {
	dir := t.TempDir()
	blobPath := filepath.Join(dir, "layer.bin")
	require.NoError(t, os.WriteFile(blobPath, []byte("layer-bytes"), 0o600))

	manifestJSON := []byte(`{"manifest":true}`)
	configBytes := []byte(`{"config":true}`)
	manifestDesc := ocispec.Descriptor{Digest: godigest.FromBytes(manifestJSON)}
	configDigest := godigest.FromBytes(configBytes)
	blobDigest := godigest.FromString("layer-bytes")

	d := &DistroLayout{cache: &manifestCache{
		desc:         manifestDesc,
		manifestJSON: manifestJSON,
		configBytes:  configBytes,
		configDigest: configDigest,
		blobs:        map[godigest.Digest]string{blobDigest: blobPath},
	}}

	t.Run("no cache returns not found", func(t *testing.T) {
		nilCache := &DistroLayout{}
		_, err := nilCache.Fetch(context.Background(), manifestDesc)
		require.ErrorIs(t, err, errdef.ErrNotFound)
	})

	t.Run("fetches the manifest", func(t *testing.T) {
		rc, err := d.Fetch(context.Background(), manifestDesc)
		require.NoError(t, err)
		defer func() { require.NoError(t, rc.Close()) }()
		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.JSONEq(t, string(manifestJSON), string(got))
	})

	t.Run("fetches the config", func(t *testing.T) {
		rc, err := d.Fetch(context.Background(), ocispec.Descriptor{Digest: configDigest})
		require.NoError(t, err)
		defer func() { require.NoError(t, rc.Close()) }()
		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.Equal(t, configBytes, got)
	})

	t.Run("fetches a layer blob from disk", func(t *testing.T) {
		rc, err := d.Fetch(context.Background(), ocispec.Descriptor{Digest: blobDigest})
		require.NoError(t, err)
		defer func() { require.NoError(t, rc.Close()) }()
		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.Equal(t, "layer-bytes", string(got))
	})

	t.Run("unknown digest returns not found", func(t *testing.T) {
		_, err := d.Fetch(context.Background(), ocispec.Descriptor{Digest: godigest.FromString("unknown")})
		require.ErrorIs(t, err, errdef.ErrNotFound)
	})
}

func TestDistroLayoutResolve(t *testing.T) {
	manifestDesc := ocispec.Descriptor{Digest: godigest.FromString("manifest")}
	d := &DistroLayout{
		digest: manifestDesc.Digest.String(),
		Distro: distro.ZarfDistro{Metadata: distro.ZarfDistroMetadata{Name: "rke2"}},
		cache:  &manifestCache{desc: manifestDesc},
	}

	t.Run("no cache returns not found", func(t *testing.T) {
		nilCache := &DistroLayout{}
		_, err := nilCache.Resolve(context.Background(), "rke2")
		require.ErrorIs(t, err, errdef.ErrNotFound)
	})

	t.Run("resolves by digest", func(t *testing.T) {
		desc, err := d.Resolve(context.Background(), manifestDesc.Digest.String())
		require.NoError(t, err)
		require.Equal(t, manifestDesc, desc)
	})

	t.Run("resolves by package name", func(t *testing.T) {
		desc, err := d.Resolve(context.Background(), "rke2")
		require.NoError(t, err)
		require.Equal(t, manifestDesc, desc)
	})

	t.Run("unknown reference returns not found", func(t *testing.T) {
		_, err := d.Resolve(context.Background(), "does-not-exist")
		require.ErrorIs(t, err, errdef.ErrNotFound)
	})
}

func TestSetRegistryDigest(t *testing.T) {
	d := &DistroLayout{cache: &manifestCache{}}
	d.SetRegistryDigest("sha256:abc123")
	require.Equal(t, "sha256:abc123", d.digest)
	require.Nil(t, d.cache)
	require.False(t, d.IsPushable())
}

func TestIsPushable(t *testing.T) {
	require.True(t, (&DistroLayout{cache: &manifestCache{}}).IsPushable())
	require.False(t, (&DistroLayout{}).IsPushable())
}

func TestTotalSize(t *testing.T) {
	require.EqualValues(t, 0, (&DistroLayout{}).TotalSize())
	require.EqualValues(t, 42, (&DistroLayout{cache: &manifestCache{totalSize: 42}}).TotalSize())
}
