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
	"sync"
	"testing"

	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/pkg/helpers"
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
