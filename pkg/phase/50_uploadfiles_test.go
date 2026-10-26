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
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/api"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/containerd/platforms"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/k0sproject/dig"
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
		Distro: &distro.Distro{
			Spec: distro.Spec{
				Config: distro.Config{
					ImagesConfig: distro.ImageConfig{
						Images: []string{ref},
						Path:   "/var/lib/rancher/k3s/agent/images",
					},
				},
			},
		},
	})
	p.imgFiles = map[api.Arch][]v1alpha1.File{}

	lp, err := layout.FromPath(imagesPath)
	if err != nil {
		t.Fatalf("failed to open the image layout: %v", err)
	}
	for _, arch := range []api.Arch{api.ArchAMD64, api.ArchARM64} {
		if err := p.exportImagesForArch(ctx, store, lp, arch); err != nil {
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

		assertTarballNamesImage(t, files[0].LocalSource.Path, ref)

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
		Distro: &distro.Distro{
			Spec: distro.Spec{
				Config: distro.Config{
					ImagesConfig: distro.ImageConfig{Images: []string{ref}},
				},
			},
		},
	})
	p.imgFiles = map[api.Arch][]v1alpha1.File{}

	lp, err := layout.FromPath(imagesPath)
	if err != nil {
		t.Fatalf("failed to open the image layout: %v", err)
	}

	err = p.exportImagesForArch(ctx, store, lp, api.ArchARM64)
	if err == nil {
		t.Fatal("exportImagesForArch() returned no error for an image the index has no arm64 manifest for")
	}
	if !strings.Contains(err.Error(), string(api.ArchARM64)) {
		t.Errorf("error %q does not name the architecture that could not be exported", err)
	}
}

// newFileUploadPhase builds an UploadFiles over the given package files. TempDirectory is a scratch
// directory rather than a real package layout: distroFilesFor only joins paths against it.
func newFileUploadPhase(t *testing.T, files ...*v1alpha1.File) *UploadFiles {
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

// TestDistroFilesForSelectsByProfileAndWhen covers the two gates on spec.config.files. These are
// the package's own files rather than an install method's, so nothing runs after this phase to
// rename a staged file into place -- an executable here goes straight to its target.
func TestDistroFilesForSelectsByProfileAndWhen(t *testing.T) {
	etcdctl := v1alpha1.File{
		Target:     "/usr/local/bin/etcdctl",
		Executable: true,
	}
	etcdctl.Selector.Profile = cluster.RoleController
	etcdctl.Selector.When = ".tools.etcdctl.enabled"

	always := v1alpha1.File{Target: "/usr/local/bin/always"}

	for _, tt := range []struct {
		name    string
		host    *cluster.Host
		enabled bool
		want    []string
	}{
		{
			name:    "a controller with the tool enabled",
			host:    &cluster.Host{Role: cluster.RoleController},
			enabled: true,
			want:    []string{"etcdctl", "always"},
		},
		{
			name:    "a controller with the tool disabled",
			host:    &cluster.Host{Role: cluster.RoleController},
			enabled: false,
			want:    []string{"always"},
		},
		{
			name:    "a worker never takes a controller file, enabled or not",
			host:    &cluster.Host{Role: cluster.RoleWorker},
			enabled: true,
			want:    []string{"always"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := &UploadFiles{}
			p.SetManager(&Manager{
				TempDirectory: t.TempDir(),
				Values: map[string]any{
					"tools": map[string]any{
						"etcdctl": map[string]any{
							"enabled": tt.enabled,
						},
					},
				},
			})
			p.disFiles = v1alpha1.Files{&etcdctl, &always}

			got, err := p.distroFilesFor(context.Background(), tt.host, api.ArchAMD64)
			require.NoError(t, err)

			names := make([]string, 0, len(got))
			for _, f := range got {
				names = append(names, f.Name)
				require.Equal(t, f.OriginalTarget, f.Target,
					"nothing renames a file from this list, so it is not uploaded under a staging name")
				require.Equal(t, "file", f.Category,
					"the category is what puts this in the host manifest, which is how a reset finds it again")
			}
			require.Equal(t, tt.want, names)
		})
	}
}

// TestExportImagesForArchWritesASingleArchImage covers the other half of layoutImage: an image
// pulled for one architecture is tagged as a manifest rather than an index, so it is read straight
// out of index.json instead of through a parent.
func TestExportImagesForArchWritesASingleArchImage(t *testing.T) {
	ctx := context.Background()
	const ref = "ghcr.io/colonel-byte/pause:3.10"

	tempDir := t.TempDir()
	imagesPath := filepath.Join(tempDir, config.ImagesDir)
	store, err := oci.NewWithContext(ctx, imagesPath)
	if err != nil {
		t.Fatalf("failed to create the store: %v", err)
	}
	manifestDesc := pushImageManifest(ctx, t, store, "amd64")
	if err := store.Tag(ctx, manifestDesc, ref); err != nil {
		t.Fatalf("failed to tag the manifest: %v", err)
	}

	p := &UploadFiles{}
	p.SetManager(&Manager{
		TempDirectory: tempDir,
		Distro: &distro.Distro{
			Spec: distro.Spec{
				Config: distro.Config{
					ImagesConfig: distro.ImageConfig{
						Images: []string{ref},
						Path:   "/var/lib/rancher/k3s/agent/images",
					},
				},
			},
		},
	})
	p.imgFiles = map[api.Arch][]v1alpha1.File{}

	lp, err := layout.FromPath(imagesPath)
	if err != nil {
		t.Fatalf("failed to open the image layout: %v", err)
	}
	if err := p.exportImagesForArch(ctx, store, lp, api.ArchAMD64); err != nil {
		t.Fatalf("exportImagesForArch() unexpected error: %v", err)
	}

	files := p.imgFiles[api.ArchAMD64]
	if len(files) != 1 {
		t.Fatalf("imgFiles[amd64] has %d entries, want 1", len(files))
	}
	assertTarballNamesImage(t, files[0].LocalSource.Path, ref)
}

// assertTarballNamesImage reads the docker-save manifest out of a tarball and checks it names the
// image by the reference the distro config asked for. A tarball whose RepoTags do not match is
// imported under a name no pod spec refers to, which an "image exists on the host" check misses.
func assertTarballNamesImage(t *testing.T, path, ref string) {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("failed to open the tarball: %v", err)
	}
	defer func() { require.NoError(t, f.Close()) }()

	tr := tar.NewReader(f)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("failed to read the tarball: %v", err)
		}
		if h.Name != "manifest.json" {
			continue
		}

		var m []struct {
			Config   string   `json:"Config"`
			RepoTags []string `json:"RepoTags"`
			Layers   []string `json:"Layers"`
		}
		if err := json.NewDecoder(tr).Decode(&m); err != nil {
			t.Fatalf("failed to decode manifest.json: %v", err)
		}
		if len(m) != 1 {
			t.Fatalf("manifest.json holds %d entries, want 1", len(m))
		}
		if got := m[0].RepoTags; len(got) != 1 || got[0] != ref {
			t.Errorf("RepoTags = %v, want [%s]", got, ref)
		}
		if m[0].Config == "" {
			t.Error("manifest.json names no config blob")
		}
		return
	}
	t.Error("the tarball holds no manifest.json, so nothing will import it")
}

// A chart manifest the cluster disabled keeps its images in the package -- publishing needs them
// there -- and has none of them exported or uploaded. The disabled image is pushed to the store
// here on purpose: if the filter were missing, the export would succeed rather than error, and
// the only thing that catches it is counting what came out.
func TestExportImagesForArchSkipsADisabledChartsImages(t *testing.T) {
	ctx := context.Background()
	const (
		kept    = "ghcr.io/colonel-byte/pause:3.10"
		dropped = "ghcr.io/colonel-byte/hubble-ui:0.13.6"
	)

	tempDir := t.TempDir()
	imagesPath := filepath.Join(tempDir, config.ImagesDir)
	store, err := oci.NewWithContext(ctx, imagesPath)
	if err != nil {
		t.Fatalf("failed to create the store: %v", err)
	}
	// One index tagged under both references: the test counts what is exported, so what
	// the two refs resolve to does not matter, and pushing two identical manifests would
	// collide on their shared config blob.
	desc := pushImageIndex(ctx, t, store, []ocispec.Descriptor{
		pushImageManifest(ctx, t, store, "amd64"),
	})
	for _, ref := range []string{kept, dropped} {
		if err := store.Tag(ctx, desc, ref); err != nil {
			t.Fatalf("failed to tag %s: %v", ref, err)
		}
	}

	p := &UploadFiles{}
	p.SetManager(&Manager{
		TempDirectory: tempDir,
		Distro: &distro.Distro{
			Spec: distro.Spec{
				Config: distro.Config{
					ImagesConfig: distro.ImageConfig{
						Images: []string{kept},
						Path:   "/var/lib/rancher/rke2/agent/images",
					},
					Engine: dig.Mapping{
						config.EngineManifest: dig.Mapping{
							"rke2-cilium": dig.Mapping{
								"enabled": false,
								"images":  []any{dropped},
								"values":  "hubble:\n  enabled: false\n",
							},
						},
					},
				},
			},
		},
	})
	p.imgFiles = map[api.Arch][]v1alpha1.File{}

	lp, err := layout.FromPath(imagesPath)
	if err != nil {
		t.Fatalf("failed to open the image layout: %v", err)
	}
	if err := p.exportImagesForArch(ctx, store, lp, api.ArchAMD64); err != nil {
		t.Fatalf("exportImagesForArch unexpected error: %v", err)
	}

	files := p.imgFiles[api.ArchAMD64]
	if len(files) != 1 {
		t.Fatalf("imgFiles has %d entries, want 1", len(files))
	}
	assertTarballNamesImage(t, files[0].LocalSource.Path, kept)

	if _, err := os.Stat(filepath.Join(tempDir, config.TarBallDir, string(api.ArchAMD64), "ghcr.io_colonel-byte_hubble-ui.tar")); !os.IsNotExist(err) {
		t.Error("a disabled chart's image was exported to a tarball")
	}
}

// Enabling the same chart is what puts its images back, so the filter is the enablement and not
// the presence of a chart manifest.
func TestExportImagesForArchKeepsAnEnabledChartsImages(t *testing.T) {
	ctx := context.Background()
	const ref = "ghcr.io/colonel-byte/hubble-ui:0.13.6"

	tempDir := t.TempDir()
	imagesPath := filepath.Join(tempDir, config.ImagesDir)
	store, err := oci.NewWithContext(ctx, imagesPath)
	if err != nil {
		t.Fatalf("failed to create the store: %v", err)
	}
	desc := pushImageIndex(ctx, t, store, []ocispec.Descriptor{
		pushImageManifest(ctx, t, store, "amd64"),
	})
	if err := store.Tag(ctx, desc, ref); err != nil {
		t.Fatalf("failed to tag %s: %v", ref, err)
	}

	p := &UploadFiles{}
	p.SetManager(&Manager{
		TempDirectory: tempDir,
		Distro: &distro.Distro{
			Spec: distro.Spec{
				Config: distro.Config{
					ImagesConfig: distro.ImageConfig{Path: "/var/lib/rancher/rke2/agent/images"},
					Engine: dig.Mapping{
						config.EngineManifest: dig.Mapping{
							"rke2-cilium": dig.Mapping{
								"images": []any{ref},
								"values": "hubble:\n  enabled: true\n",
							},
						},
					},
				},
			},
		},
	})
	p.imgFiles = map[api.Arch][]v1alpha1.File{}

	lp, err := layout.FromPath(imagesPath)
	if err != nil {
		t.Fatalf("failed to open the image layout: %v", err)
	}
	if err := p.exportImagesForArch(ctx, store, lp, api.ArchAMD64); err != nil {
		t.Fatalf("exportImagesForArch unexpected error: %v", err)
	}

	files := p.imgFiles[api.ArchAMD64]
	if len(files) != 1 {
		t.Fatalf("imgFiles has %d entries, want 1", len(files))
	}
	assertTarballNamesImage(t, files[0].LocalSource.Path, ref)
}
