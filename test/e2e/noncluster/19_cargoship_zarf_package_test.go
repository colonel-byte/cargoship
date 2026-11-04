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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/test"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"oras.land/oras-go/v2"
	orasoci "oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"
)

const (
	imageFileDistroDir = "test/e2e/noncluster/testdata/minimal-image-file"
	zarfPayloadFixture = "test/e2e/noncluster/testdata/minimal-image-file/payload.sh"
	// The source the definition declares for its file, and so the key the package is searched
	// by. It is never fetched: the store answers for it.
	zarfPayloadURL = "https://example.invalid/cargoship/payload.sh"
)

// TestCargoshipCreateFromZarfPackage builds a package with --content-store pointed at a zarf
// package, and checks that both halves -- the images and the declared file -- came out of it
// rather than from the registry and the network.
//
// The package is assembled here rather than by the zarf CLI, which the suite cannot assume is
// installed: a built zarf package is an images/ OCI Image Layout whose index entries carry the
// reference each image was pulled under, plus a zarf.yaml whose component files keep the source
// their author declared, plus one plain tar per component holding the bytes. That is what is
// reproduced below, and the format itself is covered against real zarf output by
// TestRealZarfPackage in pkg/zarfpackage.
//
// What this adds over the unit tests is the built binary, a real definition, and the file path:
// a file taken from a store is verified against the definition's shasum and then staged through
// the same extraction and permission handling as a downloaded one, so a reader that returned
// the wrong bytes fails here rather than on a node.
func TestCargoshipCreateFromZarfPackage(t *testing.T) {
	addr := test.SetupInMemoryRegistry(t)
	pushTestImage(t, addr, pauseImageTag)

	payload, err := os.ReadFile(zarfPayloadFixture)
	require.NoError(t, err)

	pkgPath := zarfPackageFromRegistry(t, addr, []string{pauseImageTag}, payload)

	defDir := copyDistroDefinition(t, imageFileDistroDir)
	defPath := filepath.Join(defDir, "distro.yaml")
	data, err := os.ReadFile(defPath)
	require.NoError(t, err)
	rewritten := strings.ReplaceAll(string(data), "__REGISTRY__", addr)
	// The payload's checksum is substituted rather than committed: the fixture is rewritten by
	// the addlicense and end-of-file hooks, so a checksum in the definition would go stale the
	// next time one of them touched it, and fail here as a shasum mismatch.
	payloadSum := sha256.Sum256(payload)
	rewritten = strings.ReplaceAll(rewritten, "__PAYLOAD_SHA256__", hex.EncodeToString(payloadSum[:]))
	require.NotContains(t, rewritten, "__PAYLOAD_SHA256__")
	require.NoError(t, os.WriteFile(defPath, []byte(rewritten), 0o600))

	outDir := t.TempDir()
	stdout, stderr, err := e2e.Cargoship(t, "create", defDir, "--content-store", pkgPath, "-o", outDir)
	require.NoError(t, err)

	// The log is the only place the build says which source it used, and a silent fallback to
	// the registry or to the file's URL would otherwise look exactly like a success.
	output := stdout + stderr
	require.Contains(t, output, "resolving package content from a local content store")
	require.Contains(t, output, "zarf package")
	require.Contains(t, output, "file read from content store")
	require.NotContains(t, output, "not held by the local image source",
		"the declared image is in the package, so nothing should have fallen back to the registry")
	require.NotContains(t, output, "file not held by the content store",
		"the declared file is in the package, so nothing should have fallen back to its URL")

	pkg := requireSinglePackage(t, outDir)
	requireZarfSourcedContent(t, pkg, addr, payload)
}

// requireZarfSourcedContent unpacks a built package and checks it holds the declared image,
// annotated the way a registry pull would leave it, and the declared file with the bytes the
// store held rather than anything fetched.
func requireZarfSourcedContent(t *testing.T, pkg string, addr string, payload []byte) {
	t.Helper()

	unpacked := filepath.Join(t.TempDir(), "pkg")
	require.NoError(t, os.MkdirAll(unpacked, 0o755))
	require.NoError(t, archive.Decompress(context.Background(), pkg, unpacked, archive.DecompressOpts{}))

	indexBytes, err := os.ReadFile(filepath.Join(unpacked, "images", ocispec.ImageIndexFile))
	require.NoError(t, err)
	var index ocispec.Index
	require.NoError(t, json.Unmarshal(indexBytes, &index))

	ref := addr + "/" + pauseImageTag
	var found bool
	for _, m := range index.Manifests {
		if m.Annotations[ocispec.AnnotationRefName] == ref {
			found = true
			require.Equal(t, ref, m.Annotations["org.opencontainers.image.base.name"],
				"%s carries no base-name annotation, so the package could not be read back", ref)
		}
	}
	require.True(t, found, "the package's image layout does not hold %s", ref)

	// The file is staged at files/<index>/<basename of target>, so the definition's single
	// file entry lands at a path the definition itself determines.
	staged := filepath.Join(unpacked, "files", "0", "cargoship-payload.sh")
	got, err := os.ReadFile(staged)
	require.NoError(t, err, "the package does not hold the declared file")
	require.Equal(t, string(payload), string(got),
		"the staged file is not the bytes the content store held")
}

// zarfPackageFromRegistry builds a zarf-shaped package archive holding each tag, copied out of
// the in-memory registry and annotated the way zarf annotates what it pulls, plus one component
// file carrying payload under the URL the definition declares.
func zarfPackageFromRegistry(t *testing.T, addr string, tags []string, payload []byte) string {
	t.Helper()
	ctx := context.Background()

	pkgDir := filepath.Join(t.TempDir(), "zarf-package")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))

	// images/ is a plain OCI layout, written by oras and then re-indexed with the reference
	// annotations zarf writes and oras does not.
	layoutDir := filepath.Join(pkgDir, "images")
	store, err := orasoci.New(layoutDir)
	require.NoError(t, err)

	entries := make([]ocispec.Descriptor, 0, len(tags))
	for _, tag := range tags {
		ref := addr + "/" + tag
		repo, err := remote.NewRepository(ref)
		require.NoError(t, err)
		repo.PlainHTTP = true

		desc, err := oras.Copy(ctx, repo, ref, store, ref, oras.DefaultCopyOptions)
		require.NoError(t, err)

		desc.Annotations = map[string]string{
			ocispec.AnnotationRefName:       ref,
			ocispec.AnnotationBaseImageName: ref,
		}
		entries = append(entries, desc)
	}
	indexBytes, err := json.Marshal(ocispec.Index{
		Versioned: ocispec.Index{}.Versioned,
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: entries,
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(layoutDir, ocispec.ImageIndexFile), indexBytes, 0o644))

	// zarf.yaml, with the file's source left exactly as an author would have written it --
	// which is the fact the whole file path depends on.
	const component = "payload"
	definition := "" +
		"apiVersion: zarf.dev/v1alpha1\n" +
		"kind: ZarfPackageConfig\n" +
		"metadata:\n" +
		"  name: e2e-fixture\n" +
		"components:\n" +
		"  - name: " + component + "\n" +
		"    files:\n" +
		"      - source: " + zarfPayloadURL + "\n" +
		"        target: /usr/local/bin/payload.sh\n"
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "zarf.yaml"), []byte(definition), 0o644))

	// components/<name>.tar: a plain tar whose members carry the component name, with the file
	// at files/<index>/<basename of target>.
	componentsDir := filepath.Join(pkgDir, "components")
	require.NoError(t, os.MkdirAll(componentsDir, 0o755))
	tarFile, err := os.Create(filepath.Join(componentsDir, component+".tar"))
	require.NoError(t, err)
	tw := tar.NewWriter(tarFile)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     component + "/files/0/payload.sh",
		Mode:     0o644,
		Size:     int64(len(payload)),
		Typeflag: tar.TypeReg,
	}))
	_, err = tw.Write(payload)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, tarFile.Close())

	// zarf maps the package's contents to the archive root rather than nesting them under a
	// directory, so the sources are the package's own entries.
	entriesOnDisk, err := os.ReadDir(pkgDir)
	require.NoError(t, err)
	sources := make([]string, 0, len(entriesOnDisk))
	for _, e := range entriesOnDisk {
		sources = append(sources, filepath.Join(pkgDir, e.Name()))
	}

	archivePath := filepath.Join(t.TempDir(), "zarf-package-e2e-fixture-amd64.tar.zst")
	require.NoError(t, archive.Compress(ctx, sources, archivePath, archive.CompressOpts{}))
	return archivePath
}
