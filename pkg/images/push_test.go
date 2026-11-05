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

package images

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/colonel-byte/cargoship/test"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/pkg/transform"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry"
	orasRemote "oras.land/oras-go/v2/registry/remote"
)

// writeIndex stores an image index over the given manifests, the shape a multi-architecture
// package tags, and returns its descriptor.
func writeIndex(ctx context.Context, t *testing.T, store *oci.Store, manifests ...ocispec.Descriptor) ocispec.Descriptor {
	t.Helper()
	body, err := json.Marshal(ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: manifests,
	})
	require.NoError(t, err)
	return pushBlob(ctx, t, store, ocispec.MediaTypeImageIndex, body)
}

// withPlatform is what ensurePlatform does during a real pull: a registry serving a bare
// manifest returns no platform, and an index's children need one.
func withPlatform(desc ocispec.Descriptor, arch string) ocispec.Descriptor {
	desc.Platform = &ocispec.Platform{OS: "linux", Architecture: arch}
	return desc
}

// sourceLayout builds an OCI layout shaped like a package's images/ directory: one
// single-architecture image, one multi-architecture index, and one image under a namespace.
func sourceLayout(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	store, err := oci.NewWithContext(ctx, dir)
	require.NoError(t, err)

	// One amd64 manifest serves both single-architecture references. Two references pointing at
	// one manifest is what a real layout holds whenever two images share content, and it is also
	// the only option here: pushManifest is content-addressed, so a second identical arch would
	// collide on the digest it already stored.
	amd64 := pushManifest(ctx, t, store, "amd64")
	arm64 := pushManifest(ctx, t, store, "arm64")

	require.NoError(t, store.Tag(ctx, amd64, "registry.k8s.io/pause:3.10"))
	require.NoError(t, store.Tag(ctx, amd64, "ghcr.io/colonel-byte/cargoship/agent:v1.2.3"))

	index := writeIndex(ctx, t, store, withPlatform(amd64, "amd64"), withPlatform(arm64, "arm64"))
	require.NoError(t, store.Tag(ctx, index, "registry.k8s.io/etcd:3.5.21-0"))

	return dir
}

func mustParse(t *testing.T, refs ...string) []transform.Image {
	t.Helper()
	out := make([]transform.Image, 0, len(refs))
	for _, ref := range refs {
		img, err := transform.ParseImageRef(ref)
		require.NoError(t, err)
		out = append(out, img)
	}
	return out
}

// resolve reads back a reference from the registry under test.
func resolve(t *testing.T, ref string) ocispec.Descriptor {
	t.Helper()
	parsed, err := registry.ParseReference(ref)
	require.NoError(t, err)
	repo := &orasRemote.Repository{Reference: parsed, PlainHTTP: true}
	desc, err := repo.Resolve(context.Background(), ref)
	require.NoError(t, err, "expected %s to be in the registry", ref)
	return desc
}

// TestPush covers the reference rewriting a mirror depends on. The rule is a pure host swap,
// because a containerd hosts.toml mirror asks for the repository path unchanged -- so a
// namespace that moves, or a tag that gains a suffix, is a mirror the node never hits.
func TestPush(t *testing.T) {
	ctx := context.Background()
	dir := sourceLayout(t)

	t.Run("host is swapped and namespace and tag are preserved", func(t *testing.T) {
		host := test.SetupInMemoryRegistry(t)
		images := mustParse(t,
			"registry.k8s.io/pause:3.10",
			"ghcr.io/colonel-byte/cargoship/agent:v1.2.3",
		)
		require.NoError(t, Push(ctx, images, dir, host, PushOptions{PlainHTTP: true}))

		resolve(t, host+"/pause:3.10")
		resolve(t, host+"/colonel-byte/cargoship/agent:v1.2.3")
	})

	t.Run("a repository prefix is kept ahead of the namespace", func(t *testing.T) {
		host := test.SetupInMemoryRegistry(t)
		target := host + "/cargoship"
		images := mustParse(t, "registry.k8s.io/pause:3.10", "ghcr.io/colonel-byte/cargoship/agent:v1.2.3")
		require.NoError(t, Push(ctx, images, dir, target, PushOptions{PlainHTTP: true}))

		resolve(t, target+"/pause:3.10")
		resolve(t, target+"/colonel-byte/cargoship/agent:v1.2.3")
	})

	t.Run("a multi-architecture image is pushed as an index carrying every platform", func(t *testing.T) {
		host := test.SetupInMemoryRegistry(t)
		images := mustParse(t, "registry.k8s.io/etcd:3.5.21-0")
		require.NoError(t, Push(ctx, images, dir, host, PushOptions{PlainHTTP: true}))

		desc := resolve(t, host+"/etcd:3.5.21-0")
		require.Equal(t, ocispec.MediaTypeImageIndex, desc.MediaType,
			"the index must be pushed whole, not flattened to one platform")

		parsed, err := registry.ParseReference(host + "/etcd:3.5.21-0")
		require.NoError(t, err)
		repo := &orasRemote.Repository{Reference: parsed, PlainHTTP: true}
		rc, err := repo.Fetch(ctx, desc)
		require.NoError(t, err)
		defer func() { require.NoError(t, rc.Close()) }()

		var index ocispec.Index
		require.NoError(t, json.NewDecoder(rc).Decode(&index))
		require.Len(t, index.Manifests, 2)

		arches := []string{}
		for _, m := range index.Manifests {
			require.NotNil(t, m.Platform, "every child needs a platform for a node to select on")
			arches = append(arches, m.Platform.Architecture)
		}
		require.ElementsMatch(t, []string{"amd64", "arm64"}, arches)
	})

	t.Run("the pushed digest is the one the package held", func(t *testing.T) {
		host := test.SetupInMemoryRegistry(t)
		src, err := oci.New(dir)
		require.NoError(t, err)
		want, err := src.Resolve(ctx, "registry.k8s.io/pause:3.10")
		require.NoError(t, err)

		require.NoError(t, Push(ctx, mustParse(t, "registry.k8s.io/pause:3.10"), dir, host, PushOptions{PlainHTTP: true}))

		require.Equal(t, want.Digest, resolve(t, host+"/pause:3.10").Digest,
			"a mirror must hold the same bytes the package was tested with")
	})

	t.Run("an image the package does not hold fails rather than being skipped", func(t *testing.T) {
		host := test.SetupInMemoryRegistry(t)
		err := Push(ctx, mustParse(t, "registry.k8s.io/absent:1.0"), dir, host, PushOptions{PlainHTTP: true})
		require.Error(t, err)
		require.Contains(t, err.Error(), "registry.k8s.io/absent:1.0")
	})

	t.Run("bad input is refused", func(t *testing.T) {
		require.ErrorContains(t, Push(ctx, nil, dir, "example.com", PushOptions{}), "image list is required")
		images := mustParse(t, "registry.k8s.io/pause:3.10")
		require.ErrorContains(t, Push(ctx, images, "", "example.com", PushOptions{}), "source directory is required")
		require.ErrorContains(t, Push(ctx, images, dir, "", PushOptions{}), "target registry is required")
		require.ErrorContains(t, Push(ctx, images, dir, "NOT A HOST", PushOptions{}), "invalid target registry")
	})
}

// TestGraphSize pins the total a push reports progress against. A blob reachable from two
// architectures is moved once, so counting it twice would report a push as half done when it
// had finished.
func TestGraphSize(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := oci.NewWithContext(ctx, dir)
	require.NoError(t, err)

	manifest := pushManifest(ctx, t, store, "amd64")
	index := writeIndex(ctx, t, store, manifest, manifest)

	manifestSize, err := graphSize(ctx, store, manifest)
	require.NoError(t, err)
	indexSize, err := graphSize(ctx, store, index)
	require.NoError(t, err)

	require.Equal(t, index.Size+manifestSize, indexSize,
		"a manifest listed twice in an index must be counted once")
}
