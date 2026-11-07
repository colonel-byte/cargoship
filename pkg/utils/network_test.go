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

package utils

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/helpers"
)

func TestDownloadToFileWithChecksumCachesAndUsesCache(t *testing.T) {
	ctx := context.Background()
	cacheRoot := t.TempDir()
	orig := config.CommonOptions.CachePath
	config.CommonOptions.CachePath = cacheRoot
	t.Cleanup(func() { config.CommonOptions.CachePath = orig })

	content := []byte("test-content")
	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])

	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if _, err := w.Write(content); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	dst := filepath.Join(t.TempDir(), "out.bin")
	cacheBase := "test-file.bin"

	// First download should hit server and populate cache.
	if err := DownloadToFileWithChecksum(ctx, server.URL+"/file", dst, checksum, cacheBase); err != nil {
		t.Fatalf("DownloadToFileWithChecksum first call failed: %v", err)
	}

	if hits != 1 {
		t.Fatalf("expected 1 HTTP hit on first download, got %d", hits)
	}

	cacheDir, err := config.GetAbsCachePath()
	if err != nil {
		t.Fatalf("GetAbsCachePath failed: %v", err)
	}

	filesDir := filepath.Join(cacheDir, cacheFilesSubdir)
	cacheFile := filepath.Join(filesDir, checksum+"_"+cacheBase)
	if _, err := os.Stat(cacheFile); err != nil {
		t.Fatalf("expected cache file %s to exist: %v", cacheFile, err)
	}

	cachedSum, err := helpers.GetSHA256OfFile(cacheFile)
	if err != nil {
		t.Fatalf("GetSHA256OfFile on cache failed: %v", err)
	}
	if cachedSum != checksum {
		t.Fatalf("cache file checksum mismatch: got %s, want %s", cachedSum, checksum)
	}

	// Second download should read from cache and not hit server again.
	dst2 := filepath.Join(t.TempDir(), "out2.bin")
	if err := DownloadToFileWithChecksum(ctx, server.URL+"/file", dst2, checksum, cacheBase); err != nil {
		t.Fatalf("DownloadToFileWithChecksum second call failed: %v", err)
	}

	if hits != 1 {
		t.Fatalf("expected no additional HTTP hits on cache use, got %d", hits)
	}

	out2Sum, err := helpers.GetSHA256OfFile(dst2)
	if err != nil {
		t.Fatalf("GetSHA256OfFile dst2 failed: %v", err)
	}
	if out2Sum != checksum {
		t.Fatalf("dst2 checksum mismatch: got %s, want %s", out2Sum, checksum)
	}
}

func TestDownloadToFileWithChecksumCorruptCacheTriggersRefresh(t *testing.T) {
	ctx := context.Background()
	cacheRoot := t.TempDir()
	orig := config.CommonOptions.CachePath
	config.CommonOptions.CachePath = cacheRoot
	t.Cleanup(func() { config.CommonOptions.CachePath = orig })

	goodContent := []byte("good-content")
	goodSum := sha256.Sum256(goodContent)
	checksum := hex.EncodeToString(goodSum[:])

	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if _, err := w.Write(goodContent); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	dst := filepath.Join(t.TempDir(), "out.bin")
	cacheBase := "corrupt.bin"

	// Initial download populates cache.
	if err := DownloadToFileWithChecksum(ctx, server.URL+"/file", dst, checksum, cacheBase); err != nil {
		t.Fatalf("DownloadToFileWithChecksum initial call failed: %v", err)
	}

	if hits != 1 {
		t.Fatalf("expected 1 HTTP hit on initial download, got %d", hits)
	}

	cacheDir, err := config.GetAbsCachePath()
	if err != nil {
		t.Fatalf("GetAbsCachePath failed: %v", err)
	}
	filesDir := filepath.Join(cacheDir, cacheFilesSubdir)
	cacheFile := filepath.Join(filesDir, checksum+"_"+cacheBase)

	// Corrupt cache file.
	if err := os.WriteFile(cacheFile, []byte("bad"), 0o644); err != nil {
		t.Fatalf("failed to corrupt cache file: %v", err)
	}

	// Next call should see checksum mismatch and refetch from server.
	dst2 := filepath.Join(t.TempDir(), "out2.bin")
	if err := DownloadToFileWithChecksum(ctx, server.URL+"/file", dst2, checksum, cacheBase); err != nil {
		t.Fatalf("DownloadToFileWithChecksum with corrupt cache failed: %v", err)
	}

	if hits != 2 {
		t.Fatalf("expected second HTTP hit after corrupt cache, got %d", hits)
	}

	// Cache file should now contain good content again.
	cacheSum, err := helpers.GetSHA256OfFile(cacheFile)
	if err != nil {
		t.Fatalf("GetSHA256OfFile cache after refresh failed: %v", err)
	}
	if cacheSum != checksum {
		t.Fatalf("refreshed cache checksum mismatch: got %s, want %s", cacheSum, checksum)
	}

	out2Sum, err := helpers.GetSHA256OfFile(dst2)
	if err != nil {
		t.Fatalf("GetSHA256OfFile dst2 failed: %v", err)
	}
	if out2Sum != checksum {
		t.Fatalf("dst2 checksum mismatch after refresh: got %s, want %s", out2Sum, checksum)
	}
}

func TestDownloadToFileInlineChecksumUsesHelper(t *testing.T) {
	ctx := context.Background()
	cacheRoot := t.TempDir()
	orig := config.CommonOptions.CachePath
	config.CommonOptions.CachePath = cacheRoot
	t.Cleanup(func() { config.CommonOptions.CachePath = orig })

	content := []byte("inline-checksum")
	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])

	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if _, err := w.Write(content); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	dst := filepath.Join(t.TempDir(), "out.bin")
	srcWithChecksum := server.URL + "/file@" + checksum

	if err := DownloadToFile(ctx, srcWithChecksum, dst); err != nil {
		t.Fatalf("DownloadToFile with inline checksum failed: %v", err)
	}

	if hits != 1 {
		t.Fatalf("expected 1 HTTP hit for inline checksum download, got %d", hits)
	}

	outSum, err := helpers.GetSHA256OfFile(dst)
	if err != nil {
		t.Fatalf("GetSHA256OfFile dst failed: %v", err)
	}
	if outSum != checksum {
		t.Fatalf("dst checksum mismatch for inline checksum: got %s, want %s", outSum, checksum)
	}

	cacheDir, err := config.GetAbsCachePath()
	if err != nil {
		t.Fatalf("GetAbsCachePath failed: %v", err)
	}
	filesDir := filepath.Join(cacheDir, cacheFilesSubdir)
	entries, err := os.ReadDir(filesDir)
	if err != nil {
		t.Fatalf("ReadDir filesDir failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected at least one cached file for inline checksum download")
	}

	// Ensure at least one cache entry starts with the checksum prefix.
	found := false
	for _, e := range entries {
		name := e.Name()
		if len(name) >= len(checksum) && name[:len(checksum)] == checksum {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a cache file starting with checksum prefix %s", checksum)
	}
}

// TestDownloadToFileWithChecksumConcurrentCacheWrites guards against the cache-write
// race: multiple downloads of the same checksum used to write straight to the final
// cachePath via os.Create, so concurrent writers could interleave and corrupt the
// shared cache entry. updateCache now writes to a temp file and renames it into place,
// which is atomic, so every writer's copy is self-consistent. Run with -race to also
// catch any data race in the write path itself.
func TestDownloadToFileWithChecksumConcurrentCacheWrites(t *testing.T) {
	ctx := context.Background()
	cacheRoot := t.TempDir()
	orig := config.CommonOptions.CachePath
	config.CommonOptions.CachePath = cacheRoot
	t.Cleanup(func() { config.CommonOptions.CachePath = orig })

	content := []byte("concurrent-content")
	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write(content); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			dst := filepath.Join(t.TempDir(), "out.bin")
			errs[i] = DownloadToFileWithChecksum(ctx, server.URL+"/file", dst, checksum, "concurrent.bin")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: DownloadToFileWithChecksum failed: %v", i, err)
		}
	}

	cacheDir, err := config.GetAbsCachePath()
	if err != nil {
		t.Fatalf("GetAbsCachePath failed: %v", err)
	}
	cacheFile := filepath.Join(cacheDir, cacheFilesSubdir, checksum+"_concurrent.bin")
	cachedSum, err := helpers.GetSHA256OfFile(cacheFile)
	if err != nil {
		t.Fatalf("GetSHA256OfFile on cache failed: %v", err)
	}
	if cachedSum != checksum {
		t.Fatalf("cache file checksum mismatch after concurrent writes: got %s, want %s", cachedSum, checksum)
	}
}

// withIsolatedCache points the shared cache at a directory this test owns and returns the
// files/ subdirectory inside it, where CachedArchive puts an entry.
func withIsolatedCache(t *testing.T) string {
	t.Helper()

	cacheRoot := t.TempDir()
	orig := config.CommonOptions.CachePath
	config.CommonOptions.CachePath = cacheRoot
	t.Cleanup(func() { config.CommonOptions.CachePath = orig })

	return filepath.Join(cacheRoot, cacheFilesSubdir)
}

// serveCounted serves content and counts how many requests it answered.
func serveCounted(t *testing.T, content []byte, hits *int) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*hits++
		if _, err := w.Write(content); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func TestCachedArchiveServesSecondCallFromCache(t *testing.T) {
	ctx := context.Background()
	cacheFiles := withIsolatedCache(t)

	content := []byte("archive-bytes")
	var hits int
	server := serveCounted(t, content, &hits)

	first, err := CachedArchive(ctx, server.URL+"/rke2.tar.gz", "rke2.tar.gz")
	if err != nil {
		t.Fatalf("CachedArchive first call failed: %v", err)
	}
	if hits != 1 {
		t.Fatalf("expected 1 request on a cold cache, got %d", hits)
	}

	second, err := CachedArchive(ctx, server.URL+"/rke2.tar.gz", "rke2.tar.gz")
	if err != nil {
		t.Fatalf("CachedArchive second call failed: %v", err)
	}
	if hits != 1 {
		t.Fatalf("expected the second call to be served from the cache, got %d requests", hits)
	}
	if first != second {
		t.Fatalf("expected both calls to return the same cache path, got %q and %q", first, second)
	}

	got, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("unable to read the cached archive: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("cached archive content mismatch: got %q, want %q", got, content)
	}

	// The entry lands in the shared files/ directory, beside the shasum-keyed ones.
	entries, err := os.ReadDir(cacheFiles)
	if err != nil {
		t.Fatalf("unable to read the cache directory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 cache entry, got %d", len(entries))
	}
	if filepath.Dir(first) != cacheFiles {
		t.Fatalf("expected the entry inside %q, got %q", cacheFiles, first)
	}
}

// TestCachedArchiveKeysDistinctURLsApart is the reason the key is a digest of the whole URL
// rather than the file name: every rke2 and etcd release ends in the same base name, so keying
// on that would serve one version's archive for another's.
func TestCachedArchiveKeysDistinctURLsApart(t *testing.T) {
	ctx := context.Background()
	withIsolatedCache(t)

	firstContent := []byte("from the first release")
	secondContent := []byte("from the second release")

	var firstHits, secondHits int
	firstServer := serveCounted(t, firstContent, &firstHits)
	secondServer := serveCounted(t, secondContent, &secondHits)

	firstPath, err := CachedArchive(ctx, firstServer.URL+"/v1/archive.tar.gz", "archive.tar.gz")
	if err != nil {
		t.Fatalf("CachedArchive for the first URL failed: %v", err)
	}
	secondPath, err := CachedArchive(ctx, secondServer.URL+"/v2/archive.tar.gz", "archive.tar.gz")
	if err != nil {
		t.Fatalf("CachedArchive for the second URL failed: %v", err)
	}

	if firstPath == secondPath {
		t.Fatalf("two different URLs sharing a base name must not share a cache entry, both got %q", firstPath)
	}

	// Coming back to the first URL must serve the first archive, not whichever was written last.
	againPath, err := CachedArchive(ctx, firstServer.URL+"/v1/archive.tar.gz", "archive.tar.gz")
	if err != nil {
		t.Fatalf("CachedArchive returning to the first URL failed: %v", err)
	}
	if firstHits != 1 {
		t.Fatalf("expected the first URL to be downloaded once, got %d requests", firstHits)
	}
	got, err := os.ReadFile(againPath)
	if err != nil {
		t.Fatalf("unable to read the cached archive: %v", err)
	}
	if string(got) != string(firstContent) {
		t.Fatalf("cache served the wrong archive: got %q, want %q", got, firstContent)
	}
}

// TestCachedArchiveFailedDownloadCachesNothing matters because an entry is served on its key
// alone, with no digest to check it against: a truncated or empty one left behind would be
// handed to every later build as though it were the archive.
func TestCachedArchiveFailedDownloadCachesNothing(t *testing.T) {
	ctx := context.Background()
	cacheFiles := withIsolatedCache(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	if _, err := CachedArchive(ctx, server.URL+"/missing.tar.gz", "missing.tar.gz"); err == nil {
		t.Fatal("expected CachedArchive to fail on a 404")
	}

	entries, err := os.ReadDir(cacheFiles)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatalf("unable to read the cache directory: %v", err)
		}
		return
	}
	if len(entries) != 0 {
		t.Fatalf("a failed download must leave nothing cached, found %d entries", len(entries))
	}
}

// A member extracted out of an archive is cached under the archive's own content digest, so a
// rebuild takes it from there rather than fetching and decompressing the archive again.
func TestMemberCacheRoundTrip(t *testing.T) {
	previous := config.CommonOptions.CachePath
	config.CommonOptions.CachePath = t.TempDir()
	t.Cleanup(func() { config.CommonOptions.CachePath = previous })

	ctx := context.Background()
	const digest = "sha256:faa2577be9b54e45ede4db5ad1d13489c0a51229c00569c2f101ae49cdc06b88"
	const member = "bin/rke2"
	const body = "engine binary\n"

	if _, ok := CachedMember(ctx, digest, member); ok {
		t.Fatal("an empty cache reported a member as held")
	}

	src := filepath.Join(t.TempDir(), "rke2")
	if err := os.WriteFile(src, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the source: %v", err)
	}
	CacheMember(ctx, digest, member, src)

	cached, ok := CachedMember(ctx, digest, member)
	if !ok {
		t.Fatal("the member was not cached")
	}
	got, err := os.ReadFile(cached)
	if err != nil {
		t.Fatalf("reading the cached member: %v", err)
	}
	if string(got) != body {
		t.Errorf("cached %q, want %q", got, body)
	}
	// The file name carries the member's base name, so a cache directory is readable.
	if filepath.Base(cached) == "" || !strings.HasSuffix(filepath.Base(cached), "_rke2") {
		t.Errorf("cache entry %q does not name the member it holds", filepath.Base(cached))
	}
}

// The key is the archive and the member together: a different archive, or a different path out
// of the same one, is a different entry. Getting this wrong would serve one file's bytes for
// another, which no later check would catch for an entry declaring no shasum.
func TestMemberCacheKeyIsArchivePlusMember(t *testing.T) {
	previous := config.CommonOptions.CachePath
	config.CommonOptions.CachePath = t.TempDir()
	t.Cleanup(func() { config.CommonOptions.CachePath = previous })

	ctx := context.Background()
	const amd64Archive = "sha256:faa2577be9b54e45ede4db5ad1d13489c0a51229c00569c2f101ae49cdc06b88"
	const arm64Archive = "sha256:a59ee55a8a68ed5ae8af39e91cf89314f431178f456b3f63d3f87638180b417a"

	paths := map[string]string{
		"amd64/rke2":    memberCachePath(ctx, amd64Archive, "bin/rke2"),
		"arm64/rke2":    memberCachePath(ctx, arm64Archive, "bin/rke2"),
		"amd64/killall": memberCachePath(ctx, amd64Archive, "bin/rke2-killall.sh"),
	}
	seen := map[string]string{}
	for name, p := range paths {
		if p == "" {
			t.Fatalf("%s produced no cache path", name)
		}
		if other, clash := seen[p]; clash {
			t.Errorf("%s and %s share a cache entry", name, other)
		}
		seen[p] = name
	}

	// Nothing to key on means no caching rather than a shared key.
	if got := memberCachePath(ctx, "", "bin/rke2"); got != "" {
		t.Errorf("an archive with no digest produced the cache path %q", got)
	}
	if got := memberCachePath(ctx, amd64Archive, ""); got != "" {
		t.Errorf("an empty member produced the cache path %q", got)
	}
}
