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
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/coci"
	"github.com/colonel-byte/cargoship/test"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/types"
	"oras.land/oras-go/v2/registry"
	orasRemote "oras.land/oras-go/v2/registry/remote"
)

// plainHTTPOptions is what every test here mirrors with: the in-memory registry speaks plain
// HTTP and nothing else.
func plainHTTPOptions() MirrorOptions {
	return MirrorOptions{RemoteOptions: types.RemoteOptions{PlainHTTP: true}}
}

func plainHTTPListOptions() ListMirroredOptions {
	return ListMirroredOptions{RemoteOptions: types.RemoteOptions{PlainHTTP: true}}
}

// TestMirrorRecordRef pins the one rule the write and the read halves both apply.
func TestMirrorRecordRef(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		pkg     string
		version string
		want    string
		wantErr string
	}{
		{
			name:    "a bare registry",
			target:  "harbor.internal",
			pkg:     "rke2",
			version: "0.1.0",
			want:    "harbor.internal/cargoship/rke2:0.1.0",
		},
		{
			name:    "a repository prefix is kept, because that is where the mirror itself went",
			target:  "harbor.internal/fleet",
			pkg:     "rke2",
			version: "0.1.0",
			want:    "harbor.internal/fleet/cargoship/rke2:0.1.0",
		},
		{
			name:    "a trailing slash does not double",
			target:  "harbor.internal/fleet/",
			pkg:     "k3s",
			version: "1.36.1",
			want:    "harbor.internal/fleet/cargoship/k3s:1.36.1",
		},
		{
			name:    "no registry",
			pkg:     "rke2",
			version: "0.1.0",
			wantErr: "target registry is required",
		},
		{
			name:    "no version to tag it with",
			target:  "harbor.internal",
			pkg:     "rke2",
			wantErr: "needs both a package name and a version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MirrorRecordRef(tt.target, tt.pkg, tt.version)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestMirrorRecord covers the artifact a finished mirror leaves behind. Its config media type is
// what classifies it, so that is what the read half matches on rather than the repository name.
func TestMirrorRecord(t *testing.T) {
	ctx := context.Background()
	l := mirrorLayout(t)
	host := test.SetupInMemoryRegistry(t)

	require.NoError(t, Mirror(ctx, l, host, plainHTTPOptions()))

	ref, err := MirrorRecordRef(host, "rke2", "0.1.0")
	require.NoError(t, err)
	manifest, repo := fetchManifest(t, ref)

	require.Equal(t, coci.CargoshipMirrorConfigMediaType, manifest.Config.MediaType,
		"the config media type is what marks this a mirror record")

	var record MirrorRecord
	require.NoError(t, json.Unmarshal(fetchBlob(ctx, t, repo, manifest.Config), &record))
	require.Equal(t, MirrorRecordSchemaVersion, record.SchemaVersion)
	require.Equal(t, "rke2", record.Name)
	require.Equal(t, "0.1.0", record.Version)
	require.Equal(t, []string{"amd64", "arm64"}, record.Arches)
	require.Equal(t, "rke2", record.Engine)
	require.False(t, record.MirroredAt.IsZero())
	require.Equal(t, 1, record.Images, "the fixture carries one image")
	require.Equal(t, 2, record.Files, "and two files with a source; the inline one is not mirrored")

	t.Run("the layer is the package's own definition", func(t *testing.T) {
		require.Len(t, manifest.Layers, 1)
		require.Equal(t, config.DistroYAML, manifest.Layers[0].Annotations[ocispec.AnnotationTitle])

		want, err := os.ReadFile(filepath.Join(l.DirPath(), config.DistroYAML))
		require.NoError(t, err)
		require.Equal(t, want, fetchBlob(ctx, t, repo, manifest.Layers[0]))
	})
}

// TestListMirroredTwoVersions is the question the record exists to answer: a registry can hold
// two versions of one package, because the record's tag is the version.
func TestListMirroredTwoVersions(t *testing.T) {
	ctx := context.Background()
	host := test.SetupInMemoryRegistry(t)

	require.NoError(t, Mirror(ctx, mirrorLayoutVersion(t, "1.35.6"), host, plainHTTPOptions()))
	require.NoError(t, Mirror(ctx, mirrorLayoutVersion(t, "1.36.1"), host, plainHTTPOptions()))

	t.Run("the catalogue lists both", func(t *testing.T) {
		got, err := ListMirrored(ctx, host, plainHTTPListOptions())
		require.NoError(t, err)
		require.Len(t, got, 2)

		versions := []string{got[0].Version, got[1].Version}
		require.ElementsMatch(t, []string{"1.35.6", "1.36.1"}, versions)
		for _, r := range got {
			require.Equal(t, host+"/cargoship/rke2:"+r.Version, r.Reference,
				"the reference a record was read from is reported, since the stored JSON cannot know it")
		}
	})

	t.Run("a name reads one repository instead", func(t *testing.T) {
		opts := plainHTTPListOptions()
		opts.Name = "rke2"
		got, err := ListMirrored(ctx, host, opts)
		require.NoError(t, err)
		require.Len(t, got, 2)
	})

	t.Run("a name nothing was mirrored under lists nothing", func(t *testing.T) {
		opts := plainHTTPListOptions()
		opts.Name = "k3s"
		got, err := ListMirrored(ctx, host, opts)
		require.NoError(t, err)
		require.Empty(t, got)
	})
}

// TestListMirroredSkipsForeignArtifacts covers a registry somebody else also writes into. The
// reserved prefix makes a collision unlikely, not impossible, and a listing that failed on one
// would be a listing an operator cannot use.
func TestListMirroredSkipsForeignArtifacts(t *testing.T) {
	ctx := context.Background()
	host := test.SetupInMemoryRegistry(t)

	require.NoError(t, Mirror(ctx, mirrorLayout(t), host, plainHTTPOptions()))

	ref := host + "/cargoship/something-else:1.0"
	parsed, err := registry.ParseReference(ref)
	require.NoError(t, err)

	foreign := filepath.Join(t.TempDir(), "payload.txt")
	require.NoError(t, os.WriteFile(foreign, []byte("not a mirror record\n"), 0o644))
	layerDesc, open, err := describeFile(foreign)
	require.NoError(t, err)

	require.NoError(t, pushArtifact(ctx, &orasRemote.Repository{Reference: parsed, PlainHTTP: true}, artifact{
		ref:             ref,
		configMediaType: "application/vnd.example.thing.v1+json",
		config:          []byte(`{}`),
		layer:           layerDesc,
		open:            open,
	}, 1))

	got, err := ListMirrored(ctx, host, plainHTTPListOptions())
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "rke2", got[0].Name)
}

// TestMirrorRecordHalves covers the two cases that would otherwise leave a wrong record: a run
// given one half of the work, and a run that failed.
func TestMirrorRecordHalves(t *testing.T) {
	ctx := context.Background()

	t.Run("a run given only --images still records itself", func(t *testing.T) {
		host := test.SetupInMemoryRegistry(t)
		opts := plainHTTPOptions()
		opts.Images = true

		require.NoError(t, Mirror(ctx, mirrorLayout(t), host, opts))

		got, err := ListMirrored(ctx, host, plainHTTPListOptions())
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, 1, got[0].Images)
		require.Zero(t, got[0].Files, "a count is what this run pushed, not what the package carries")
	})

	t.Run("a mirror that failed records nothing", func(t *testing.T) {
		host := test.SetupInMemoryRegistry(t)
		l := mirrorLayout(t)
		// An unparseable reference fails the image half before anything is pushed, which is
		// the shape of every mirror that gets part way through.
		l.Distro.Spec.Config.ImagesConfig.Images = []string{"NOT A REFERENCE"}

		require.Error(t, Mirror(ctx, l, host, plainHTTPOptions()))

		got, err := ListMirrored(ctx, host, plainHTTPListOptions())
		require.NoError(t, err)
		require.Empty(t, got, "a record is a claim the content is there, so a failed mirror must leave none")
	})

	t.Run("a package with no version is reported rather than failing the mirror", func(t *testing.T) {
		host := test.SetupInMemoryRegistry(t)
		l := mirrorLayout(t)
		l.Distro.Metadata.Version = ""

		require.NoError(t, Mirror(ctx, l, host, plainHTTPOptions()))

		got, err := ListMirrored(ctx, host, plainHTTPListOptions())
		require.NoError(t, err)
		require.Empty(t, got, "there is no tag to record it under")
	})
}

// fetchBlob reads one descriptor's content out of the registry under test.
func fetchBlob(ctx context.Context, t *testing.T, repo *orasRemote.Repository, desc ocispec.Descriptor) []byte {
	t.Helper()
	rc, err := repo.Fetch(ctx, desc)
	require.NoError(t, err)
	defer func() { require.NoError(t, rc.Close()) }()
	body, err := io.ReadAll(rc)
	require.NoError(t, err)
	return body
}
