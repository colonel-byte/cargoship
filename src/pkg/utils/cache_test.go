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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveCachePathExplicit(t *testing.T) {
	path := "/tmp/custom-cache"
	got, err := ResolveCachePath(path)
	if err != nil {
		t.Fatalf("ResolveCachePath explicit failed: %v", err)
	}
	if got != path {
		t.Fatalf("ResolveCachePath explicit: got %q, want %q", got, path)
	}
}

func TestResolveCachePathDefaultUsesUserCacheDir(t *testing.T) {
	got, err := ResolveCachePath("")
	if err != nil {
		t.Fatalf("ResolveCachePath default failed: %v", err)
	}
	userCache, err := os.UserCacheDir()
	if err != nil {
		t.Fatalf("os.UserCacheDir failed: %v", err)
	}
	wantPrefix := filepath.Join(userCache, CacheDir)
	if got != wantPrefix {
		t.Fatalf("ResolveCachePath default: got %q, want %q", got, wantPrefix)
	}
}

func TestDownloadToCacheWithSHA256(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("test content payload"))
	}))
	defer ts.Close()

	// sha256 of "test content payload"
	const validSha = "ece3930c9301a5dfa3754d55161558076c4a3fa898f6fd9374c54138853ad3ea"

	// Success case
	target, err := DownloadToCache(ts.URL, "test/file.txt", validSha)
	require.NoError(t, err)
	require.FileExists(t, target)

	// Second call uses cache
	target2, err := DownloadToCache(ts.URL, "test/file.txt", validSha)
	require.NoError(t, err)
	require.Equal(t, target, target2)

	// Mismatch case
	_, err = DownloadToCache(ts.URL, "test/file2.txt", "invalidsha256hash")
	require.ErrorContains(t, err, "checksum mismatch")
}
