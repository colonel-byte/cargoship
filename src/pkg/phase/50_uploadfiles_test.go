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

package phase

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/src/api"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
	"github.com/containerd/platforms"
	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content/oci"
)

func pushImageBlob(ctx context.Context, t *testing.T, store *oci.Store, mediaType string, b []byte) ocispec.Descriptor {
	t.Helper()

	desc := ocispec.Descriptor{
		MediaType: mediaType,
		Digest:    digest.FromBytes(b),
		Size:      int64(len(b)),
	}
	if err := store.Push(ctx, desc, bytes.NewReader(b)); err != nil {
		t.Fatalf("failed to push %s: %v", mediaType, err)
	}
	return desc
}

// pushImageManifest writes a layerless manifest into the store and returns a descriptor carrying the
// platform, the way a descriptor read out of a registry index does.
func pushImageManifest(ctx context.Context, t *testing.T, store *oci.Store, arch string) ocispec.Descriptor {
	t.Helper()

	configBytes, err := json.Marshal(ocispec.Image{
		Platform: ocispec.Platform{OS: "linux", Architecture: arch},
	})
	if err != nil {
		t.Fatalf("failed to marshal the image config: %v", err)
	}
	configDesc := pushImageBlob(ctx, t, store, ocispec.MediaTypeImageConfig, configBytes)

	manifestBytes, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ocispec.Descriptor{},
	})
	if err != nil {
		t.Fatalf("failed to marshal the image manifest: %v", err)
	}
	desc := pushImageBlob(ctx, t, store, ocispec.MediaTypeImageManifest, manifestBytes)
	desc.Platform = &ocispec.Platform{OS: "linux", Architecture: arch}
	return desc
}

func pushImageIndex(ctx context.Context, t *testing.T, store *oci.Store, manifests []ocispec.Descriptor) ocispec.Descriptor {
	t.Helper()

	indexBytes, err := json.Marshal(ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: manifests,
	})
	if err != nil {
		t.Fatalf("failed to marshal the image index: %v", err)
	}
	return pushImageBlob(ctx, t, store, ocispec.MediaTypeImageIndex, indexBytes)
}

func newImageStore(ctx context.Context, t *testing.T) *oci.Store {
	t.Helper()

	store, err := oci.NewWithContext(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("failed to create the store: %v", err)
	}
	return store
}

func TestResolveImageManifestReturnsAManifestUnchanged(t *testing.T) {
	ctx := context.Background()
	store := newImageStore(ctx, t)
	manifestDesc := pushImageManifest(ctx, t, store, "amd64")

	got, err := resolveImageManifest(ctx, store, manifestDesc, platforms.Only(ocispec.Platform{OS: "linux", Architecture: "arm64"}))
	if err != nil {
		t.Fatalf("resolveImageManifest() unexpected error: %v", err)
	}
	if got.Digest != manifestDesc.Digest {
		t.Errorf("digest = %q, want %q", got.Digest, manifestDesc.Digest)
	}
}

func TestResolveImageManifestSelectsThePlatform(t *testing.T) {
	ctx := context.Background()
	store := newImageStore(ctx, t)
	amd64Desc := pushImageManifest(ctx, t, store, "amd64")
	arm64Desc := pushImageManifest(ctx, t, store, "arm64")
	indexDesc := pushImageIndex(ctx, t, store, []ocispec.Descriptor{amd64Desc, arm64Desc})

	tests := []struct {
		name string
		arch string
		want digest.Digest
	}{
		{
			name: "amd64",
			arch: "amd64",
			want: amd64Desc.Digest,
		},
		{
			name: "arm64",
			arch: "arm64",
			want: arm64Desc.Digest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveImageManifest(ctx, store, indexDesc, platforms.Only(ocispec.Platform{OS: "linux", Architecture: tt.arch}))
			if err != nil {
				t.Fatalf("resolveImageManifest() unexpected error: %v", err)
			}
			if got.Digest != tt.want {
				t.Errorf("digest = %q, want %q", got.Digest, tt.want)
			}
			if got.MediaType != ocispec.MediaTypeImageManifest {
				t.Errorf("media type = %q, want %q", got.MediaType, ocispec.MediaTypeImageManifest)
			}
		})
	}
}

func TestResolveImageManifestWithoutAMatchErrors(t *testing.T) {
	ctx := context.Background()
	store := newImageStore(ctx, t)
	amd64Desc := pushImageManifest(ctx, t, store, "amd64")
	indexDesc := pushImageIndex(ctx, t, store, []ocispec.Descriptor{amd64Desc})

	_, err := resolveImageManifest(ctx, store, indexDesc, platforms.Only(ocispec.Platform{OS: "linux", Architecture: "riscv64"}))
	if err == nil {
		t.Fatal("resolveImageManifest() returned no error, want one")
	}
	if !strings.Contains(err.Error(), "linux/amd64") {
		t.Errorf("error %q does not name the platforms the index holds", err)
	}
}

func TestResolveImageManifestSkipsChildrenWithoutAPlatform(t *testing.T) {
	ctx := context.Background()
	store := newImageStore(ctx, t)
	// An attestation or a signature is listed in an index without a platform.
	unknownDesc := pushImageManifest(ctx, t, store, "amd64")
	unknownDesc.Platform = nil
	arm64Desc := pushImageManifest(ctx, t, store, "arm64")
	indexDesc := pushImageIndex(ctx, t, store, []ocispec.Descriptor{unknownDesc, arm64Desc})

	got, err := resolveImageManifest(ctx, store, indexDesc, platforms.Only(ocispec.Platform{OS: "linux", Architecture: "arm64"}))
	if err != nil {
		t.Fatalf("resolveImageManifest() unexpected error: %v", err)
	}
	if got.Digest != arm64Desc.Digest {
		t.Errorf("digest = %q, want the arm64 manifest %q", got.Digest, arm64Desc.Digest)
	}
}

// TestPackageExportPlatformFollowsThePackage is the regression test for exporting against the
// architecture of the machine running cargoship. Both architectures are asserted, so whichever one
// the test happens to run on, the other proves the matcher is not reading runtime.GOARCH.
func TestPackageExportPlatformFollowsThePackage(t *testing.T) {
	for _, arch := range []api.Arch{api.ArchAMD64, api.ArchARM64, api.ArchRISCV} {
		t.Run(string(arch), func(t *testing.T) {
			matcher := packageExportPlatform(api.Arches{arch})

			if !matcher.Match(ocispec.Platform{OS: "linux", Architecture: string(arch)}) {
				t.Errorf("matcher does not match linux/%s, which is what the package targets", arch)
			}

			other := api.ArchAMD64
			if arch == api.ArchAMD64 {
				other = api.ArchARM64
			}
			if matcher.Match(ocispec.Platform{OS: "linux", Architecture: string(other)}) {
				t.Errorf("matcher matches linux/%s, which the package does not target", other)
			}
		})
	}
}

// TestPackageExportPlatformIsAlwaysLinux pins the OS to the host being uploaded to rather than the
// one cargoship runs on, so building from macOS does not select against darwin.
func TestPackageExportPlatformIsAlwaysLinux(t *testing.T) {
	matcher := packageExportPlatform(api.Arches{api.ArchAMD64})

	if matcher.Match(ocispec.Platform{OS: "darwin", Architecture: "amd64"}) {
		t.Error("matcher matches darwin/amd64, but image tarballs are only ever imported on Linux")
	}
}

// TestPackageExportPlatformWithoutASingleArch covers the cases packageExportPlatform cannot answer
// on its own: a package carrying several architectures needs a matcher per host, and one carrying
// none has nothing to pin to. Both keep the previous behaviour until TODO(#258) lands.
func TestPackageExportPlatformWithoutASingleArch(t *testing.T) {
	local := ocispec.Platform{OS: "linux", Architecture: "amd64"}

	for _, tt := range []struct {
		name   string
		arches api.Arches
	}{
		{name: "several architectures", arches: api.Arches{api.ArchAMD64, api.ArchARM64}},
		{name: "no architecture", arches: nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := packageExportPlatform(tt.arches).Match(local)
			want := platforms.DefaultStrict().Match(local)
			if got != want {
				t.Errorf("Match(linux/amd64) = %v, want %v, matching the DefaultStrict fallback", got, want)
			}
		})
	}
}

// newFileUploadPhase builds an UploadFiles over the given package files. TempDirectory is a scratch
// directory rather than a real package layout: distroFilesFor only joins paths against it.
func newFileUploadPhase(t *testing.T, files ...*v1alpha1.ZarfFile) *UploadFiles {
	t.Helper()

	p := &UploadFiles{}
	p.SetManager(&Manager{TempDirectory: t.TempDir()})
	p.disFiles = files

	return p
}

func TestDistroFilesForSelectsByArch(t *testing.T) {
	p := newFileUploadPhase(t,
		newDistroFile("/usr/bin/k3s-amd64", api.ArchAMD64),
		newDistroFile("/usr/bin/k3s-arm64", api.ArchARM64),
		newDistroFile("/etc/k3s/config.yaml", ""),
	)

	got, err := p.distroFilesFor(context.Background(), newDetectedHost(t, "arm64"), api.ArchARM64)

	require.NoError(t, err)
	require.Equal(t, []string{"k3s-arm64", "config.yaml"}, fileNames(got),
		"an arm64 host takes its own binary and the file that selects no architecture")
}

// TestDistroFilesForKeepsTheAssembledIndex is the regression test for the invariant this loop
// shares with profileFilesForArch: a file's position in the package's file list is the name of the
// directory it was assembled into, so skipping one must not renumber the files after it.
func TestDistroFilesForKeepsTheAssembledIndex(t *testing.T) {
	p := newFileUploadPhase(t,
		newDistroFile("/usr/bin/first", api.ArchAMD64),
		newDistroFile("/usr/bin/second", api.ArchAMD64),
		newDistroFile("/usr/bin/third", api.ArchARM64),
	)

	got, err := p.distroFilesFor(context.Background(), newDetectedHost(t, "arm64"), api.ArchARM64)

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "2", filepath.Base(filepath.Dir(got[0].LocalSource.Path)),
		"third is the third file in the package, so it was assembled into directory 2 regardless of what was skipped")
}
