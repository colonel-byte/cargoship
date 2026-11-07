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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/api"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/fileoverride"
	"github.com/colonel-byte/cargoship/pkg/haulerstore"
	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

const grabbedPayload = "k3s binary contents\n"

func payloadShasum() string {
	sum := sha256.Sum256([]byte(grabbedPayload))
	return hex.EncodeToString(sum[:])
}

// mirrorServer serves grabbedPayload at every path, standing in for an internal mirror.
func mirrorServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("mirror server write failed: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFileGrabberRemoteOverride(t *testing.T) {
	srv := mirrorServer(t, grabbedPayload)
	buildPath := t.TempDir()

	overrides, err := fileoverride.Parse([]string{"https://rpm.rancher.io=" + srv.URL + "/mirror"})
	require.NoError(t, err)

	file := v1alpha1.File{
		Source: "https://rpm.rancher.io/public/k3s.rpm",
		Target: "/usr/local/bin/k3s",
		Shasum: payloadShasum(),
	}

	provenance, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, overrides, nil, ociOptions{})
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "0", "k3s"))
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, string(got))

	require.NotNil(t, provenance, "an overridden file must report where it actually came from")
	require.Equal(t, distro.FileSource{
		Path:     "files/0/k3s",
		Declared: "https://rpm.rancher.io/public/k3s.rpm",
		Resolved: srv.URL + "/mirror/public/k3s.rpm",
		Override: "https://rpm.rancher.io",
		Shasum:   payloadShasum(),
	}, *provenance)
}

func TestFileGrabberLocalOverride(t *testing.T) {
	staged := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(staged, "public"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(staged, "public", "k3s.rpm"), []byte(grabbedPayload), 0o600))

	buildPath := t.TempDir()
	overrides, err := fileoverride.Parse([]string{"https://rpm.rancher.io=" + staged})
	require.NoError(t, err)

	file := v1alpha1.File{
		Source: "https://rpm.rancher.io/public/k3s.rpm",
		Target: "/usr/local/bin/k3s",
		Shasum: payloadShasum(),
	}

	provenance, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, overrides, nil, ociOptions{})
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "0", "k3s"))
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, string(got))

	require.NotNil(t, provenance)
	require.Equal(t, filepath.Join(staged, "public", "k3s.rpm"), provenance.Resolved,
		"a local override records the path on the build host the bytes were read from")
	require.Equal(t, "files/0/k3s", provenance.Path)
}

// TestFileGrabberRefusesOverrideWithoutShasum covers the one case this feature must not make
// easy: redirecting a download that nothing can verify.
func TestFileGrabberRefusesOverrideWithoutShasum(t *testing.T) {
	srv := mirrorServer(t, grabbedPayload)
	buildPath := t.TempDir()

	overrides, err := fileoverride.Parse([]string{"https://rpm.rancher.io=" + srv.URL})
	require.NoError(t, err)

	file := v1alpha1.File{
		Source: "https://rpm.rancher.io/public/k3s.rpm",
		Target: "/usr/local/bin/k3s",
	}

	_, err = fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, overrides, nil, ociOptions{})
	require.ErrorContains(t, err, "declares no shasum")
}

// TestFileGrabberChecksumMismatchLeavesNothingBehind is the regression guard for a staged file
// surviving a failure: buildPath is walked later to generate the package checksums, so anything
// left at dst would ship.
func TestFileGrabberChecksumMismatchLeavesNothingBehind(t *testing.T) {
	srv := mirrorServer(t, "not what the definition declared\n")
	buildPath := t.TempDir()

	overrides, err := fileoverride.Parse([]string{"https://rpm.rancher.io=" + srv.URL})
	require.NoError(t, err)

	file := v1alpha1.File{
		Source: "https://rpm.rancher.io/public/k3s.rpm",
		Target: "/usr/local/bin/k3s",
		Shasum: payloadShasum(),
	}

	_, err = fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, overrides, nil, ociOptions{})
	require.Error(t, err)

	_, statErr := os.Stat(filepath.Join(buildPath, string(config.FilesDir), "0", "k3s"))
	require.True(t, os.IsNotExist(statErr), "a file that failed its checksum must not be left in the build path")
}

// TestFileGrabberUnmatchedSourceIsUntouched confirms an override set that does not apply leaves
// the declared source alone rather than guessing.
func TestFileGrabberUnmatchedSourceIsUntouched(t *testing.T) {
	srv := mirrorServer(t, grabbedPayload)
	buildPath := t.TempDir()

	overrides, err := fileoverride.Parse([]string{"https://rpm.rancher.io=https://mirror.invalid"})
	require.NoError(t, err)

	file := v1alpha1.File{
		Source: srv.URL + "/k3s",
		Target: "/usr/local/bin/k3s",
		Shasum: payloadShasum(),
	}

	provenance, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, overrides, nil, ociOptions{})
	require.NoError(t, err)
	require.Nil(t, provenance, "a file no override touched must record nothing, so packages built without overrides are unchanged")

	got, err := os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "0", "k3s"))
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, string(got))
}

// TestFileGrabberLocalSourceStillResolvesAgainstDistroPath confirms the override plumbing did
// not disturb a definition that already points at a file next to itself.
func TestFileGrabberLocalSourceStillResolvesAgainstDistroPath(t *testing.T) {
	distroPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(distroPath, "k3s"), []byte(grabbedPayload), 0o600))

	buildPath := t.TempDir()
	file := v1alpha1.File{
		Source: "k3s",
		Target: "/usr/local/bin/k3s",
		Shasum: payloadShasum(),
	}

	_, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, distroPath, 0, file, nil, nil, ociOptions{})
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "0", "k3s"))
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, string(got))
}

// TestFileGrabberExtractPathChecksExtractedFileShasum confirms that when extractPath is set,
// file.Shasum verifies the extracted file at dst rather than the downloaded archive.
func TestFileGrabberExtractPathChecksExtractedFileShasum(t *testing.T) {
	extractedContent := "extracted rke2 binary\n"
	extractedSum := sha256.Sum256([]byte(extractedContent))
	extractedHex := hex.EncodeToString(extractedSum[:])

	srv := serveBytes(t, tarGz(t, tarGzEntry{
		name:    "bin/rke2",
		content: extractedContent,
	}), nil)

	buildPath := t.TempDir()
	overrides, err := fileoverride.Parse([]string{"https://github.com=" + srv.URL})
	require.NoError(t, err)

	file := v1alpha1.File{
		Source:      "https://github.com/rancher/rke2/releases/download/v1.37.0+rke2r1/rke2.tar.gz",
		ExtractPath: "bin/rke2",
		Target:      "/usr/local/bin/rke2",
		Shasum:      extractedHex,
	}

	provenance, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, overrides, nil, ociOptions{})
	require.NoError(t, err)
	require.NotNil(t, provenance)
	require.Equal(t, extractedHex, provenance.Shasum)

	got, err := os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "0", "rke2"))
	require.NoError(t, err)
	require.Equal(t, extractedContent, string(got))
}

// TestFileGrabberExtractPathReusesCachedArchive confirms that multiple files extracted from the
// same URL only download the archive once when sharing an in-session cache.
func TestFileGrabberExtractPathReusesCachedArchive(t *testing.T) {
	file1Content := "binary one\n"
	file1Sum := sha256.Sum256([]byte(file1Content))
	file1Hex := hex.EncodeToString(file1Sum[:])

	file2Content := "binary two\n"
	file2Sum := sha256.Sum256([]byte(file2Content))
	file2Hex := hex.EncodeToString(file2Sum[:])

	var downloadCount int
	srv := serveBytes(t, tarGz(t,
		tarGzEntry{
			name:    "bin/one",
			content: file1Content,
		},
		tarGzEntry{
			name:    "bin/two",
			content: file2Content,
		},
	), &downloadCount)

	buildPath := t.TempDir()
	isolateCacheDirectory(t)

	file1 := v1alpha1.File{
		Source:      srv.URL + "/archive.tar.gz",
		ExtractPath: "bin/one",
		Target:      "/usr/local/bin/one",
		Shasum:      file1Hex,
	}
	file2 := v1alpha1.File{
		Source:      srv.URL + "/archive.tar.gz",
		ExtractPath: "bin/two",
		Target:      "/usr/local/bin/two",
		Shasum:      file2Hex,
	}

	_, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file1, nil, nil, ociOptions{})
	require.NoError(t, err)

	_, err = fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 1, file2, nil, nil, ociOptions{})
	require.NoError(t, err)

	require.Equal(t, 1, downloadCount, "archive must be downloaded exactly once across both extractions")

	got1, err := os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "0", "one"))
	require.NoError(t, err)
	require.Equal(t, file1Content, string(got1))

	got2, err := os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "1", "two"))
	require.NoError(t, err)
	require.Equal(t, file2Content, string(got2))
}

// tarGzEntry is one file written into a test archive by tarGz.
type tarGzEntry struct {
	name    string
	content string
}

// tarGz builds a gzipped tar holding entries, in order.
func tarGz(t *testing.T, entries ...tarGzEntry) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, e := range entries {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: e.name,
			Mode: 0o755,
			Size: int64(len(e.content)),
		}))
		_, err := tw.Write([]byte(e.content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	return buf.Bytes()
}

// serveBytes serves body at every path, counting the requests it answered.
func serveBytes(t *testing.T, body []byte, requests *int) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests != nil {
			*requests++
		}
		if _, err := w.Write(body); err != nil {
			t.Errorf("writing archive: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

// isolateCacheDirectory points the shared file cache at a directory of this test's own and
// returns the files/ subdirectory inside it, where a cached archive lands. Without this a test
// that stages an extractPath file would write into the running user's real cache.
func isolateCacheDirectory(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	previous := config.CommonOptions.CachePath
	config.CommonOptions.CachePath = dir
	t.Cleanup(func() {
		config.CommonOptions.CachePath = previous
	})

	return filepath.Join(dir, "files")
}

// TestFileGrabberExtractPathCachesArchiveAcrossBuilds confirms the archive an extractPath file
// came out of is kept in the shared cache and reused by a later, entirely separate build. This
// is the behaviour the per-build cache it replaced could not have: an archive declares no digest
// of its own, so nothing used to key it, and every build re-downloaded every one.
func TestFileGrabberExtractPathCachesArchiveAcrossBuilds(t *testing.T) {
	content := "extracted binary\n"
	sum := sha256.Sum256([]byte(content))

	var downloadCount int
	srv := serveBytes(t, tarGz(t, tarGzEntry{
		name:    "bin/rke2",
		content: content,
	}), &downloadCount)

	cacheFiles := isolateCacheDirectory(t)

	file := v1alpha1.File{
		Source:      srv.URL + "/rke2.tar.gz",
		ExtractPath: "bin/rke2",
		Target:      "/usr/local/bin/rke2",
		Shasum:      hex.EncodeToString(sum[:]),
	}

	// Two builds, each with its own build directory, as two invocations of create would have.
	for _, buildPath := range []string{t.TempDir(), t.TempDir()} {
		_, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, nil, nil, ociOptions{})
		require.NoError(t, err)

		got, err := os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "0", "rke2"))
		require.NoError(t, err)
		require.Equal(t, content, string(got))
	}

	require.Equal(t, 1, downloadCount, "the second build must extract from the cached archive rather than download it again")

	cached, err := os.ReadDir(cacheFiles)
	require.NoError(t, err)
	require.Len(t, cached, 1, "the archive must be left in the shared cache for the next build")
	require.True(t, strings.HasPrefix(cached[0].Name(), "url-"),
		"a URL-keyed entry must be distinguishable from the shasum-keyed entries beside it, got %q", cached[0].Name())
}

// TestFileGrabberExtractPathFailedDownloadCachesNothing confirms a failed download leaves no
// entry in the shared cache. It matters more now that the cache outlives the build: an entry is
// served on its name alone, so a truncated or empty one would be handed to every later build as
// though it were the archive.
func TestFileGrabberExtractPathFailedDownloadCachesNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	cacheFiles := isolateCacheDirectory(t)

	file := v1alpha1.File{
		Source:      srv.URL + "/rke2.tar.gz",
		ExtractPath: "bin/rke2",
		Target:      "/usr/local/bin/rke2",
	}

	_, err := fileGrabber(context.Background(), string(config.FilesDir), t.TempDir(), t.TempDir(), 0, file, nil, nil, ociOptions{})
	require.Error(t, err)

	left, err := os.ReadDir(cacheFiles)
	if err != nil {
		require.ErrorIs(t, err, os.ErrNotExist)
		return
	}
	require.Empty(t, left, "a failed download must leave nothing in the cache for a later build to find")
}

// TestFileGrabberExtractPathArchivesSharingABaseName confirms two different URLs whose paths
// end in the same file name get their own slot in the cache rather than overwriting each other.
func TestFileGrabberExtractPathArchivesSharingABaseName(t *testing.T) {
	firstContent := "from the first archive\n"
	firstSum := sha256.Sum256([]byte(firstContent))
	secondContent := "from the second archive\n"
	secondSum := sha256.Sum256([]byte(secondContent))

	firstSrv := serveBytes(t, tarGz(t, tarGzEntry{
		name:    "bin/tool",
		content: firstContent,
	}), nil)
	secondSrv := serveBytes(t, tarGz(t, tarGzEntry{
		name:    "bin/tool",
		content: secondContent,
	}), nil)

	buildPath := t.TempDir()
	isolateCacheDirectory(t)

	first := v1alpha1.File{
		Source:      firstSrv.URL + "/archive.tar.gz",
		ExtractPath: "bin/tool",
		Target:      "/usr/local/bin/first",
		Shasum:      hex.EncodeToString(firstSum[:]),
	}
	second := v1alpha1.File{
		Source:      secondSrv.URL + "/archive.tar.gz",
		ExtractPath: "bin/tool",
		Target:      "/usr/local/bin/second",
		Shasum:      hex.EncodeToString(secondSum[:]),
	}

	// The third file comes back to the first URL, so it is served from the cache after the
	// second archive has been downloaded. That is the ordering a shared directory would get
	// wrong: both archives are named archive.tar.gz, and the cache hit would read whichever
	// one was written last.
	firstAgain := first
	firstAgain.Target = "/usr/local/bin/first-again"

	_, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, first, nil, nil, ociOptions{})
	require.NoError(t, err)
	_, err = fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 1, second, nil, nil, ociOptions{})
	require.NoError(t, err)
	_, err = fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 2, firstAgain, nil, nil, ociOptions{})
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "0", "first"))
	require.NoError(t, err)
	require.Equal(t, firstContent, string(got))

	got, err = os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "1", "second"))
	require.NoError(t, err)
	require.Equal(t, secondContent, string(got))

	got, err = os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "2", "first-again"))
	require.NoError(t, err)
	require.Equal(t, firstContent, string(got))
}

// haulWithFile writes a hauler store holding one file artifact recorded as having come from
// originalRef, and returns it opened. Only a file artifact is built: fileGrabber never looks at
// the image half of a store.
func haulWithFile(t *testing.T, originalRef string, name string, content string) *haulerstore.Store {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "store")
	blobDir := filepath.Join(dir, "blobs", "sha256")
	require.NoError(t, os.MkdirAll(blobDir, 0o755))

	blob := func(mediaType string, b []byte, annotations map[string]string) ocispec.Descriptor {
		sum := sha256.Sum256(b)
		hexSum := hex.EncodeToString(sum[:])
		require.NoError(t, os.WriteFile(filepath.Join(blobDir, hexSum), b, 0o644))
		return ocispec.Descriptor{
			MediaType:   mediaType,
			Digest:      digest.NewDigestFromEncoded(digest.SHA256, hexSum),
			Size:        int64(len(b)),
			Annotations: annotations,
		}
	}

	layer := blob("application/vnd.content.hauler.file.layer.v1", []byte(content),
		map[string]string{ocispec.AnnotationTitle: name})
	config := blob("application/vnd.content.hauler.file.http.config.v1+json", []byte(`{}`), nil)
	manifestBytes, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    config,
		Layers:    []ocispec.Descriptor{layer},
	})
	require.NoError(t, err)
	manifest := blob(ocispec.MediaTypeImageManifest, manifestBytes, map[string]string{
		haulerstore.AnnotationOriginalRef: originalRef,
		haulerstore.AnnotationKind:        "dev.hauler/image",
	})

	indexBytes, err := json.Marshal(ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: []ocispec.Descriptor{manifest},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ocispec.ImageIndexFile), indexBytes, 0o644))

	store, err := haulerstore.Open(context.Background(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}

func TestFileGrabberFromHaulerStore(t *testing.T) {
	const declared = "https://get.k3s.io/k3s"
	buildPath := t.TempDir()
	store := haulWithFile(t, declared, "k3s", grabbedPayload)

	file := v1alpha1.File{
		Source: declared,
		Target: "/usr/local/bin/k3s",
		Shasum: payloadShasum(),
	}

	provenance, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, nil, store, ociOptions{})
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "0", "k3s"))
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, string(got))

	require.NotNil(t, provenance, "a file taken from a store must report where it came from")
	require.Equal(t, distro.FileSource{
		Path:     "files/0/k3s",
		Declared: declared,
		Resolved: "k3s",
		Store:    store.Source(),
		Shasum:   payloadShasum(),
	}, *provenance)
}

// The store is bytes the definition's author never saw, so -- exactly as for an override -- it
// is refused for a file that declares no shasum to verify it against.
func TestFileGrabberHaulerStoreRefusesFileWithoutShasum(t *testing.T) {
	const declared = "https://get.k3s.io/k3s"
	store := haulWithFile(t, declared, "k3s", grabbedPayload)

	file := v1alpha1.File{
		Source: declared,
		Target: "/usr/local/bin/k3s",
	}

	_, err := fileGrabber(context.Background(), string(config.FilesDir), t.TempDir(), t.TempDir(), 0, file, nil, store, ociOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "shasum")
}

// An explicit override was asked for by name, so it wins over a store that happens to hold the
// same file.
func TestFileGrabberOverrideBeatsHaulerStore(t *testing.T) {
	const declared = "https://get.k3s.io/k3s"
	srv := mirrorServer(t, grabbedPayload)
	buildPath := t.TempDir()
	store := haulWithFile(t, declared, "k3s", "store contents that must not be used\n")

	overrides, err := fileoverride.Parse([]string{"https://get.k3s.io=" + srv.URL + "/mirror"})
	require.NoError(t, err)

	file := v1alpha1.File{
		Source: declared,
		Target: "/usr/local/bin/k3s",
		Shasum: payloadShasum(),
	}

	provenance, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, overrides, store, ociOptions{})
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "0", "k3s"))
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, string(got), "the override's bytes must win over the store's")
	require.Equal(t, srv.URL+"/mirror/k3s", provenance.Resolved)
	require.Empty(t, provenance.Store, "an overridden file did not come from the store")
}

// A file the store does not hold falls back to its declared source, so a partial haul still
// builds.
func TestFileGrabberFallsBackWhenStoreLacksTheFile(t *testing.T) {
	srv := mirrorServer(t, grabbedPayload)
	buildPath := t.TempDir()
	store := haulWithFile(t, "https://get.k3s.io/something-else", "other", "other\n")

	file := v1alpha1.File{
		Source: srv.URL + "/k3s",
		Target: "/usr/local/bin/k3s",
		Shasum: payloadShasum(),
	}

	provenance, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, nil, store, ociOptions{})
	require.NoError(t, err)
	require.Nil(t, provenance, "a file read from its declared source reports no redirection")

	got, err := os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "0", "k3s"))
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, string(got))
}

// haulWithMultiPlatformFile writes a hauler store holding one file artifact published as an
// image index over one titled manifest per architecture, and returns it opened. This is the
// shape a vendor-published engine bundle has, and the one a store resolved nothing for.
func haulWithMultiPlatformFile(t *testing.T, originalRef string, name string, perArch map[string]string) *haulerstore.Store {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "store")
	blobDir := filepath.Join(dir, "blobs", "sha256")
	require.NoError(t, os.MkdirAll(blobDir, 0o755))

	blob := func(mediaType string, b []byte, annotations map[string]string) ocispec.Descriptor {
		sum := sha256.Sum256(b)
		hexSum := hex.EncodeToString(sum[:])
		require.NoError(t, os.WriteFile(filepath.Join(blobDir, hexSum), b, 0o644))
		return ocispec.Descriptor{
			MediaType:   mediaType,
			Digest:      digest.NewDigestFromEncoded(digest.SHA256, hexSum),
			Size:        int64(len(b)),
			Annotations: annotations,
		}
	}

	children := make([]ocispec.Descriptor, 0, len(perArch))
	for _, arch := range slices.Sorted(maps.Keys(perArch)) {
		layer := blob("application/vnd.content.hauler.file.layer.v1", []byte(perArch[arch]),
			map[string]string{ocispec.AnnotationTitle: name})
		config := blob("application/vnd.content.hauler.file.http.config.v1+json", []byte(`{}`), nil)
		manifestBytes, err := json.Marshal(ocispec.Manifest{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: ocispec.MediaTypeImageManifest,
			Config:    config,
			Layers:    []ocispec.Descriptor{layer},
		})
		require.NoError(t, err)
		child := blob(ocispec.MediaTypeImageManifest, manifestBytes, nil)
		child.Platform = &ocispec.Platform{OS: "linux", Architecture: arch}
		children = append(children, child)
	}

	indexBytes, err := json.Marshal(ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: children,
	})
	require.NoError(t, err)
	entry := blob(ocispec.MediaTypeImageIndex, indexBytes, map[string]string{
		haulerstore.AnnotationOriginalRef: originalRef,
		haulerstore.AnnotationKind:        "dev.hauler/image",
	})

	outerBytes, err := json.Marshal(ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: []ocispec.Descriptor{entry},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ocispec.ImageIndexFile), outerBytes, 0o644))

	store, err := haulerstore.Open(context.Background(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}

// The entry's own selector.arch is what picks a platform out of a multi-platform source. It is
// the only thing that can: a file is staged arch-blind, keyed on its index in the list, and
// nothing else in the build knows which architecture an entry meant.
func TestFileGrabberMultiPlatformStoreUsesSelectorArch(t *testing.T) {
	const declared = "oci://registry.example.com/vendor/engine:v1.37.1"
	buildPath := t.TempDir()
	store := haulWithMultiPlatformFile(t, declared, "engine", map[string]string{
		"amd64": grabbedPayload,
		"arm64": "arm64 contents that must not be staged\n",
	})

	file := v1alpha1.File{
		Source:   declared,
		Target:   "/usr/local/bin/engine",
		Shasum:   payloadShasum(),
		Selector: v1alpha1.BinarySelector{Arch: api.Arches{api.ArchAMD64}},
	}

	provenance, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, nil, store, ociOptions{})
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "0", "engine"))
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, string(got), "the arm64 child was staged for an amd64 entry")

	require.NotNil(t, provenance)
	require.Equal(t, store.Source(), provenance.Store)
}

// An entry that names no single architecture cannot say which platform of a multi-platform
// source it meant, and the refusal has to name the entry: falling back to the declared source
// would reach the network for content the operator staged.
func TestFileGrabberMultiPlatformStoreRefusesAmbiguousEntry(t *testing.T) {
	const declared = "oci://registry.example.com/vendor/engine:v1.37.1"
	store := haulWithMultiPlatformFile(t, declared, "engine", map[string]string{
		"amd64": grabbedPayload,
		"arm64": "arm64 contents\n",
	})

	for _, tc := range []struct {
		name     string
		selector v1alpha1.BinarySelector
	}{
		{
			name:     "no architecture",
			selector: v1alpha1.BinarySelector{},
		},
		{
			name:     "several architectures",
			selector: v1alpha1.BinarySelector{Arch: api.Arches{api.ArchAMD64, api.ArchARM64}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := v1alpha1.File{
				Source:   declared,
				Target:   "/usr/local/bin/engine",
				Shasum:   payloadShasum(),
				Selector: tc.selector,
			}

			_, err := fileGrabber(context.Background(), string(config.FilesDir), t.TempDir(), t.TempDir(), 0, file, nil, store, ociOptions{})
			require.Error(t, err)
			require.Contains(t, err.Error(), "selector.arch")
			require.Contains(t, err.Error(), declared)
		})
	}
}

// An extractPath whose leading segment is the target's own base name used to fail, because the
// extraction creates a directory at exactly the path the staged member is then renamed onto:
//
//	os/0/zarf-injector/files/0/zarf-injector  ->  os/0/zarf-injector
//
// The rename reported "file exists" naming a path the definition never mentions. zarf
// publishes every component tarball that way, so the first real case of it was pulling the
// injector out of ghcr.io/zarf-dev/packages/init. It has nothing to do with where the archive
// came from -- it is the shared tail of fileGrabber -- so it is asserted here over a local
// source, which is the cheapest way to reach it.
func TestFileGrabberExtractPathSharingTheTargetName(t *testing.T) {
	buildPath := t.TempDir()
	distroPath := t.TempDir()

	archive := tarGz(t, tarGzEntry{name: "zarf-injector/files/0/zarf-injector", content: grabbedPayload})
	require.NoError(t, os.WriteFile(filepath.Join(distroPath, "component.tar.gz"), archive, 0o600))

	file := v1alpha1.File{
		Source:      "component.tar.gz",
		ExtractPath: "zarf-injector/files/0/zarf-injector",
		Target:      "/usr/local/bin/zarf-injector",
		Executable:  true,
		Shasum:      payloadShasum(),
	}

	_, err := fileGrabber(context.Background(), string(config.OSDir), buildPath, distroPath, 0, file, nil, nil, ociOptions{})
	require.NoError(t, err)

	staged := filepath.Join(buildPath, string(config.OSDir), "0", "zarf-injector")
	got, err := os.ReadFile(staged)
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, string(got))

	info, err := os.Stat(staged)
	require.NoError(t, err)
	require.False(t, info.IsDir(), "the staged target must be the extracted file, not the directory the extraction created")

	// Nothing of the extracted tree may survive beside it: buildPath is walked to generate the
	// package checksums, so a leftover directory would ship.
	entries, err := os.ReadDir(filepath.Join(buildPath, string(config.OSDir), "0"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "the extraction left something behind beside the staged file")
	require.Equal(t, "zarf-injector", entries[0].Name())
}
