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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/fileoverride"
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

	file := v1alpha1.ZarfFile{
		Source: "https://rpm.rancher.io/public/k3s.rpm",
		Target: "/usr/local/bin/k3s",
		Shasum: payloadShasum(),
	}

	provenance, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, overrides, nil)
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

	file := v1alpha1.ZarfFile{
		Source: "https://rpm.rancher.io/public/k3s.rpm",
		Target: "/usr/local/bin/k3s",
		Shasum: payloadShasum(),
	}

	provenance, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, overrides, nil)
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

	file := v1alpha1.ZarfFile{
		Source: "https://rpm.rancher.io/public/k3s.rpm",
		Target: "/usr/local/bin/k3s",
	}

	_, err = fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, overrides, nil)
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

	file := v1alpha1.ZarfFile{
		Source: "https://rpm.rancher.io/public/k3s.rpm",
		Target: "/usr/local/bin/k3s",
		Shasum: payloadShasum(),
	}

	_, err = fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, overrides, nil)
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

	file := v1alpha1.ZarfFile{
		Source: srv.URL + "/k3s",
		Target: "/usr/local/bin/k3s",
		Shasum: payloadShasum(),
	}

	provenance, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, overrides, nil)
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
	file := v1alpha1.ZarfFile{
		Source: "k3s",
		Target: "/usr/local/bin/k3s",
		Shasum: payloadShasum(),
	}

	_, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, distroPath, 0, file, nil, nil)
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

	file := v1alpha1.ZarfFile{
		Source:      "https://github.com/rancher/rke2/releases/download/v1.37.0+rke2r1/rke2.tar.gz",
		ExtractPath: "bin/rke2",
		Target:      "/usr/local/bin/rke2",
		Shasum:      extractedHex,
	}

	provenance, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, overrides, nil)
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
	archives := newDownloadCache()
	t.Cleanup(func() {
		require.NoError(t, archives.cleanup())
	})

	file1 := v1alpha1.ZarfFile{
		Source:      srv.URL + "/archive.tar.gz",
		ExtractPath: "bin/one",
		Target:      "/usr/local/bin/one",
		Shasum:      file1Hex,
	}
	file2 := v1alpha1.ZarfFile{
		Source:      srv.URL + "/archive.tar.gz",
		ExtractPath: "bin/two",
		Target:      "/usr/local/bin/two",
		Shasum:      file2Hex,
	}

	_, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file1, nil, archives)
	require.NoError(t, err)

	_, err = fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 1, file2, nil, archives)
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

// isolateTempDirectory points utils.MakeTempDir at a directory this test owns, so a test can
// see exactly which temp directories staging a file left behind.
func isolateTempDirectory(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	previous := config.CommonOptions.TempDirectory
	config.CommonOptions.TempDirectory = dir
	t.Cleanup(func() {
		config.CommonOptions.TempDirectory = previous
	})

	return dir
}

// TestFileGrabberExtractPathWithoutCacheRemovesArchive confirms a caller that passes no cache
// has the downloaded archive cleaned up before fileGrabber returns, rather than left in the
// temp directory for the rest of the process.
func TestFileGrabberExtractPathWithoutCacheRemovesArchive(t *testing.T) {
	content := "extracted binary\n"
	sum := sha256.Sum256([]byte(content))

	srv := serveBytes(t, tarGz(t, tarGzEntry{
		name:    "bin/rke2",
		content: content,
	}), nil)

	tempRoot := isolateTempDirectory(t)
	buildPath := t.TempDir()

	file := v1alpha1.ZarfFile{
		Source:      srv.URL + "/rke2.tar.gz",
		ExtractPath: "bin/rke2",
		Target:      "/usr/local/bin/rke2",
		Shasum:      hex.EncodeToString(sum[:]),
	}

	_, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, nil, nil)
	require.NoError(t, err)

	left, err := os.ReadDir(tempRoot)
	require.NoError(t, err)
	require.Empty(t, left, "downloaded archive must not outlive a fileGrabber call that was given no cache")
}

// TestFileGrabberExtractPathFailedDownloadRemovesArchive confirms a failed download leaves
// nothing behind either, including in the shared cache directory.
func TestFileGrabberExtractPathFailedDownloadRemovesArchive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	tempRoot := isolateTempDirectory(t)
	archives := newDownloadCache()

	file := v1alpha1.ZarfFile{
		Source:      srv.URL + "/rke2.tar.gz",
		ExtractPath: "bin/rke2",
		Target:      "/usr/local/bin/rke2",
	}

	_, err := fileGrabber(context.Background(), string(config.FilesDir), t.TempDir(), t.TempDir(), 0, file, nil, archives)
	require.Error(t, err)

	require.NoError(t, archives.cleanup())
	left, err := os.ReadDir(tempRoot)
	require.NoError(t, err)
	require.Empty(t, left, "a failed download must not leave a temp directory behind")
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
	archives := newDownloadCache()
	t.Cleanup(func() {
		require.NoError(t, archives.cleanup())
	})

	first := v1alpha1.ZarfFile{
		Source:      firstSrv.URL + "/archive.tar.gz",
		ExtractPath: "bin/tool",
		Target:      "/usr/local/bin/first",
		Shasum:      hex.EncodeToString(firstSum[:]),
	}
	second := v1alpha1.ZarfFile{
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

	_, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, first, nil, archives)
	require.NoError(t, err)
	_, err = fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 1, second, nil, archives)
	require.NoError(t, err)
	_, err = fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 2, firstAgain, nil, archives)
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

// TestDownloadCacheCleanupRemovesCachedArchives confirms the cache a whole assemble shares is
// emptied by cleanup, which is the only thing that removes the archives it kept.
func TestDownloadCacheCleanupRemovesCachedArchives(t *testing.T) {
	content := "cached binary\n"
	sum := sha256.Sum256([]byte(content))

	srv := serveBytes(t, tarGz(t, tarGzEntry{
		name:    "bin/rke2",
		content: content,
	}), nil)

	tempRoot := isolateTempDirectory(t)
	archives := newDownloadCache()

	file := v1alpha1.ZarfFile{
		Source:      srv.URL + "/rke2.tar.gz",
		ExtractPath: "bin/rke2",
		Target:      "/usr/local/bin/rke2",
		Shasum:      hex.EncodeToString(sum[:]),
	}

	_, err := fileGrabber(context.Background(), string(config.FilesDir), t.TempDir(), t.TempDir(), 0, file, nil, archives)
	require.NoError(t, err)

	left, err := os.ReadDir(tempRoot)
	require.NoError(t, err)
	require.Len(t, left, 1, "a cached archive must survive the call that downloaded it")

	require.NoError(t, archives.cleanup())
	left, err = os.ReadDir(tempRoot)
	require.NoError(t, err)
	require.Empty(t, left)

	// cleanup is called from a defer, so it has to tolerate a cache that already ran it and
	// one that never downloaded anything at all.
	require.NoError(t, archives.cleanup())
	require.NoError(t, newDownloadCache().cleanup())
	require.NoError(t, (*downloadCache)(nil).cleanup())
}
