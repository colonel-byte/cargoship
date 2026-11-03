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

package noncluster

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/test"
	ggcrname "github.com/google/go-containerregistry/pkg/name"
	ggcr "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	ggcrremote "github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	ggcrtypes "github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	orasoci "oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"
)

// The images the minimal-image fixture references. The bytes are built in process rather
// than pulled, so the tags name what they stand for and not a real release. pause is
// declared in imageConfig.images and cilium under a chart entry in engine.manifest, which
// is what makes the round-trip cover both ways a package can name an image.
const (
	pauseImageTag  = "pause:3.10"
	ciliumImageTag = "cilium:1.20.2"
)

// The coordinates publish tags the minimal-image package under, from its metadata.
const (
	imagePackageName    = "e2e-minimal-image"
	imagePackageVersion = "0.0.1"
)

// TestCargoshipImportRoundTrip exercises the built binary's "import" command over an OCI
// Image Layout, the format `skopeo copy ... oci-archive:` writes and the only input import
// accepts. See docs/guides/importing-archives.md.
//
// The layout is produced by copying a published package out of the in-memory registry, so
// the archive carries the annotations and media types `cargoship publish` really writes -
// which is the one thing pkg/distro's unit tests cannot cover, because they hand-build the
// layout they then import. The copy stands in for Skopeo: it is the same registry-to-layout
// transfer, run in process so the suite needs no external binary and no network.
func TestCargoshipImportRoundTrip(t *testing.T) {
	addr := test.SetupInMemoryRegistry(t)
	pushTestImage(t, addr, pauseImageTag)
	pushTestImage(t, addr, ciliumImageTag)
	pkgPath := imagePackage(t, addr)
	dst := fmt.Sprintf("oci://%s/%s", addr, "e2e-import")

	_, _, err := e2e.Cargoship(t, "publish", pkgPath, dst, "--plain-http", "--confirm")
	require.NoError(t, err)

	layoutDir := ociLayoutFromRegistry(t, addr, "e2e-import/"+imagePackageName, imagePackageVersion)
	requireImageLayer(t, layoutDir)

	for _, tc := range []struct {
		name   string
		source string
	}{
		{
			name:   "oci-archive tar",
			source: tarDir(t, layoutDir),
		},
		{
			name:   "unpacked layout directory",
			source: layoutDir,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outDir := t.TempDir()
			_, _, err := e2e.Cargoship(t, "import", tc.source, "-o", outDir)
			require.NoError(t, err)

			wantBytes, err := os.ReadFile(pkgPath)
			require.NoError(t, err)
			gotBytes, err := os.ReadFile(requireSinglePackage(t, outDir))
			require.NoError(t, err)
			require.Equal(t, wantBytes, gotBytes, "imported package must be byte-identical to the published one")
		})
	}
}

// TestCargoshipImportMultiArch covers importing a package that covers two architectures.
//
// It is the case the unit tests cannot reach: a cargoship multi-arch package holds one set
// of blobs, so the registry tags an index listing that one manifest digest once per
// architecture, and copying the tagged index into a layout nests it under the layout's own
// index.json. Selecting the package therefore takes both halves of selectManifest - the
// descent into a nested index, and the platform match inside it - over an index the
// publisher really wrote rather than one the test hand-built.
func TestCargoshipImportMultiArch(t *testing.T) {
	pkgPath := multiArchPackage(t)
	addr := test.SetupInMemoryRegistry(t)
	dst := fmt.Sprintf("oci://%s/e2e-import-multi", addr)

	_, _, err := e2e.Cargoship(t, "publish", pkgPath, dst, "--plain-http", "--confirm")
	require.NoError(t, err)

	repoPath := "e2e-import-multi/" + multiArchPackageName
	index := fetchIndex(t, addr, repoPath, imagePackageVersion)
	require.Equal(t, []string{"amd64", "arm64"}, indexArchitectures(index),
		"the import below is only a multi-arch case while the published index covers both")

	layoutDir := ociLayoutFromRegistry(t, addr, repoPath, imagePackageVersion)
	requireNestedIndex(t, layoutDir)
	tarPath := tarDir(t, layoutDir)

	wantBytes, err := os.ReadFile(pkgPath)
	require.NoError(t, err)

	// Both architectures resolve to the one manifest the package pushed, so either import
	// returns the same bytes. What differs is the path taken to find it.
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run("imports as "+arch, func(t *testing.T) {
			outDir := t.TempDir()
			_, _, err := e2e.Cargoship(t, "import", tarPath, "-o", outDir, "-a", arch)
			require.NoError(t, err)

			gotBytes, err := os.ReadFile(requireSinglePackage(t, outDir))
			require.NoError(t, err)
			require.Equal(t, wantBytes, gotBytes, "imported package must be byte-identical to the published one")
		})
	}
}

// requireNestedIndex fails unless the layout's own index.json points at an image index
// rather than straight at a manifest. That nesting is what makes this test cover the
// descent in selectManifest, and it is a property of how the index was copied rather than
// anything the test states, so it is checked instead of assumed.
func requireNestedIndex(t *testing.T, layoutDir string) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(layoutDir, ocispec.ImageIndexFile))
	require.NoError(t, err)

	var index ocispec.Index
	require.NoError(t, json.Unmarshal(data, &index))

	// The store lists every manifest it holds, so the copied index sits beside the manifest
	// it points at. One image index among them is what sends the import down the descent.
	var nested int
	for _, m := range index.Manifests {
		if m.MediaType == ocispec.MediaTypeImageIndex {
			nested++
		}
	}
	require.NotZerof(t, nested, "%s names no image index, so importing it never descends into one", ocispec.ImageIndexFile)
}

// requireImageLayer fails unless the archive carries a layer from the bundled image. The
// import assembles image layers on a different branch from the metadata ones, so a fixture
// that quietly stopped bundling an image would leave that branch untested while every
// assertion below still passed.
func requireImageLayer(t *testing.T, layoutDir string) {
	t.Helper()

	var titles []string
	require.NoError(t, filepath.WalkDir(filepath.Join(layoutDir, "blobs"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var manifest struct {
			Layers []struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"layers"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil // not a manifest, just a blob
		}
		for _, l := range manifest.Layers {
			if title := l.Annotations[ocispec.AnnotationTitle]; title != "" {
				titles = append(titles, title)
			}
		}
		return nil
	}))

	require.NotEmpty(t, titles, "%s holds no manifest naming any layer", layoutDir)
	var images []string
	for _, title := range titles {
		if strings.HasPrefix(title, config.ImagesDir+"/") {
			images = append(images, title)
		}
	}
	require.NotEmptyf(t, images, "no %s/ layer among %v: the fixture is not bundling an image", config.ImagesDir, titles)
}

// pushTestImage builds a single-layer image in process and pushes it to the in-memory
// registry, so a package can bundle an image without the suite pulling one over the
// network. The content is a few bytes: what matters is that it is a real manifest, config
// and layer, which is what cargoship walks when it assembles an images layer.
func pushTestImage(t *testing.T, addr string, tag string) {
	t.Helper()

	// The payload names the tag so two images do not share a layer digest, which would
	// make a test that counts blobs pass for the wrong reason.
	layer := static.NewLayer([]byte("cargoship e2e "+tag), ggcrtypes.DockerLayer)
	img, err := mutate.AppendLayers(empty.Image, layer)
	require.NoError(t, err)

	cfg, err := img.ConfigFile()
	require.NoError(t, err)
	cfg.Architecture = "amd64"
	cfg.OS = "linux"
	img, err = mutate.ConfigFile(img, cfg)
	require.NoError(t, err)

	ref, err := ggcrname.ParseReference(fmt.Sprintf("%s/%s", addr, tag))
	require.NoError(t, err)
	require.NoError(t, ggcrremote.Write(ref, img))

	// Fail here rather than inside "cargoship create" if the push produced nothing usable.
	var fetched ggcr.Image
	fetched, err = ggcrremote.Image(ref)
	require.NoError(t, err)
	_, err = fetched.Manifest()
	require.NoError(t, err)
}

// imagePackage creates a package from the minimal-image fixture, pointed at the in-memory
// registry. Unlike minimalPackage it is not shared between tests: the fixture names a
// registry address that is allocated per test.
func imagePackage(t *testing.T, addr string) string {
	t.Helper()

	defDir := copyDistroDefinition(t, imageDistroDir)
	defPath := filepath.Join(defDir, "distro.yaml")
	data, err := os.ReadFile(defPath)
	require.NoError(t, err)
	rewritten := strings.ReplaceAll(string(data), "__REGISTRY__", addr)
	require.NotEqual(t, string(data), rewritten, "%s holds no __REGISTRY__ placeholder to rewrite", imageDistroDir)
	require.NoError(t, os.WriteFile(defPath, []byte(rewritten), 0o600))

	outDir := t.TempDir()
	_, _, err = e2e.Cargoship(t, "create", defDir, "-o", outDir)
	require.NoError(t, err)

	return requireSinglePackage(t, outDir)
}

// ociLayoutFromRegistry copies a published package out of the registry into an OCI Image
// Layout directory, which is what `skopeo copy docker://... oci-archive:` produces.
func ociLayoutFromRegistry(t *testing.T, addr string, repoPath string, tag string) string {
	t.Helper()
	ctx := context.Background()

	repo, err := remote.NewRepository(fmt.Sprintf("%s/%s:%s", addr, repoPath, tag))
	require.NoError(t, err)
	repo.PlainHTTP = true

	layoutDir := filepath.Join(t.TempDir(), "layout")
	store, err := orasoci.New(layoutDir)
	require.NoError(t, err)

	_, err = oras.Copy(ctx, repo, tag, store, tag, oras.DefaultCopyOptions)
	require.NoError(t, err)

	return layoutDir
}

// tarDir writes dir into a tar file, the shape an oci-archive arrives in.
func tarDir(t *testing.T, dir string) string {
	t.Helper()

	tarPath := filepath.Join(t.TempDir(), "oci-archive.tar")
	f, err := os.Create(tarPath)
	require.NoError(t, err)
	defer f.Close() //nolint:errcheck

	tw := tar.NewWriter(f)
	require.NoError(t, filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if d.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close() //nolint:errcheck
		_, err = io.Copy(tw, src)
		return err
	}))
	require.NoError(t, tw.Close())

	return tarPath
}
