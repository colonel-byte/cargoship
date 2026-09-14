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

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	rke2Repo  = "https://github.com/rancher/rke2"
	rke2Tag   = "v1.36.4+rke2r1"
	coreAsset = "rke2-images-core.linux-amd64.txt"
)

// assetURL is the URL the cache keys an entry against, spelled the way fetchReleaseLines
// spells it.
func assetURL(repoURL, tagURL, asset string) string {
	return repoURL + "/releases/download/" + tagURL + "/" + asset
}

// A cached asset is read back as it was stored, so a second render of a version costs no
// request.
func TestReleaseLinesCacheRoundTrip(t *testing.T) {
	c := &releaseLinesCache{dir: t.TempDir()}
	url := assetURL(rke2Repo, rke2Tag, coreAsset)
	want := []string{"rancher/rke2-runtime:v1.36.4", "rancher/pause:3.9"}

	c.store(url, want)

	got, ok := c.lookup(url)
	require.True(t, ok, "an asset just stored is a hit")
	require.Equal(t, want, got)
}

// The URL an entry was fetched from is part of what makes it usable: an entry recorded
// against another URL is a miss, not an answer.
func TestReleaseLinesCacheURLMismatchIsMiss(t *testing.T) {
	c := &releaseLinesCache{dir: t.TempDir()}
	url := assetURL(rke2Repo, rke2Tag, coreAsset)
	c.store(url, []string{"rancher/pause:3.9"})

	path := c.entryPath(url)
	require.NoError(t, os.WriteFile(path, []byte(`{"url":"https://elsewhere.example.com/x","lines":["x"]}`), 0o644))

	_, ok := c.lookup(url)
	require.False(t, ok, "an entry fetched from another URL cannot answer for this one")
}

// Two repositories serving the same asset name on the same tag cache separately.
func TestReleaseLinesCacheSeparatesRepos(t *testing.T) {
	c := &releaseLinesCache{dir: t.TempDir()}
	mirror := "https://mirror.example.com/rke2"

	c.store(assetURL(rke2Repo, rke2Tag, coreAsset), []string{"upstream"})
	c.store(assetURL(mirror, rke2Tag, coreAsset), []string{"mirror"})

	got, ok := c.lookup(assetURL(rke2Repo, rke2Tag, coreAsset))
	require.True(t, ok)
	require.Equal(t, []string{"upstream"}, got)
}

// A truncated entry is a miss, so an interrupted run costs a refetch rather than handing back
// a partial image list.
func TestReleaseLinesCacheTruncatedEntryIsMiss(t *testing.T) {
	c := &releaseLinesCache{dir: t.TempDir()}
	url := assetURL(rke2Repo, rke2Tag, coreAsset)
	c.store(url, []string{"rancher/pause:3.9"})

	path := c.entryPath(url)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data[:len(data)/2], 0o644))

	_, ok := c.lookup(url)
	require.False(t, ok, "half an entry is not an asset")
}

// An empty entry is a miss too: an asset that produced no lines should be fetched again
// rather than remembered as empty.
func TestReleaseLinesCacheEmptyIsNotStored(t *testing.T) {
	c := &releaseLinesCache{dir: t.TempDir()}
	url := assetURL(rke2Repo, rke2Tag, coreAsset)

	c.store(url, nil)

	_, ok := c.lookup(url)
	require.False(t, ok)
}

// A cache with nowhere to write is off rather than broken: every lookup misses and storing is
// a no-op, which is what the targets did before there was a cache.
func TestReleaseLinesCacheOffIsANoOp(t *testing.T) {
	c := &releaseLinesCache{}
	url := assetURL(rke2Repo, rke2Tag, coreAsset)

	require.Empty(t, c.entryPath(url))
	c.store(url, []string{"rancher/pause:3.9"})

	_, ok := c.lookup(url)
	require.False(t, ok)
}

// An asset URL is one flat file name, the way an image reference is one tarball name.
func TestEntryName(t *testing.T) {
	require.Equal(t,
		"github.com_rancher_rke2_releases_download_v1.36.4+rke2r1_rke2-images-core.linux-amd64.txt",
		entryName(assetURL(rke2Repo, rke2Tag, coreAsset)))
	require.NotEqual(t,
		entryName(assetURL(rke2Repo, rke2Tag, coreAsset)),
		entryName(assetURL("https://mirror.example.com/rke2", rke2Tag, coreAsset)),
		"two repositories serving the same asset cache apart")
}

// Nothing in a URL can name a directory of its own or walk out of the cache.
func TestEntryPathStaysInsideCacheDir(t *testing.T) {
	dir := t.TempDir()
	c := &releaseLinesCache{dir: dir}

	path := c.entryPath("https://github.com/../../../etc/passwd/releases/download/../../x.txt")

	require.Equal(t, dir, filepath.Dir(path), "the entry is written directly in the cache directory")
	require.True(t, strings.HasPrefix(filepath.Clean(path), filepath.Clean(dir)+string(filepath.Separator)),
		"entry path %q escaped the cache directory %q", path, dir)
}

// A leading ~ resolves against the home directory; anything else is left as written.
func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	got, err := expandHome("~/.cache/cargoship")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".cache/cargoship"), got)

	got, err = expandHome("~")
	require.NoError(t, err)
	require.Equal(t, home, got)

	for _, path := range []string{"/var/cache/cargoship", "relative/cache", "~notahome/cache", ""} {
		got, err := expandHome(path)
		require.NoError(t, err)
		require.Equal(t, path, got, "only a leading ~ is expanded")
	}
}

// Setting CARGOSHIP_EXAMPLES_NO_CACHE turns the cache off for the run.
func TestExampleCacheDirOff(t *testing.T) {
	t.Setenv(exampleCacheOff, "1")

	dir, err := exampleCacheDir()
	require.NoError(t, err)
	require.Empty(t, dir)
}

// An interrupted write leaves the entry that was already there, not a truncated one.
func TestWriteFileAtomicLeavesNoPartialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "entry.json")

	require.NoError(t, writeFileAtomic(path, []byte("first")))
	require.NoError(t, writeFileAtomic(path, []byte("second")))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "second", string(data))

	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temporary file is left behind")
}

// A release's text asset is its non-empty lines, in file order.
func TestSplitReleaseLines(t *testing.T) {
	got := splitReleaseLines([]byte("  rancher/pause:3.9  \n\nrancher/rke2-runtime:v1.36.4\n   \n"))
	require.Equal(t, []string{"rancher/pause:3.9", "rancher/rke2-runtime:v1.36.4"}, got)
	require.Nil(t, splitReleaseLines([]byte("\n  \n")))
}
