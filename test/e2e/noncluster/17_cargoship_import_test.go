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

// pauseImageTag is the image the minimal-image fixture references. The bytes are built in
// process rather than pulled, so the tag names what it stands for and not a real release.
const pauseImageTag = "pause:3.10"

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
	pushPauseImage(t, addr)
	pkgPath := imagePackage(t, addr)
	dst := fmt.Sprintf("oci://%s/%s", addr, "e2e-import")

	_, _, err := e2e.Cargoship(t, "publish", pkgPath, dst, "--plain-http", "--confirm")
	require.NoError(t, err)

	layoutDir := ociLayoutFromRegistry(t, addr, "e2e-import")
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

			imported, err := filepath.Glob(filepath.Join(outDir, "*.tar.zst"))
			require.NoError(t, err)
			require.Len(t, imported, 1, "expected exactly one package imported")

			wantBytes, err := os.ReadFile(pkgPath)
			require.NoError(t, err)
			gotBytes, err := os.ReadFile(imported[0])
			require.NoError(t, err)
			require.Equal(t, wantBytes, gotBytes, "imported package must be byte-identical to the published one")
		})
	}
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

// pushPauseImage builds a single-layer image in process and pushes it to the in-memory
// registry, so a package can bundle an image without the suite pulling one over the
// network. The content is a few bytes: what matters is that it is a real manifest, config
// and layer, which is what cargoship walks when it assembles an images layer.
func pushPauseImage(t *testing.T, addr string) {
	t.Helper()

	layer := static.NewLayer([]byte("cargoship e2e pause"), ggcrtypes.DockerLayer)
	img, err := mutate.AppendLayers(empty.Image, layer)
	require.NoError(t, err)

	cfg, err := img.ConfigFile()
	require.NoError(t, err)
	cfg.Architecture = "amd64"
	cfg.OS = "linux"
	img, err = mutate.ConfigFile(img, cfg)
	require.NoError(t, err)

	ref, err := ggcrname.ParseReference(fmt.Sprintf("%s/%s", addr, pauseImageTag))
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

	matches, err := filepath.Glob(filepath.Join(outDir, "*.tar.zst"))
	require.NoError(t, err)
	require.Len(t, matches, 1, "expected exactly one package in %s, got %v", outDir, matches)

	return matches[0]
}

// ociLayoutFromRegistry copies a published package out of the registry into an OCI Image
// Layout directory, which is what `skopeo copy docker://... oci-archive:` produces.
func ociLayoutFromRegistry(t *testing.T, addr string, namespace string) string {
	t.Helper()
	ctx := context.Background()

	ref := fmt.Sprintf("%s/%s/%s:%s", addr, namespace, imagePackageName, imagePackageVersion)
	repo, err := remote.NewRepository(ref)
	require.NoError(t, err)
	repo.PlainHTTP = true

	layoutDir := filepath.Join(t.TempDir(), "layout")
	store, err := orasoci.New(layoutDir)
	require.NoError(t, err)

	tag := imagePackageVersion
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
