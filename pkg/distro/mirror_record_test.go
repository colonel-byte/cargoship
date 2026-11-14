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
	"strings"
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
func TestRecordRef(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		target  string
		kind    string
		pkg     string
		version string
		want    string
		wantErr string
	}{
		{
			name:    "a mirror record in a bare registry",
			target:  "harbor.internal",
			kind:    RecordKindMirror,
			pkg:     "rke2",
			version: "0.1.0",
			want:    "harbor.internal/cargoship:_mirror.rke2.0.1.0",
		},
		{
			name:    "a package record, which is the one a consumer pulls",
			target:  "ghcr.io/colonel-byte",
			kind:    RecordKindPackage,
			pkg:     "rancher-rke2-multi-cni-cilium",
			version: "1.36.0-rke2r1",
			want:    "ghcr.io/colonel-byte/cargoship:_package.rancher-rke2-multi-cni-cilium.1.36.0-rke2r1",
		},
		{
			name:    "a repository prefix is kept, because that is where the content itself went",
			target:  "harbor.internal/fleet",
			kind:    RecordKindMirror,
			pkg:     "rke2",
			version: "0.1.0",
			want:    "harbor.internal/fleet/cargoship:_mirror.rke2.0.1.0",
		},
		{
			name:    "a trailing slash does not double",
			target:  "harbor.internal/fleet/",
			kind:    RecordKindMirror,
			pkg:     "k3s",
			version: "1.36.1",
			want:    "harbor.internal/fleet/cargoship:_mirror.k3s.1.36.1",
		},
		{
			name:    "no registry",
			kind:    RecordKindMirror,
			pkg:     "rke2",
			version: "0.1.0",
			wantErr: "target registry is required",
		},
		{
			name:    "no version to tag it with",
			target:  "harbor.internal",
			kind:    RecordKindMirror,
			pkg:     "rke2",
			wantErr: "needs both a package name and a version",
		},
		{
			name:    "an unknown kind has no tag prefix to take",
			target:  "harbor.internal",
			kind:    "something-else",
			pkg:     "rke2",
			version: "0.1.0",
			wantErr: `unknown record kind "something-else"`,
		},
		{
			name:    "a name past the bound would not fit the tag",
			target:  "harbor.internal",
			kind:    RecordKindMirror,
			pkg:     strings.Repeat("a", 65),
			version: "0.1.0",
			wantErr: "over the 64 allowed",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := RecordRef(tt.target, tt.kind, tt.pkg, tt.version)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
			require.LessOrEqual(t, len(strings.SplitN(got, ":", 2)[1]), 128,
				"a tag is at most 128 characters, which is what the name and version bounds are sized for")
		})
	}
}

// TestKindForTagAndName covers reading a record's kind and package back out of its tag, which is
// what lets a listing skip a tag without fetching the manifest behind it.
func TestKindForTagAndName(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		tag      string
		wantKind string
		wantIs   bool
		wantName string
	}{
		{
			name:     "a mirror record",
			tag:      "_mirror.rke2.0.1.0",
			wantKind: RecordKindMirror,
			wantIs:   true,
			wantName: "rke2",
		},
		{
			name:     "a package record whose version holds dots",
			tag:      "_package.rancher-k3s-multi.1.36.4-k3s1",
			wantKind: RecordKindPackage,
			wantIs:   true,
			wantName: "rancher-k3s-multi",
		},
		{
			// A package published into the same repository, which is the collision the
			// underscore prefix exists to avoid.
			name:   "a plain version tag is not a record",
			tag:    "1.36.4-k3s1",
			wantIs: false,
		},
		{
			name:   "something else underscored is not a record either",
			tag:    "_notarecord.rke2.0.1.0",
			wantIs: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			kind, ok := kindForTag(tt.tag)
			require.Equal(t, tt.wantIs, ok)
			if !tt.wantIs {
				return
			}
			require.Equal(t, tt.wantKind, kind)
			require.Equal(t, tt.wantName, recordTagName(tt.tag, kind))
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

	ref, err := RecordRef(host, RecordKindMirror, "rke2", "0.1.0")
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
			require.Equal(t, host+"/cargoship:_mirror.rke2."+r.Version, r.Reference,
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

// TestDiscoverNewestVersion is the question a consumer actually asks: given a package name and a
// registry, where do I pull the newest one from.
//
// It is here because that question is the reason the layout changed. Records used to live one
// repository per package, so answering it meant walking the registry's catalogue -- which ghcr.io
// refuses outright and Docker Hub does not implement. One repository makes it a single tag query,
// and the tag carries the kind and the package so the query can be narrowed before any manifest
// is fetched.
func TestDiscoverNewestVersion(t *testing.T) {
	ctx := context.Background()
	host := test.SetupInMemoryRegistry(t)

	for _, v := range []string{"1.35.10", "1.36.1", "1.36.4"} {
		require.NoError(t, Mirror(ctx, mirrorLayoutVersion(t, v), host, plainHTTPOptions()))
	}
	// A second package, so the scoping is actually doing something.
	other := mirrorLayoutVersion(t, "9.9.9")
	other.Distro.Metadata.Name = "k3s"
	require.NoError(t, Mirror(ctx, other, host, plainHTTPOptions()))

	t.Run("every record is found without reading the catalogue", func(t *testing.T) {
		all, err := ListMirrored(ctx, host, listOptions(""))
		require.NoError(t, err)
		require.Len(t, all, 4)
	})

	t.Run("scoped to one package, newest first", func(t *testing.T) {
		found, err := ListMirrored(ctx, host, listOptions("rke2"))
		require.NoError(t, err)

		versions := make([]string, 0, len(found))
		for _, r := range found {
			versions = append(versions, r.Version)
			require.Equal(t, "rke2", r.Name)
			require.Equal(t, RecordKindMirror, r.Kind, "a mirror records itself as one")
		}
		require.Equal(t, []string{"1.36.4", "1.36.1", "1.35.10"}, versions,
			"newest first, so the first record is the answer to 'which is latest'")
	})

	t.Run("the record names where its content is", func(t *testing.T) {
		found, err := ListMirrored(ctx, host, listOptions("rke2"))
		require.NoError(t, err)
		require.NotEmpty(t, found)

		newest := found[0]
		require.Equal(t, host+"/cargoship:_mirror.rke2.1.36.4", newest.Reference,
			"the reference a record was read from, which is what a consumer fetches to get the distro.yaml behind it")
		require.Equal(t, "rke2", newest.Name, "the record is the authority on the name, not the tag it was parsed from")
	})
}

// listOptions is the plain-HTTP listing the in-memory registry needs, optionally scoped.
func listOptions(name string) ListMirroredOptions {
	return ListMirroredOptions{
		RemoteOptions: plainHTTPOptions().RemoteOptions,
		Name:          name,
	}
}

// TestPublishIsDiscoverable covers the half a consumer looking for a package to pull actually
// needs. `cargoship mirror` unpacks content into a registry; `cargoship publish` puts the package
// there as one artifact, and until it recorded itself `cargoship ls` could not see it at all --
// so "where do I pull the newest rancher-rke2-multi-cni-cilium" had no answer, however the
// records were laid out.
//
// It drives writePublishRecord rather than Publish, because a publishable layout needs a computed
// manifest cache and no fixture in this package builds one -- Publish's own tests cover only its
// error paths for the same reason. What is asserted is everything writePublishRecord decides; that
// Publish calls it after a successful push is one line and read rather than exercised.
func TestPublishIsDiscoverable(t *testing.T) {
	ctx := context.Background()
	host := test.SetupInMemoryRegistry(t)
	l := mirrorLayoutVersion(t, "1.36.0-rke2r1")

	writePublishRecord(ctx, l, registry.Reference{Registry: host}, registry.Reference{
		Registry:   host,
		Repository: "rke2",
		Reference:  "1.36.0-rke2r1",
	}, PublishOptions{
		Retries:       1,
		RemoteOptions: plainHTTPOptions().RemoteOptions,
	})

	found, err := ListMirrored(ctx, host, listOptions("rke2"))
	require.NoError(t, err)
	require.Len(t, found, 1, "a publish should leave exactly one record")

	require.Equal(t, RecordKindPackage, found[0].Kind,
		"the kind is what tells a consumer to pull this whole rather than point nodes at it")
	require.Equal(t, "1.36.0-rke2r1", found[0].Version)
	require.Equal(t, host+"/cargoship:_package.rke2.1.36.0-rke2r1", found[0].Reference)
}

// TestPublishRecordUsesTheTagItWasPublishedUnder covers --tag, which publishes a package under
// something other than its own version. The record is keyed on what was written, because a record
// pointing at a reference nobody can pull is worse than no record.
func TestPublishRecordUsesTheTagItWasPublishedUnder(t *testing.T) {
	ctx := context.Background()
	host := test.SetupInMemoryRegistry(t)
	l := mirrorLayoutVersion(t, "1.36.0-rke2r1")

	writePublishRecord(ctx, l, registry.Reference{Registry: host}, registry.Reference{
		Registry:   host,
		Repository: "rke2",
		Reference:  "edge",
	}, PublishOptions{
		Retries:       1,
		Tag:           "edge",
		RemoteOptions: plainHTTPOptions().RemoteOptions,
	})

	found, err := ListMirrored(ctx, host, listOptions("rke2"))
	require.NoError(t, err)
	require.Len(t, found, 1)
	require.Equal(t, "edge", found[0].Version, "the record follows the tag, not the metadata version")
	require.Equal(t, host+"/cargoship:_package.rke2.edge", found[0].Reference)
}

// TestRecordingIsGated covers the two defaults, which are deliberately opposite. A mirror has
// recorded itself since the feature existed, so it keeps doing so and --no-record is the way out;
// publishing is the new half, so it records nothing unless asked and a registry somebody already
// publishes into does not quietly gain an artifact per package on an upgrade.
func TestRecordingIsGated(t *testing.T) {
	t.Run("a mirror records by default", func(t *testing.T) {
		ctx := context.Background()
		host := test.SetupInMemoryRegistry(t)
		require.NoError(t, Mirror(ctx, mirrorLayout(t), host, plainHTTPOptions()))

		found, err := ListMirrored(ctx, host, listOptions(""))
		require.NoError(t, err)
		require.Len(t, found, 1)
	})

	t.Run("--no-record skips it, and the content still lands", func(t *testing.T) {
		ctx := context.Background()
		host := test.SetupInMemoryRegistry(t)
		opts := plainHTTPOptions()
		opts.NoRecord = true
		require.NoError(t, Mirror(ctx, mirrorLayout(t), host, opts))

		found, err := ListMirrored(ctx, host, listOptions(""))
		require.NoError(t, err)
		require.Empty(t, found, "nothing recorded, so a listing sees an empty registry")
	})

	t.Run("a publish records nothing unless asked", func(t *testing.T) {
		ctx := context.Background()
		host := test.SetupInMemoryRegistry(t)
		l := mirrorLayout(t)

		// Publish's own gate, which is the one line writePublishRecord sits behind.
		opts := PublishOptions{Retries: 1, RemoteOptions: plainHTTPOptions().RemoteOptions}
		require.False(t, opts.Record, "the zero value is off, which is what makes it opt-in")
		if opts.Record {
			writePublishRecord(ctx, l, registry.Reference{Registry: host}, registry.Reference{Registry: host}, opts)
		}

		found, err := ListMirrored(ctx, host, listOptions(""))
		require.NoError(t, err)
		require.Empty(t, found)
	})
}
