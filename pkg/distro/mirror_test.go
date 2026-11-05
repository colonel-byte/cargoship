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
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	v1alpha1 "github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1"
	distroapi "github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/contentstore"
	"github.com/colonel-byte/cargoship/pkg/haulerstore"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/test"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/types"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry"
	orasRemote "oras.land/oras-go/v2/registry/remote"
)

const (
	installSource = "https://get.rke2.io/install.sh"
	sysctlSource  = "https://example.com/90-cargoship.conf"
)

// mirrorLayout stages a package on disk the way a build leaves one: an images/ OCI layout, and
// each file under files/<index>/<basename of target>.
func mirrorLayout(t *testing.T) *layout.DistroLayout {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()

	store, err := oci.NewWithContext(ctx, filepath.Join(dir, config.ImagesDir))
	require.NoError(t, err)

	cfg := []byte(`{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}`)
	cfgDesc := ocispec.Descriptor{MediaType: ocispec.MediaTypeImageConfig, Digest: digest.FromBytes(cfg), Size: int64(len(cfg))}
	require.NoError(t, store.Push(ctx, cfgDesc, bytes.NewReader(cfg)))

	manifest, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    cfgDesc,
		Layers:    []ocispec.Descriptor{},
	})
	require.NoError(t, err)
	manifestDesc := ocispec.Descriptor{MediaType: ocispec.MediaTypeImageManifest, Digest: digest.FromBytes(manifest), Size: int64(len(manifest))}
	require.NoError(t, store.Push(ctx, manifestDesc, bytes.NewReader(manifest)))
	require.NoError(t, store.Tag(ctx, manifestDesc, "registry.k8s.io/pause:3.10"))

	// The staged path is <subdir>/<index in the declared list>/<basename of target>, which is
	// the convention the build writes and every reader of a package rebuilds.
	stage := func(subdir string, index int, target, body string) {
		dest := filepath.Join(dir, subdir, strconv.Itoa(index), filepath.Base(target))
		require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o755))
		require.NoError(t, os.WriteFile(dest, []byte(body), 0o644))
	}
	stage(config.FilesDir, 0, "/opt/rke2/install.sh", "#!/bin/sh\necho install\n")
	stage(config.OSDir, 0, "/etc/sysctl.d/90-cargoship.conf", "vm.max_map_count=262144\n")

	dis := distroapi.Distro{
		Spec: distroapi.Spec{
			Config: distroapi.Config{
				ImagesConfig: distroapi.ImageConfig{Images: []string{"registry.k8s.io/pause:3.10"}},
				Files: v1alpha1.Files{
					{Source: installSource, Target: "/opt/rke2/install.sh"},
					// No source: nothing to mirror it under, and nothing to match it by.
					{Target: "/etc/motd", Data: "welcome\n"},
				},
				OS: distroapi.OS{
					Files: v1alpha1.Files{
						{Source: sysctlSource, Target: "/etc/sysctl.d/90-cargoship.conf"},
					},
				},
			},
		},
	}
	return layout.NewDistroLayout(dir, dis)
}

// fetchManifest reads back a reference from the registry under test.
func fetchManifest(t *testing.T, ref string) (ocispec.Manifest, *orasRemote.Repository) {
	t.Helper()
	parsed, err := registry.ParseReference(ref)
	require.NoError(t, err)
	repo := &orasRemote.Repository{Reference: parsed, PlainHTTP: true}

	desc, err := repo.Resolve(context.Background(), ref)
	require.NoError(t, err, "expected %s in the registry", ref)
	rc, err := repo.Fetch(context.Background(), desc)
	require.NoError(t, err)
	defer func() { require.NoError(t, rc.Close()) }()

	var manifest ocispec.Manifest
	require.NoError(t, json.NewDecoder(rc).Decode(&manifest))
	return manifest, repo
}

// TestMirrorFiles covers the shape a mirrored file takes. It is hauler's shape on purpose, so the
// assertions here are the ones pkg/haulerstore's classifier makes on the way back in.
func TestMirrorFiles(t *testing.T) {
	ctx := context.Background()
	l := mirrorLayout(t)
	host := test.SetupInMemoryRegistry(t)

	require.NoError(t, Mirror(ctx, l, host, MirrorOptions{
		Files:         true,
		RemoteOptions: types.RemoteOptions{PlainHTTP: true},
	}))

	t.Run("a file lands at the reference the shared rule computes", func(t *testing.T) {
		ref, err := contentstore.FileRef(host, installSource)
		require.NoError(t, err)

		manifest, repo := fetchManifest(t, ref)
		require.Equal(t, haulerstore.MediaTypeFileConfigHTTP, manifest.Config.MediaType,
			"the config media type is what marks this a file rather than an image")
		require.Equal(t, installSource, manifest.Annotations[haulerstore.AnnotationOriginalRef],
			"the declared source is the provenance a reader matches on")

		require.Len(t, manifest.Layers, 1)
		require.Equal(t, "install.sh", manifest.Layers[0].Annotations[ocispec.AnnotationTitle],
			"the layer title is the name the file is restored under")

		rc, err := repo.Fetch(ctx, manifest.Layers[0])
		require.NoError(t, err)
		defer func() { require.NoError(t, rc.Close()) }()
		body, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.Equal(t, "#!/bin/sh\necho install\n", string(body))
	})

	t.Run("an os file is mirrored the same way", func(t *testing.T) {
		ref, err := contentstore.FileRef(host, sysctlSource)
		require.NoError(t, err)

		manifest, _ := fetchManifest(t, ref)
		require.Equal(t, sysctlSource, manifest.Annotations[haulerstore.AnnotationOriginalRef])
		require.Equal(t, "90-cargoship.conf", manifest.Layers[0].Annotations[ocispec.AnnotationTitle])
	})

	t.Run("a file with no source is skipped rather than failing the mirror", func(t *testing.T) {
		// Mirror returned no error above, which is the assertion. Confirm nothing was invented
		// for the inline file: there is no source to build a reference from at all.
		_, err := contentstore.FileRef(host, "")
		require.Error(t, err)
	})
}

// TestMirrorImages checks the images half reaches the registry through Mirror, with the host
// swapped and the path kept. images.Push covers the rewriting rules in detail.
func TestMirrorImages(t *testing.T) {
	ctx := context.Background()
	l := mirrorLayout(t)
	host := test.SetupInMemoryRegistry(t)

	require.NoError(t, Mirror(ctx, l, host, MirrorOptions{
		Images:        true,
		RemoteOptions: types.RemoteOptions{PlainHTTP: true},
	}))

	parsed, err := registry.ParseReference(host + "/pause:3.10")
	require.NoError(t, err)
	repo := &orasRemote.Repository{Reference: parsed, PlainHTTP: true}
	_, err = repo.Resolve(ctx, host+"/pause:3.10")
	require.NoError(t, err, "the image must be reachable at the path a containerd mirror asks for")
}

func TestMirrorRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	require.ErrorContains(t, Mirror(ctx, nil, "example.com", MirrorOptions{}), "package layout is required")
	require.ErrorContains(t, Mirror(ctx, mirrorLayout(t), "", MirrorOptions{}), "target registry is required")
}

// TestDescribeFile pins the descriptor a mirrored file is announced with. The digest and the size
// have to come from the file on disk, because a registry rejects content that does not match what
// it was told to expect -- and the opener has to be good for more than one read, since a retry
// after a partial upload starts again from the first byte.
func TestDescribeFile(t *testing.T) {
	body := []byte("vm.max_map_count=262144\n")
	path := filepath.Join(t.TempDir(), "90-cargoship.conf")
	require.NoError(t, os.WriteFile(path, body, 0o644))

	desc, open, err := describeFile(path)
	require.NoError(t, err)
	require.Equal(t, digest.FromBytes(body), desc.Digest)
	require.Equal(t, int64(len(body)), desc.Size)
	require.Equal(t, ocispec.MediaTypeImageLayer, desc.MediaType)

	for attempt := range 2 {
		r, err := open()
		require.NoError(t, err, "attempt %d", attempt)
		got, err := io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
		require.Equal(t, body, got, "every attempt must read the whole file from the start")
	}
}

func TestDescribeFileMissingFile(t *testing.T) {
	_, _, err := describeFile(filepath.Join(t.TempDir(), "absent"))
	require.ErrorContains(t, err, "unable to read the staged file")
}
