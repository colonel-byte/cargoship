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
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/api"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
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

// exampleRoot is the generated example tree, reached from this package's directory.
const exampleRoot = "../../../example"

// writeManifestablePackage lays out the smallest directory computeManifest accepts:
// the definition it reads the metadata and the build timestamp from, and the
// checksums file it reads before anything else. Nothing here is checksummed --
// computeManifest hashes a file it finds no checksum for.
func writeManifestablePackage(t *testing.T, distroYAML []byte) *DistroLayout {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.DistroYAML), distroYAML, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.Checksums), nil, 0o600))
	return &DistroLayout{dirPath: dir}
}

// manifestAnnotations returns the annotations of the manifest computeManifest packed.
func manifestAnnotations(t *testing.T, d *DistroLayout) map[string]string {
	t.Helper()
	require.NoError(t, d.computeManifest(t.Context()))
	var manifest ocispec.Manifest
	require.NoError(t, json.Unmarshal(d.cache.manifestJSON, &manifest))
	return manifest.Annotations
}

// The created annotation is the one place a package's build timestamp is reparsed
// rather than copied, so it is the one place the layout it was written in matters.
// assemble writes it with api.BuildTimestampFormat; zarf v0.87.0 removed the
// v1alpha1 constant this used to read, and a layout mismatch is not an error -- the
// parse fails and the timestamp silently becomes the epoch.
func TestComputeManifestCreatedAnnotation(t *testing.T) {
	// The timestamp is written out the way assemble writes it rather than formatted
	// with the same constant the code under test reads, so that changing the constant
	// fails here instead of moving both sides together.
	t.Run("a build timestamp becomes the created annotation", func(t *testing.T) {
		d := writeManifestablePackage(t, []byte(`apiVersion: zarf.dev/v1alpha1
kind: ZarfDistro
metadata:
  name: timestamped
  version: 0.0.1
build:
  timestamp: Wed, 04 Mar 2026 17:55:19 +0000
`))

		got := manifestAnnotations(t, d)
		require.Equal(t, "2026-03-04T17:55:19Z", got[ocispec.AnnotationCreated])
	})

	// The literal above is only the right wire form while assemble writes timestamps
	// in this layout, so that is asserted rather than assumed.
	t.Run("assemble writes a timestamp in the layout the annotation is parsed from", func(t *testing.T) {
		built := time.Date(2026, time.March, 4, 17, 55, 19, 0, time.UTC)
		require.Equal(t, "Wed, 04 Mar 2026 17:55:19 +0000", built.Format(api.BuildTimestampFormat))
	})

	// An unparseable timestamp has to stay a zero time rather than failing the push:
	// a package built by an older cargoship is still pushable.
	for _, tt := range []struct {
		name      string
		timestamp string
	}{
		{
			name:      "absent",
			timestamp: "",
		},
		{
			name:      "written in some other layout",
			timestamp: "2026-03-04T17:55:19Z",
		},
	} {
		t.Run("a timestamp "+tt.name+" falls back to the epoch", func(t *testing.T) {
			d := writeManifestablePackage(t, []byte(`apiVersion: zarf.dev/v1alpha1
kind: ZarfDistro
metadata:
  name: untimestamped
  version: 0.0.1
build:
  timestamp: `+tt.timestamp+`
`))

			got := manifestAnnotations(t, d)
			require.Equal(t, time.Time{}.UTC().Format(OCITimestampFormat), got[ocispec.AnnotationCreated])
		})
	}
}

// Every definition cargoship ships has to still reach a manifest. computeManifest
// parses the definition itself rather than reusing the loaded package, so it is
// where a change to the parse or to the metadata annotations shows up, and the
// examples are the only definitions that cover the full vocabulary the generators
// emit. Nothing is downloaded: the definition is the only file in the layout.
func TestComputeManifestForEveryGeneratedExample(t *testing.T) {
	var definitions []string
	err := filepath.WalkDir(exampleRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == config.DistroYAML {
			definitions = append(definitions, path)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, definitions, "no example definitions under "+exampleRoot)

	for _, path := range definitions {
		t.Run(path, func(t *testing.T) {
			distroYAML, err := os.ReadFile(path)
			require.NoError(t, err)

			got := manifestAnnotations(t, writeManifestablePackage(t, distroYAML))
			// The title is what a registry lists the package under, and it is the one
			// annotation every definition is required to carry.
			require.NotEmpty(t, got[ocispec.AnnotationTitle])
			require.NotEmpty(t, got[ocispec.AnnotationCreated])
		})
	}
}
