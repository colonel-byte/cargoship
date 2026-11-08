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

package assemble

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/colonel-byte/cargoship/api"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/images"
	"github.com/colonel-byte/cargoship/test"
	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote"
)

// artifactLayer is one titled layer of a test artifact.
type artifactLayer struct {
	title   string
	content []byte
}

// pushArtifact pushes a file artifact to repo:tag and returns the pushed root's digest.
//
// perArch with a single "" key pushes one manifest, which is the shape an artifact published
// for one platform has. Anything else pushes an image index over one manifest per platform.
func pushArtifact(t *testing.T, addr, repoName, tag string, perArch map[string][]artifactLayer) string {
	t.Helper()
	return pushArtifactOS(t, addr, repoName, tag, "linux", perArch)
}

// pushArtifactOS is pushArtifact with the index children's platform OS spelled explicitly, for
// the artifacts that do not say "linux" -- a zarf or cargoship package index says "multi".
func pushArtifactOS(t *testing.T, addr, repoName, tag, platformOS string, perArch map[string][]artifactLayer) string {
	t.Helper()
	ctx := context.Background()

	repo, err := remote.NewRepository(addr + "/" + repoName)
	require.NoError(t, err)
	repo.PlainHTTP = true

	blob := func(mediaType string, b []byte) ocispec.Descriptor {
		sum := sha256.Sum256(b)
		desc := ocispec.Descriptor{
			MediaType: mediaType,
			Digest:    digest.NewDigestFromEncoded(digest.SHA256, hex.EncodeToString(sum[:])),
			Size:      int64(len(b)),
		}
		require.NoError(t, repo.Push(ctx, desc, bytes.NewReader(b)))
		return desc
	}

	manifestFor := func(layers []artifactLayer) ([]byte, ocispec.Descriptor) {
		descs := make([]ocispec.Descriptor, 0, len(layers))
		for _, l := range layers {
			desc := blob("application/gzip", l.content)
			desc.Annotations = map[string]string{ocispec.AnnotationTitle: l.title}
			descs = append(descs, desc)
		}
		configDesc := blob(ocispec.MediaTypeImageConfig, []byte(`{}`))
		b, err := json.Marshal(ocispec.Manifest{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: ocispec.MediaTypeImageManifest,
			Config:    configDesc,
			Layers:    descs,
		})
		require.NoError(t, err)
		sum := sha256.Sum256(b)
		return b, ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageManifest,
			Digest:    digest.NewDigestFromEncoded(digest.SHA256, hex.EncodeToString(sum[:])),
			Size:      int64(len(b)),
		}
	}

	arches := slices.Sorted(maps.Keys(perArch))
	if len(arches) == 1 && arches[0] == "" {
		b, desc := manifestFor(perArch[""])
		require.NoError(t, repo.PushReference(ctx, desc, bytes.NewReader(b), tag))
		return desc.Digest.String()
	}

	children := make([]ocispec.Descriptor, 0, len(arches))
	for _, arch := range arches {
		b, desc := manifestFor(perArch[arch])
		require.NoError(t, repo.Push(ctx, desc, bytes.NewReader(b)))
		desc.Platform = &ocispec.Platform{OS: platformOS, Architecture: arch}
		children = append(children, desc)
	}
	indexBytes, err := json.Marshal(ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: children,
	})
	require.NoError(t, err)
	sum := sha256.Sum256(indexBytes)
	indexDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageIndex,
		Digest:    digest.NewDigestFromEncoded(digest.SHA256, hex.EncodeToString(sum[:])),
		Size:      int64(len(indexBytes)),
	}
	require.NoError(t, repo.PushReference(ctx, indexDesc, bytes.NewReader(indexBytes), tag))
	return indexDesc.Digest.String()
}

func shasumOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// grabOCI runs fileGrabber over an oci:// entry against a plain-HTTP registry and returns the
// staged bytes.
func grabOCI(t *testing.T, file v1alpha1.File, opts ociOptions) (string, *distro.FileSource, error) {
	t.Helper()
	buildPath := t.TempDir()
	opts.PlainHTTP = true
	provenance, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, nil, nil, opts, nil)
	if err != nil {
		return "", nil, err
	}
	got, readErr := os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "0", filepath.Base(file.Target)))
	require.NoError(t, readErr)
	return string(got), provenance, nil
}

// The shape one vendor publishes an air-gapped engine bundle in: one tag, an index, and each
// platform's whole release as a single titled tarball. extractPath names a member inside it.
func TestFetchOCIFileTarballLayer(t *testing.T) {
	addr := test.SetupInMemoryRegistry(t)
	archive := tarGz(t, tarGzEntry{name: "bin/engine", content: grabbedPayload})
	pushArtifact(t, addr, "vendor/engine", "v1.37.1", map[string][]artifactLayer{
		"": {{title: "engine.linux-amd64.tar.gz", content: archive}},
	})

	got, provenance, err := grabOCI(t, v1alpha1.File{
		Source:      fmt.Sprintf("oci://%s/vendor/engine:v1.37.1", addr),
		ExtractPath: "bin/engine",
		Target:      "/usr/local/bin/engine",
		Shasum:      payloadShasum(),
	}, ociOptions{})
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, got)
	require.Nil(t, provenance, "a file read from its declared reference reports no redirection")
}

// The other shape in the wild: every file in the release as its own titled layer, where the
// title is what names the bytes and there is no archive to extract from.
func TestFetchOCIFileTitledLayer(t *testing.T) {
	addr := test.SetupInMemoryRegistry(t)
	pushArtifact(t, addr, "cri-o/bundle", "v1.35.0", map[string][]artifactLayer{
		"": {
			{title: "cri-o/README.md", content: []byte("readme\n")},
			{title: "cri-o/bin/crio", content: []byte(grabbedPayload)},
			{title: "cri-o/bin/pinns", content: []byte("pinns\n")},
		},
	})

	got, _, err := grabOCI(t, v1alpha1.File{
		Source: fmt.Sprintf("oci://%s/cri-o/bundle:v1.35.0", addr),
		Layer:  "cri-o/bin/crio",
		Target: "/usr/local/bin/crio",
		Shasum: payloadShasum(),
	}, ociOptions{})
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, got)
}

// The entry's selector.arch picks the platform child out of an index. A wrong child fails the
// declared shasum rather than passing quietly, which is the point of asserting on content.
func TestFetchOCIFileSelectsPlatformChild(t *testing.T) {
	addr := test.SetupInMemoryRegistry(t)
	pushArtifact(t, addr, "vendor/engine", "v1.37.1", map[string][]artifactLayer{
		"amd64": {{title: "engine", content: []byte(grabbedPayload)}},
		"arm64": {{title: "engine", content: []byte("arm64 contents that must not be staged\n")}},
	})

	got, _, err := grabOCI(t, v1alpha1.File{
		Source:   fmt.Sprintf("oci://%s/vendor/engine:v1.37.1", addr),
		Target:   "/usr/local/bin/engine",
		Shasum:   payloadShasum(),
		Selector: v1alpha1.BinarySelector{Arch: api.Arches{api.ArchAMD64}},
	}, ociOptions{})
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, got)
}

// An entry naming no single architecture cannot say which child of an index it meant. The
// failure has to point at selector.arch, because oras alone reports only that nothing matched.
func TestFetchOCIFileIndexWithoutArch(t *testing.T) {
	addr := test.SetupInMemoryRegistry(t)
	pushArtifact(t, addr, "vendor/engine", "v1.37.1", map[string][]artifactLayer{
		"amd64": {{title: "engine", content: []byte(grabbedPayload)}},
		"arm64": {{title: "engine", content: []byte("arm64\n")}},
	})

	_, _, err := grabOCI(t, v1alpha1.File{
		Source: fmt.Sprintf("oci://%s/vendor/engine:v1.37.1", addr),
		Target: "/usr/local/bin/engine",
		Shasum: payloadShasum(),
	}, ociOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "selector.arch")
}

func TestFetchOCIFileLayerSelectionErrors(t *testing.T) {
	addr := test.SetupInMemoryRegistry(t)
	pushArtifact(t, addr, "cri-o/bundle", "v1.35.0", map[string][]artifactLayer{
		"": {
			{title: "cri-o/README.md", content: []byte("readme\n")},
			{title: "cri-o/bin/crio", content: []byte(grabbedPayload)},
		},
	})
	source := fmt.Sprintf("oci://%s/cri-o/bundle:v1.35.0", addr)

	for _, tc := range []struct {
		name  string
		layer string
		want  []string
	}{
		{
			name:  "no layer named for a multi-layer artifact",
			layer: "",
			want:  []string{"carries 2 layers", "layer field", "cri-o/bin/crio"},
		},
		{
			name:  "layer the artifact does not carry",
			layer: "cri-o/bin/conmon",
			want:  []string{"no layer titled", "cri-o/README.md"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := grabOCI(t, v1alpha1.File{
				Source: source,
				Layer:  tc.layer,
				Target: "/usr/local/bin/crio",
				Shasum: payloadShasum(),
			}, ociOptions{})
			require.Error(t, err)
			for _, want := range tc.want {
				require.Contains(t, err.Error(), want)
			}
		})
	}
}

// An artifact whose selected layer carries no title has no file name for the extraction to
// identify an archive format from, so it is refused rather than failing inside the extractor.
func TestFetchOCIFileExtractPathNeedsATitledLayer(t *testing.T) {
	addr := test.SetupInMemoryRegistry(t)
	ctx := context.Background()
	repo, err := remote.NewRepository(addr + "/vendor/untitled")
	require.NoError(t, err)
	repo.PlainHTTP = true

	archive := tarGz(t, tarGzEntry{name: "bin/engine", content: grabbedPayload})
	sum := sha256.Sum256(archive)
	layer := ocispec.Descriptor{
		MediaType: "application/gzip",
		Digest:    digest.NewDigestFromEncoded(digest.SHA256, hex.EncodeToString(sum[:])),
		Size:      int64(len(archive)),
	}
	require.NoError(t, repo.Push(ctx, layer, bytes.NewReader(archive)))
	configSum := sha256.Sum256([]byte(`{}`))
	configDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageConfig,
		Digest:    digest.NewDigestFromEncoded(digest.SHA256, hex.EncodeToString(configSum[:])),
		Size:      2,
	}
	require.NoError(t, repo.Push(ctx, configDesc, bytes.NewReader([]byte(`{}`))))
	manifestBytes, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ocispec.Descriptor{layer},
	})
	require.NoError(t, err)
	manifestSum := sha256.Sum256(manifestBytes)
	manifestDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    digest.NewDigestFromEncoded(digest.SHA256, hex.EncodeToString(manifestSum[:])),
		Size:      int64(len(manifestBytes)),
	}
	require.NoError(t, repo.PushReference(ctx, manifestDesc, bytes.NewReader(manifestBytes), "v1"))

	_, _, err = grabOCI(t, v1alpha1.File{
		Source:      fmt.Sprintf("oci://%s/vendor/untitled:v1", addr),
		ExtractPath: "bin/engine",
		Target:      "/usr/local/bin/engine",
		Shasum:      payloadShasum(),
	}, ociOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), ocispec.AnnotationTitle)
}

// A registry override redirects an oci:// file source for the same reason it redirects an
// image pull: an operator who points a registry somewhere else means it for everything
// cargoship fetches from there.
func TestFetchOCIFileHonoursRegistryOverride(t *testing.T) {
	declaredAddr := test.SetupInMemoryRegistry(t)
	mirrorAddr := test.SetupInMemoryRegistry(t)
	pushArtifact(t, mirrorAddr, "vendor/engine", "v1.37.1", map[string][]artifactLayer{
		"": {{title: "engine", content: []byte(grabbedPayload)}},
	})

	declared := fmt.Sprintf("oci://%s/vendor/engine:v1.37.1", declaredAddr)
	got, provenance, err := grabOCI(t, v1alpha1.File{
		Source: declared,
		Target: "/usr/local/bin/engine",
		Shasum: payloadShasum(),
	}, ociOptions{
		RegistryOverrides: []images.RegistryOverride{{Source: declaredAddr, Override: mirrorAddr}},
	})
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, got, "nothing was pushed to the declared registry, so these bytes came from the mirror")

	require.NotNil(t, provenance, "an overridden fetch must record where the bytes came from")
	require.Equal(t, declared, provenance.Declared)
	require.Equal(t, fmt.Sprintf("oci://%s/vendor/engine:v1.37.1", mirrorAddr), provenance.Resolved)
	require.Equal(t, declaredAddr, provenance.Override)
}

// An override moves the choice of registry to whoever runs create, so something has to tie the
// bytes to what upstream published: a digest in the reference, or a declared shasum.
func TestFetchOCIFileOverrideNeedsADigestOrShasum(t *testing.T) {
	declaredAddr := test.SetupInMemoryRegistry(t)
	mirrorAddr := test.SetupInMemoryRegistry(t)
	pushed := pushArtifact(t, mirrorAddr, "vendor/engine", "v1.37.1", map[string][]artifactLayer{
		"": {{title: "engine", content: []byte(grabbedPayload)}},
	})
	overrides := []images.RegistryOverride{{Source: declaredAddr, Override: mirrorAddr}}

	t.Run("tag with no shasum is refused", func(t *testing.T) {
		_, _, err := grabOCI(t, v1alpha1.File{
			Source: fmt.Sprintf("oci://%s/vendor/engine:v1.37.1", declaredAddr),
			Target: "/usr/local/bin/engine",
		}, ociOptions{RegistryOverrides: overrides})
		require.Error(t, err)
		require.Contains(t, err.Error(), "refusing to fetch it unverified")
		require.Contains(t, err.Error(), "not pinned by digest")
	})

	t.Run("digest with no shasum is allowed", func(t *testing.T) {
		got, provenance, err := grabOCI(t, v1alpha1.File{
			Source: fmt.Sprintf("oci://%s/vendor/engine@%s", declaredAddr, pushed),
			Target: "/usr/local/bin/engine",
		}, ociOptions{RegistryOverrides: overrides})
		require.NoError(t, err)
		require.Equal(t, grabbedPayload, got)
		require.Equal(t, declaredAddr, provenance.Override)
	})
}

// An unrewritten reference is the definition author's own choice, exactly like a URL, so it
// needs no shasum -- and the layer is digest-verified against the manifest regardless.
func TestFetchOCIFileWithoutShasum(t *testing.T) {
	addr := test.SetupInMemoryRegistry(t)
	pushArtifact(t, addr, "vendor/engine", "v1.37.1", map[string][]artifactLayer{
		"": {{title: "engine", content: []byte(grabbedPayload)}},
	})

	got, _, err := grabOCI(t, v1alpha1.File{
		Source: fmt.Sprintf("oci://%s/vendor/engine:v1.37.1", addr),
		Target: "/usr/local/bin/engine",
	}, ociOptions{})
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, got)
}

func TestPinnedByDigest(t *testing.T) {
	for _, tc := range []struct {
		name string
		ref  string
		want bool
	}{
		{
			name: "tag",
			ref:  "oci://registry.example.com/vendor/engine:v1.37.1",
			want: false,
		},
		{
			name: "digest",
			ref:  "oci://registry.example.com/vendor/engine@sha256:" + shasumOf([]byte("x")),
			want: true,
		},
		{
			name: "trailing at",
			ref:  "oci://registry.example.com/vendor/engine@",
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, pinnedByDigest(tc.ref))
		})
	}
}

// A zarf or cargoship package index spells its children's platform OS "multi", because they
// are architectures of a package rather than container platforms. Matching "linux" alone reads
// ghcr.io/zarf-dev/packages/init as holding nothing at all.
func TestFetchOCIFilePlatformOSSpellings(t *testing.T) {
	for _, platformOS := range []string{"linux", "multi", ""} {
		t.Run("os="+platformOS, func(t *testing.T) {
			addr := test.SetupInMemoryRegistry(t)
			pushArtifactOS(t, addr, "vendor/engine", "v1", platformOS, map[string][]artifactLayer{
				"amd64": {{title: "engine", content: []byte(grabbedPayload)}},
				"arm64": {{title: "engine", content: []byte("arm64\n")}},
			})

			got, _, err := grabOCI(t, v1alpha1.File{
				Source:   fmt.Sprintf("oci://%s/vendor/engine:v1", addr),
				Target:   "/usr/local/bin/engine",
				Shasum:   payloadShasum(),
				Selector: v1alpha1.BinarySelector{Arch: api.Arches{api.ArchAMD64}},
			}, ociOptions{})
			require.NoError(t, err)
			require.Equal(t, grabbedPayload, got)
		})
	}
}

// A child for an OS a linux node cannot run is not a candidate, and the error says so rather
// than reporting the architecture as available.
func TestFetchOCIFileRejectsForeignPlatformOS(t *testing.T) {
	addr := test.SetupInMemoryRegistry(t)
	pushArtifactOS(t, addr, "vendor/engine", "v1", "windows", map[string][]artifactLayer{
		"amd64": {{title: "engine", content: []byte(grabbedPayload)}},
		"arm64": {{title: "engine", content: []byte("arm64\n")}},
	})

	_, _, err := grabOCI(t, v1alpha1.File{
		Source:   fmt.Sprintf("oci://%s/vendor/engine:v1", addr),
		Target:   "/usr/local/bin/engine",
		Shasum:   payloadShasum(),
		Selector: v1alpha1.BinarySelector{Arch: api.Arches{api.ArchAMD64}},
	}, ociOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "holds no amd64 manifest")
	require.Contains(t, err.Error(), "windows/amd64")
}

// An index whose children do not cover the entry's architecture names the ones it does, which
// is what tells an operator whether to fix the entry or the reference. The zarf init package's
// "skeleton" child is why this lists them rather than only reporting a miss.
func TestFetchOCIFileUnavailableArchNamesWhatIsThere(t *testing.T) {
	addr := test.SetupInMemoryRegistry(t)
	pushArtifact(t, addr, "vendor/engine", "v1", map[string][]artifactLayer{
		"amd64":    {{title: "engine", content: []byte(grabbedPayload)}},
		"skeleton": {{title: "engine", content: []byte("skeleton\n")}},
	})

	_, _, err := grabOCI(t, v1alpha1.File{
		Source:   fmt.Sprintf("oci://%s/vendor/engine:v1", addr),
		Target:   "/usr/local/bin/engine",
		Shasum:   payloadShasum(),
		Selector: v1alpha1.BinarySelector{Arch: api.Arches{api.ArchARM64}},
	}, ociOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "holds no arm64 manifest")
	require.Contains(t, err.Error(), "amd64, skeleton")
}

// An index over exactly one manifest names bytes whatever the entry selects, so requiring
// selector.arch there would refuse something unambiguous.
func TestFetchOCIFileSingleChildIndexNeedsNoArch(t *testing.T) {
	addr := test.SetupInMemoryRegistry(t)
	pushArtifact(t, addr, "vendor/engine", "v1", map[string][]artifactLayer{
		"amd64": {{title: "engine", content: []byte(grabbedPayload)}},
	})

	got, _, err := grabOCI(t, v1alpha1.File{
		Source: fmt.Sprintf("oci://%s/vendor/engine:v1", addr),
		Target: "/usr/local/bin/engine",
		Shasum: payloadShasum(),
	}, ociOptions{})
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, got)
}

// The shape of a zarf component tarball end to end: an index spelling its platform "multi",
// one titled .tar layer, and an extractPath whose leading segment is the target's own base
// name -- which is what the staged rename used to collide with.
func TestFetchOCIFileZarfComponentShape(t *testing.T) {
	addr := test.SetupInMemoryRegistry(t)
	component := tarGz(t, tarGzEntry{name: "zarf-injector/files/0/zarf-injector", content: grabbedPayload})
	pushArtifactOS(t, addr, "zarf-dev/packages/init", "v0.89.0", "multi", map[string][]artifactLayer{
		"amd64":    {{title: "components/zarf-injector.tar.gz", content: component}},
		"skeleton": {{title: "components/zarf-injector.tar.gz", content: []byte("not a tarball")}},
	})

	got, _, err := grabOCI(t, v1alpha1.File{
		Source:      fmt.Sprintf("oci://%s/zarf-dev/packages/init:v0.89.0", addr),
		Layer:       "components/zarf-injector.tar.gz",
		ExtractPath: "zarf-injector/files/0/zarf-injector",
		Target:      "/usr/local/bin/zarf-injector",
		Executable:  true,
		Shasum:      payloadShasum(),
		Selector:    v1alpha1.BinarySelector{Arch: api.Arches{api.ArchAMD64}},
	}, ociOptions{})
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, got)
}
