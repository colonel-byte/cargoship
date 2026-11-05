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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

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

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("test content payload")); err != nil {
			t.Errorf("writing test response: %v", err)
		}
	}))
	defer ts.Close()

	// sha256 of "test content payload"
	const validSha = "ece3930c9301a5dfa3754d55161558076c4a3fa898f6fd9374c54138853ad3ea"

	// Success case
	target, err := DownloadToCache(t.Context(), ts.URL, "test/file.txt", validSha)
	require.NoError(t, err)
	require.FileExists(t, target)

	// Second call uses cache
	target2, err := DownloadToCache(t.Context(), ts.URL, "test/file.txt", validSha)
	require.NoError(t, err)
	require.Equal(t, target, target2)

	// Mismatch case
	_, err = DownloadToCache(t.Context(), ts.URL, "test/file2.txt", "invalidsha256hash")
	require.ErrorContains(t, err, "checksum mismatch")
}

// A cache entry whose contents no longer match the checksum is replaced rather than handed back,
// so a truncated or tampered file does not pin every later run to the bad copy.
func TestDownloadToCacheReplacesCorruptedEntry(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("test content payload")); err != nil {
			t.Errorf("writing test response: %v", err)
		}
	}))
	defer ts.Close()

	// sha256 of "test content payload"
	const validSha = "ece3930c9301a5dfa3754d55161558076c4a3fa898f6fd9374c54138853ad3ea"

	target, err := DownloadToCache(t.Context(), ts.URL, "test/file.txt", validSha)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(target, []byte("corrupted"), 0o644))

	got, err := DownloadToCache(t.Context(), ts.URL, "test/file.txt", validSha)
	require.NoError(t, err)
	require.Equal(t, target, got)

	content, err := os.ReadFile(got)
	require.NoError(t, err)
	require.Equal(t, "test content payload", string(content))
}

// A server that accepts the connection and then goes silent must not hang the
// caller. The context is the lever for that, so check it actually reaches the
// request rather than only being accepted as an argument.
func TestDownloadToCacheHonoursContextCancellation(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := DownloadToCache(ctx, ts.URL, "hang/file.txt")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 5*time.Second)
}

// writeCacheFixture builds a cache root holding one entry per area, with the flat entries aged
// so an age window can be tested. It returns the root.
func writeCacheFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	old := time.Now().Add(-60 * 24 * time.Hour)

	write := func(rel string, size int, aged bool) string {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("unable to create %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
			t.Fatalf("unable to write %s: %v", path, err)
		}
		if aged {
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatalf("unable to age %s: %v", path, err)
			}
		}
		return path
	}

	write("files/url-aaa_etcd.tar.gz", 2000, true)
	write("files/deadbeef_k3s.rpm", 1000, true)
	write("files/url-fresh_new.tar.gz", 500, false)
	write("examples/asset.txt", 100, true)
	write("images/index.json", 50, true)
	write("images/blobs/sha256/abc", 3000, true)
	write("logs/run.log", 10, true)
	write("microvm/rocky.qcow2", 20, true)

	return root
}

func TestCacheUsageClassifiesAreas(t *testing.T) {
	root := writeCacheFixture(t)

	areas, err := CacheUsage(root)
	if err != nil {
		t.Fatalf("CacheUsage failed: %v", err)
	}

	kinds := map[string]CacheAreaKind{}
	sizes := map[string]int64{}
	for _, a := range areas {
		kinds[a.Name] = a.Kind
		sizes[a.Name] = a.Size
	}

	want := map[string]CacheAreaKind{
		"files":    CacheAreaFlat,
		"examples": CacheAreaFlat,
		"images":   CacheAreaLayout,
		"logs":     CacheAreaRecord,
		"microvm":  CacheAreaRecord,
	}
	for name, kind := range want {
		if kinds[name] != kind {
			t.Errorf("%s classified as %v, want %v", name, kinds[name], kind)
		}
	}

	// Sizes are summed across subdirectories, so the image layout's blob counts.
	if got := sizes["images"]; got != 3050 {
		t.Errorf("images size = %d, want 3050", got)
	}
	if got := sizes["files"]; got != 3500 {
		t.Errorf("files size = %d, want 3500", got)
	}
}

// TestCacheUsageTreatsAnUnknownAreaAsARecord is the safe default: a directory a later version
// writes must not be deleted by an older binary that has never heard of it.
func TestCacheUsageTreatsAnUnknownAreaAsARecord(t *testing.T) {
	root := writeCacheFixture(t)
	if err := os.MkdirAll(filepath.Join(root, "something-new"), 0o755); err != nil {
		t.Fatalf("unable to create the directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "something-new", "keep"), []byte("x"), 0o644); err != nil {
		t.Fatalf("unable to write: %v", err)
	}

	if _, err := PruneCache(context.Background(), root, PruneOptions{}); err != nil {
		t.Fatalf("PruneCache failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "something-new", "keep")); err != nil {
		t.Fatalf("an unrecognised area must be left alone, got %v", err)
	}
}

func TestPruneCacheRemovesEveryFetchableArea(t *testing.T) {
	root := writeCacheFixture(t)

	pruned, err := PruneCache(context.Background(), root, PruneOptions{})
	if err != nil {
		t.Fatalf("PruneCache failed: %v", err)
	}
	// files 3500 + examples 100 + images 3050
	if pruned.Size != 6650 {
		t.Errorf("pruned size = %d, want 6650", pruned.Size)
	}
	if len(pruned.SkippedLayouts) != 0 {
		t.Errorf("a full prune must skip nothing, got %v", pruned.SkippedLayouts)
	}

	for _, gone := range []string{"files/url-aaa_etcd.tar.gz", "files/url-fresh_new.tar.gz", "images/index.json", "examples/asset.txt"} {
		if _, err := os.Stat(filepath.Join(root, gone)); !os.IsNotExist(err) {
			t.Errorf("%s should be gone, stat returned %v", gone, err)
		}
	}
	for _, kept := range []string{"logs/run.log", "microvm/rocky.qcow2"} {
		if _, err := os.Stat(filepath.Join(root, kept)); err != nil {
			t.Errorf("%s must be kept, got %v", kept, err)
		}
	}
}

// TestPruneCacheOlderThanKeepsTheWorkingSet is the behaviour that makes the command usable on a
// machine still in use, and the reason an image layout is excluded from an age window.
func TestPruneCacheOlderThanKeepsTheWorkingSet(t *testing.T) {
	root := writeCacheFixture(t)

	pruned, err := PruneCache(context.Background(), root, PruneOptions{OlderThan: 30 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("PruneCache failed: %v", err)
	}

	// The two aged files plus the aged example asset; the fresh file stays.
	if pruned.Size != 3100 {
		t.Errorf("pruned size = %d, want 3100", pruned.Size)
	}
	if _, err := os.Stat(filepath.Join(root, "files/url-fresh_new.tar.gz")); err != nil {
		t.Errorf("an entry inside the window must be kept, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "files/url-aaa_etcd.tar.gz")); !os.IsNotExist(err) {
		t.Errorf("an entry outside the window should be gone, stat returned %v", err)
	}

	// The layout is reported rather than half-removed: pruning blobs by age would leave
	// index.json naming blobs that are gone.
	if len(pruned.SkippedLayouts) != 1 || pruned.SkippedLayouts[0] != "images" {
		t.Errorf("SkippedLayouts = %v, want [images]", pruned.SkippedLayouts)
	}
	if _, err := os.Stat(filepath.Join(root, "images/blobs/sha256/abc")); err != nil {
		t.Errorf("the image layout must be untouched by an age window, got %v", err)
	}
}

func TestPruneCacheDryRunRemovesNothing(t *testing.T) {
	root := writeCacheFixture(t)

	pruned, err := PruneCache(context.Background(), root, PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("PruneCache failed: %v", err)
	}
	if pruned.Size != 6650 {
		t.Errorf("a dry run must still report what would go, size = %d, want 6650", pruned.Size)
	}

	for _, kept := range []string{"files/url-aaa_etcd.tar.gz", "images/index.json", "examples/asset.txt", "logs/run.log"} {
		if _, err := os.Stat(filepath.Join(root, kept)); err != nil {
			t.Errorf("a dry run must remove nothing, %s returned %v", kept, err)
		}
	}
}

func TestPruneCacheOnAnAbsentRootIsNotAnError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "never-created")

	pruned, err := PruneCache(context.Background(), root, PruneOptions{})
	if err != nil {
		t.Fatalf("PruneCache on an absent cache failed: %v", err)
	}
	if pruned.Entries != 0 || pruned.Size != 0 {
		t.Errorf("an absent cache must prune nothing, got %+v", pruned)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		name string
		in   int64
		want string
	}{
		{
			name: "bytes",
			in:   512,
			want: "512 B",
		},
		{
			name: "kibibytes",
			in:   2048,
			want: "2.00 KiB",
		},
		{
			name: "mebibytes",
			in:   5 * 1024 * 1024,
			want: "5.00 MiB",
		},
		{
			name: "gibibytes",
			in:   3 * 1024 * 1024 * 1024,
			want: "3.00 GiB",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HumanBytes(tc.in); got != tc.want {
				t.Errorf("HumanBytes(%d) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
