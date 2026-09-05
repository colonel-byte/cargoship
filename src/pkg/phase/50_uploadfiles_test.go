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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/src/api"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	carch "github.com/colonel-byte/cargoship/src/pkg/oci/archive"
	"github.com/containerd/platforms"
	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
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

// TestImageExportPlatformFollowsTheHost is the regression test for exporting against the
// architecture of the machine running cargoship. Every architecture is asserted, so whichever one
// the test happens to run on, the others prove the matcher is not reading runtime.GOARCH.
func TestImageExportPlatformFollowsTheHost(t *testing.T) {
	for _, arch := range []api.Arch{api.ArchAMD64, api.ArchARM64, api.ArchRISCV} {
		t.Run(string(arch), func(t *testing.T) {
			matcher := imageExportPlatform(arch)

			if !matcher.Match(ocispec.Platform{OS: "linux", Architecture: string(arch)}) {
				t.Errorf("matcher does not match linux/%s, which is the architecture it was built for", arch)
			}

			other := api.ArchAMD64
			if arch == api.ArchAMD64 {
				other = api.ArchARM64
			}
			if matcher.Match(ocispec.Platform{OS: "linux", Architecture: string(other)}) {
				t.Errorf("matcher matches linux/%s, which is not the architecture it was built for", other)
			}
		})
	}
}

// TestImageExportPlatformIsAlwaysLinux pins the OS to the host being uploaded to rather than the
// one cargoship runs on, so building from macOS does not select against darwin.
func TestImageExportPlatformIsAlwaysLinux(t *testing.T) {
	if imageExportPlatform(api.ArchAMD64).Match(ocispec.Platform{OS: "darwin", Architecture: "amd64"}) {
		t.Error("matcher matches darwin/amd64, but image tarballs are only ever imported on Linux")
	}
}

// TestExportImagesForArchWritesPerArchTarballs covers the export loop over a real image index. Two
// architectures of one image share a tarball name, so each has to land in its own directory, and
// each tarball has to carry its own architecture's manifest rather than whichever was exported
// first.
func TestExportImagesForArchWritesPerArchTarballs(t *testing.T) {
	ctx := context.Background()
	const ref = "ghcr.io/colonel-byte/pause:3.10"

	tempDir := t.TempDir()
	imagesPath := filepath.Join(tempDir, config.ImagesDir)
	store, err := oci.NewWithContext(ctx, imagesPath)
	if err != nil {
		t.Fatalf("failed to create the store: %v", err)
	}
	indexDesc := pushImageIndex(ctx, t, store, []ocispec.Descriptor{
		pushImageManifest(ctx, t, store, "amd64"),
		pushImageManifest(ctx, t, store, "arm64"),
	})
	if err := store.Tag(ctx, indexDesc, ref); err != nil {
		t.Fatalf("failed to tag the index: %v", err)
	}

	p := &UploadFiles{}
	p.SetManager(&Manager{
		TempDirectory: tempDir,
		Distro: &distro.ZarfDistro{
			Spec: distro.ZarfDistroSpec{
				Config: distro.ZarfDistroConfig{
					ImagesConfig: distro.ZarfDistroImageConfig{
						Images: []string{ref},
						Path:   "/var/lib/rancher/k3s/agent/images",
					},
				},
			},
		},
	})
	p.imgFiles = map[api.Arch][]v1alpha1.ZarfFile{}

	archiveStore := &carch.OciArchiveStore{Root: imagesPath, Src: store}
	for _, arch := range []api.Arch{api.ArchAMD64, api.ArchARM64} {
		if err := p.exportImagesForArch(ctx, store, archiveStore, arch); err != nil {
			t.Fatalf("exportImagesForArch(%s) unexpected error: %v", arch, err)
		}
	}

	contents := map[api.Arch][]byte{}
	for _, arch := range []api.Arch{api.ArchAMD64, api.ArchARM64} {
		files := p.imgFiles[arch]
		if len(files) != 1 {
			t.Fatalf("imgFiles[%s] has %d entries, want 1", arch, len(files))
		}

		want := filepath.Join(tempDir, config.TarBallDir, string(arch), "ghcr.io_colonel-byte_pause.tar")
		if files[0].LocalSource.Path != want {
			t.Errorf("tarball path = %q, want %q", files[0].LocalSource.Path, want)
		}

		b, err := os.ReadFile(files[0].LocalSource.Path)
		if err != nil {
			t.Fatalf("failed to read the %s tarball: %v", arch, err)
		}
		contents[arch] = b
	}

	if bytes.Equal(contents[api.ArchAMD64], contents[api.ArchARM64]) {
		t.Error("the two tarballs are identical, so the export did not follow the architecture it was given")
	}
}

// TestExportImagesForArchWithoutAMatchingManifest fails the run rather than writing a tarball the
// nodes of that architecture cannot import.
func TestExportImagesForArchWithoutAMatchingManifest(t *testing.T) {
	ctx := context.Background()
	const ref = "ghcr.io/colonel-byte/pause:3.10"

	tempDir := t.TempDir()
	imagesPath := filepath.Join(tempDir, config.ImagesDir)
	store, err := oci.NewWithContext(ctx, imagesPath)
	if err != nil {
		t.Fatalf("failed to create the store: %v", err)
	}
	indexDesc := pushImageIndex(ctx, t, store, []ocispec.Descriptor{pushImageManifest(ctx, t, store, "amd64")})
	if err := store.Tag(ctx, indexDesc, ref); err != nil {
		t.Fatalf("failed to tag the index: %v", err)
	}

	p := &UploadFiles{}
	p.SetManager(&Manager{
		TempDirectory: tempDir,
		Distro: &distro.ZarfDistro{
			Spec: distro.ZarfDistroSpec{
				Config: distro.ZarfDistroConfig{
					ImagesConfig: distro.ZarfDistroImageConfig{Images: []string{ref}},
				},
			},
		},
	})
	p.imgFiles = map[api.Arch][]v1alpha1.ZarfFile{}

	err = p.exportImagesForArch(ctx, store, &carch.OciArchiveStore{Root: imagesPath, Src: store}, api.ArchARM64)
	if err == nil {
		t.Fatal("exportImagesForArch() returned no error for an image the index has no arm64 manifest for")
	}
	if !strings.Contains(err.Error(), string(api.ArchARM64)) {
		t.Errorf("error %q does not name the architecture that could not be exported", err)
	}
}
