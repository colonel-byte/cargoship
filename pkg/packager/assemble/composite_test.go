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
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/colonel-byte/cargoship/pkg/contentstore"
	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
)

// fakeStore is a ContentStore with no bundle behind it: enough to pin how a composite routes
// a request, without building two real archives for every case.
type fakeStore struct {
	source  string
	files   map[contentstore.FileRequest]string
	fileErr error
	images  map[string]ocispec.Descriptor
	blobs   map[string][]byte
	fetched *int
}

func (f *fakeStore) File(req contentstore.FileRequest) (string, bool, error) {
	if f.fileErr != nil {
		return "", false, f.fileErr
	}
	name, ok := f.files[req]
	return name, ok, nil
}

func (f *fakeStore) Fetch(_ context.Context, req contentstore.FileRequest, _ string) error {
	if f.fetched != nil {
		*f.fetched++
	}
	if _, ok := f.files[req]; !ok {
		return errors.New("not held")
	}
	return nil
}

func (f *fakeStore) HasImage(ref string) bool {
	_, ok := f.images[ref]
	return ok
}

func (f *fakeStore) Target() oras.ReadOnlyTarget { return fakeTarget{store: f} }
func (f *fakeStore) Source() string              { return f.source }
func (f *fakeStore) Describe() string            { return "fake store " + f.source }

type fakeTarget struct{ store *fakeStore }

func (t fakeTarget) Resolve(_ context.Context, reference string) (ocispec.Descriptor, error) {
	desc, ok := t.store.images[reference]
	if !ok {
		return ocispec.Descriptor{}, errors.New("not held")
	}
	return desc, nil
}

func (t fakeTarget) Fetch(_ context.Context, desc ocispec.Descriptor) (io.ReadCloser, error) {
	body, ok := t.store.blobs[desc.Digest.String()]
	if !ok {
		return nil, errors.New("no such blob")
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func (t fakeTarget) Exists(_ context.Context, desc ocispec.Descriptor) (bool, error) {
	_, ok := t.store.blobs[desc.Digest.String()]
	return ok, nil
}

// One store is handed back unchanged, so the ordinary build carries no merging machinery.
func TestNewCompositeStore(t *testing.T) {
	a := &fakeStore{source: "a"}
	require.Nil(t, NewCompositeStore())
	require.Same(t, ContentStore(a), NewCompositeStore(a))
	require.IsType(t, &CompositeStore{}, NewCompositeStore(a, &fakeStore{source: "b"}))
}

// A file entry names one architecture, so asking each bundle for that entry's own request
// merges them: the bundle that holds that architecture answers, whichever it is.
func TestCompositeFileTakesTheFirstBundleHoldingTheEntry(t *testing.T) {
	amd := contentstore.FileRequest{Source: "oci://registry.example.com/vendor/engine:v1", Arch: "amd64"}
	arm := contentstore.FileRequest{Source: "oci://registry.example.com/vendor/engine:v1", Arch: "arm64"}

	a := &fakeStore{source: "amd64.tar.zst", files: map[contentstore.FileRequest]string{amd: "engine.linux-amd64"}}
	b := &fakeStore{source: "arm64.tar.zst", files: map[contentstore.FileRequest]string{arm: "engine.linux-arm64"}}
	store := NewCompositeStore(a, b)

	for _, tc := range []struct {
		req  contentstore.FileRequest
		want string
	}{
		{req: amd, want: "engine.linux-amd64"},
		{req: arm, want: "engine.linux-arm64"},
	} {
		name, ok, err := store.File(tc.req)
		require.NoError(t, err)
		require.True(t, ok, "neither bundle answered for %s", tc.req.Arch)
		require.Equal(t, tc.want, name)
	}

	_, ok, err := store.File(contentstore.FileRequest{Source: amd.Source, Arch: "riscv64"})
	require.NoError(t, err)
	require.False(t, ok, "an architecture neither bundle holds must be a miss, so the build falls back")
}

// A bundle reporting an ambiguous entry has found a defect in the definition. Walking on to
// the next bundle would bury it, so the error stops the walk.
func TestCompositeFileStopsOnAnAmbiguousEntry(t *testing.T) {
	req := contentstore.FileRequest{Source: "oci://registry.example.com/vendor/engine:v1"}
	ambiguous := errors.New("holds amd64, arm64, and the file entry selects no single architecture")

	a := &fakeStore{source: "a", fileErr: ambiguous}
	b := &fakeStore{source: "b", files: map[contentstore.FileRequest]string{req: "engine"}}

	_, _, err := NewCompositeStore(a, b).File(req)
	require.ErrorIs(t, err, ambiguous, "the second bundle answered over an ambiguity the first reported")
}

// Two bundles each holding one architecture of one reference are merged into a single index
// covering both. Without this an image reference would be answered entirely by whichever
// bundle held it first, and the other architecture would fall back to the registry even though
// it was on disk.
func TestCompositeTargetMergesArchitecturesOfOneReference(t *testing.T) {
	const ref = "registry.example.com/library/pause:3.10"
	amdDesc, amdBlobs := fakeManifest(t, "amd64")
	armDesc, armBlobs := fakeManifest(t, "arm64")

	a := &fakeStore{source: "amd64.tar.zst", images: map[string]ocispec.Descriptor{ref: amdDesc}, blobs: amdBlobs}
	b := &fakeStore{source: "arm64.tar.zst", images: map[string]ocispec.Descriptor{ref: armDesc}, blobs: armBlobs}

	target := NewCompositeStore(a, b).Target()
	desc, err := target.Resolve(context.Background(), ref)
	require.NoError(t, err)
	require.Equal(t, ocispec.MediaTypeImageIndex, desc.MediaType, "two bundles holding one reference must merge into an index")

	rc, err := target.Fetch(context.Background(), desc)
	require.NoError(t, err)
	defer rc.Close() //nolint:errcheck
	body, err := io.ReadAll(rc)
	require.NoError(t, err)

	var index ocispec.Index
	require.NoError(t, json.Unmarshal(body, &index))
	require.Len(t, index.Manifests, 2)

	got := map[string]bool{}
	for _, child := range index.Manifests {
		require.NotNil(t, child.Platform, "a merged child must name the platform it answers for")
		got[child.Platform.Architecture] = true
	}
	require.True(t, got["amd64"] && got["arm64"], "merged index covers %v, want amd64 and arm64", got)
}

// A reference only one bundle holds is passed through untouched, so nothing is synthesized for
// the ordinary case.
func TestCompositeTargetPassesASingleHolderThrough(t *testing.T) {
	const ref = "registry.example.com/library/pause:3.10"
	amdDesc, amdBlobs := fakeManifest(t, "amd64")

	a := &fakeStore{source: "a", images: map[string]ocispec.Descriptor{ref: amdDesc}, blobs: amdBlobs}
	b := &fakeStore{source: "b"}

	desc, err := NewCompositeStore(a, b).Target().Resolve(context.Background(), ref)
	require.NoError(t, err)
	require.Equal(t, amdDesc.Digest, desc.Digest, "a reference one bundle holds must not be rewritten")
}

// Nothing holding the reference is an error naming every bundle that was looked in.
func TestCompositeTargetReportsAReferenceNoBundleHolds(t *testing.T) {
	a := &fakeStore{source: "amd64.tar.zst"}
	b := &fakeStore{source: "arm64.tar.zst"}

	_, err := NewCompositeStore(a, b).Target().Resolve(context.Background(), "registry.example.com/library/pause:3.10")
	require.Error(t, err)
	require.Contains(t, err.Error(), "amd64.tar.zst")
	require.Contains(t, err.Error(), "arm64.tar.zst")
}

// fakeManifest builds a manifest whose config names arch, and the blobs serving it.
func fakeManifest(t *testing.T, arch string) (ocispec.Descriptor, map[string][]byte) {
	t.Helper()

	configBytes, err := json.Marshal(ocispec.Image{
		Platform: ocispec.Platform{OS: "linux", Architecture: arch},
	})
	require.NoError(t, err)
	configDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageConfig,
		Digest:    digest.FromBytes(configBytes),
		Size:      int64(len(configBytes)),
	}
	manifestBytes, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
	})
	require.NoError(t, err)
	manifestDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    digest.FromBytes(manifestBytes),
		Size:      int64(len(manifestBytes)),
	}
	return manifestDesc, map[string][]byte{
		configDesc.Digest.String():   configBytes,
		manifestDesc.Digest.String(): manifestBytes,
	}
}
