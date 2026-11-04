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

// Package utils is for commonly used functions.
package utils

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// CacheDir is the subdirectory name used under the OS user cache directory
// for cargoship's shared cache.
const CacheDir = "cargoship"

// responseHeaderTimeout bounds the wait for a server to start replying. It is a
// header timeout rather than a whole-request timeout on purpose: a server that
// accepts the connection and then goes silent must not hang the caller forever,
// but a large artifact on a slow link must not be cut off partway through
// either. Cancelling a download that has started is the caller's job, via ctx.
const responseHeaderTimeout = 30 * time.Second

var cacheHTTPClient = &http.Client{Transport: cacheTransport()}

// cacheTransport clones the default transport so proxy and TLS settings from the
// environment still apply, and adds the header timeout described above.
func cacheTransport() http.RoundTripper {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		// Unreachable with the stdlib default. A process that replaced it keeps
		// whatever it installed rather than losing proxy support to this clone.
		return http.DefaultTransport
	}
	clone := t.Clone()
	clone.ResponseHeaderTimeout = responseHeaderTimeout
	return clone
}

// DownloadToCache downloads a URL into the given relative path under cargoship's cache directory if not already cached.
// If expectedSha256 is provided (optional), it validates that the cached or downloaded file matches the sha256 checksum.
func DownloadToCache(ctx context.Context, url, relPath string, expectedSha256 ...string) (string, error) {
	l := logger.From(ctx)

	cacheRoot, err := ResolveCachePath("")
	if err != nil {
		return "", err
	}
	target := filepath.Join(cacheRoot, relPath)
	var expected string
	if len(expectedSha256) > 0 {
		expected = strings.TrimSpace(expectedSha256[0])
	}

	if cachedFileUsable(ctx, target, expected) {
		l.Info("found file in cache, using cached copy", "target", target)
		return target, nil
	}

	l.Info("file not in cache, downloading", "url", url, "target", target)

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request for %s: %w", url, err)
	}
	resp, err := cacheHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body, nothing to do with a close error

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s: status %s", url, resp.Status)
	}

	tmpFile := target + ".tmp"
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}

	hasher := sha256.New()
	writer := io.MultiWriter(f, hasher)

	if _, err := io.Copy(writer, resp.Body); err != nil {
		f.Close() //nolint:errcheck // the copy failure below is the error worth reporting
		removePartialDownload(ctx, tmpFile)
		return "", fmt.Errorf("writing cache file: %w", err)
	}
	// The close is checked rather than deferred: a write that only fails on flush would
	// otherwise be renamed into the cache as a complete file.
	if err := f.Close(); err != nil {
		removePartialDownload(ctx, tmpFile)
		return "", fmt.Errorf("writing cache file: %w", err)
	}

	if expected != "" {
		actualSha := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(actualSha, expected) {
			removePartialDownload(ctx, tmpFile)
			return "", fmt.Errorf("checksum mismatch for %s: expected %s, got %s", url, expected, actualSha)
		}
	}

	if err := os.Rename(tmpFile, target); err != nil {
		return "", fmt.Errorf("saving cached file: %w", err)
	}
	return target, nil
}

// cachedFileUsable reports whether target is already in the cache and is the file the caller
// asked for. An entry that cannot be checksummed or does not match is removed on the way out, so
// the download that follows replaces it rather than every later run tripping over the same bad
// entry.
func cachedFileUsable(ctx context.Context, target, expected string) bool {
	l := logger.From(ctx)

	if _, err := os.Stat(target); err != nil {
		return false
	}
	if expected == "" {
		return true
	}

	matches, err := verifyFileSHA256(target, expected)
	switch {
	case err != nil:
		l.Warn("unable to checksum cached file, re-downloading", "target", target, "error", err)
	case matches:
		return true
	default:
		l.Warn("cached file checksum mismatch, re-downloading", "target", target, "expected", expected)
	}

	if err := os.Remove(target); err != nil {
		l.Warn("unable to remove unusable cached file", "target", target, "error", err)
	}
	return false
}

// removePartialDownload drops a download that is already being abandoned, so a failure to remove
// it is logged rather than returned in place of the error that caused the abandonment.
func removePartialDownload(ctx context.Context, path string) {
	if err := os.Remove(path); err != nil {
		logger.From(ctx).Warn("unable to remove partial download", "target", path, "error", err)
	}
}

func verifyFileSHA256(path, expected string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close() //nolint:errcheck // read-only, nothing to do with a close error

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	return strings.EqualFold(actual, expected), nil
}

// ResolveCachePath returns cachePath if non-empty, otherwise falls back to
// filepath.Join(os.UserCacheDir(), "cargoship") which respects XDG_CACHE_HOME on Linux.
func ResolveCachePath(cachePath string) (string, error) {
	if cachePath != "" {
		return cachePath, nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("unable to determine cache directory: %w", err)
	}
	return filepath.Join(cacheDir, CacheDir), nil
}

// Cache subdirectories holding content a build can fetch again. The names are duplicated here
// rather than imported: cacheImagesSubdir is coci/layers.ImageCacheDirectory and
// cacheExamplesSubdir is the examples generator's asset cache, and importing either package from
// here would invert the dependency.
const (
	cacheImagesSubdir   = "images"
	cacheExamplesSubdir = "examples"
)

// CacheAreaKind says whether a cache directory's entries can be removed individually.
type CacheAreaKind int

const (
	// CacheAreaFlat holds independently-keyed entries, each complete on its own, so any
	// subset can be removed and the rest still serve hits.
	CacheAreaFlat CacheAreaKind = iota
	// CacheAreaLayout holds an OCI image layout: an index naming blobs. Removing part of it
	// leaves the index pointing at blobs that are gone, so it goes all at once or not at all.
	CacheAreaLayout
	// CacheAreaRecord holds records of runs rather than fetched content. Nothing regenerates
	// one, so pruning never touches it.
	CacheAreaRecord
)

// CacheArea describes one subdirectory of the cache root.
type CacheArea struct {
	Name string
	Kind CacheAreaKind
	Path string
	// Size is the total bytes beneath Path, and Entries the number of top-level entries.
	Size    int64
	Entries int
}

// cacheAreaKinds classifies the subdirectories cargoship writes under the cache root. A directory
// found there and not named here is reported by CacheUsage and never pruned, which is the safe
// default for one this version does not know about.
var cacheAreaKinds = map[string]CacheAreaKind{
	cacheFilesSubdir:    CacheAreaFlat,
	cacheExamplesSubdir: CacheAreaFlat,
	cacheImagesSubdir:   CacheAreaLayout,
	"logs":              CacheAreaRecord,
	"e2e-logs":          CacheAreaRecord,
	// microvm holds the VM base images the local dev fleet boots. They are fetched by
	// `mage dev:vmImage` rather than by a package build, and re-fetching one is a multi-
	// gigabyte download, so it is left to that tooling rather than pruned from here.
	"microvm": CacheAreaRecord,
}

// CacheUsage reports every subdirectory under the cache root with its size, so an operator can
// see what is there before removing any of it.
func CacheUsage(cachePath string) ([]CacheArea, error) {
	root, err := ResolveCachePath(cachePath)
	if err != nil {
		return nil, err
	}

	listing, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("unable to read the cache directory: %w", err)
	}

	dirs := make([]CacheArea, 0, len(listing))
	for _, entry := range listing {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		size, count, err := dirUsage(path)
		if err != nil {
			return nil, err
		}
		kind, known := cacheAreaKinds[entry.Name()]
		if !known {
			kind = CacheAreaRecord
		}
		dirs = append(dirs, CacheArea{
			Name:    entry.Name(),
			Kind:    kind,
			Path:    path,
			Size:    size,
			Entries: count,
		})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	return dirs, nil
}

// PruneOptions control what CacheUsage's results PruneCache removes.
type PruneOptions struct {
	// OlderThan keeps any entry modified within this window. Zero removes everything
	// prunable, which is the only way an image layout is removed -- see CacheAreaLayout.
	OlderThan time.Duration
	// DryRun reports what would be removed without removing it.
	DryRun bool
}

// Pruned is what PruneCache removed, or would have removed under DryRun.
type Pruned struct {
	Dirs    []CacheArea
	Size    int64
	Entries int
	// SkippedLayouts names the image layouts left alone because OlderThan was set.
	SkippedLayouts []string
}

// PruneCache removes cached content from the cache root and reports what went.
//
// An age window prunes the flat caches entry by entry, which is what makes the command usable
// rather than nuclear: the working set of a definition under active edit stays, and the versions
// it has stopped naming go. An image layout cannot be pruned that way without breaking its index,
// so it is removed only by a full prune and is otherwise reported as skipped. Records -- the run
// logs, and the dev fleet's base images -- are never touched.
func PruneCache(ctx context.Context, cachePath string, opts PruneOptions) (Pruned, error) {
	l := logger.From(ctx)

	dirs, err := CacheUsage(cachePath)
	if err != nil {
		return Pruned{}, err
	}

	var result Pruned
	cutoff := time.Now().Add(-opts.OlderThan)

	for _, dir := range dirs {
		switch dir.Kind {
		case CacheAreaRecord:
			continue
		case CacheAreaLayout:
			if opts.OlderThan > 0 {
				result.SkippedLayouts = append(result.SkippedLayouts, dir.Name)
				continue
			}
			if !opts.DryRun {
				if err := os.RemoveAll(dir.Path); err != nil {
					return result, fmt.Errorf("unable to remove %s: %w", dir.Path, err)
				}
			}
			l.Debug("pruning cache directory", "path", dir.Path, "bytes", dir.Size, "dryRun", opts.DryRun)
			result.Dirs = append(result.Dirs, dir)
			result.Size += dir.Size
			result.Entries += dir.Entries
		case CacheAreaFlat:
			pruned, err := pruneFlatDir(ctx, dir, cutoff, opts)
			if err != nil {
				return result, err
			}
			if pruned.Entries == 0 {
				continue
			}
			result.Dirs = append(result.Dirs, pruned)
			result.Size += pruned.Size
			result.Entries += pruned.Entries
		}
	}

	return result, nil
}

// pruneFlatDir removes the top-level entries of a flat cache directory that fall outside the age
// window, and reports what went. An entry is kept on a stat failure rather than removed: the
// point of the window is to keep what might still be wanted.
func pruneFlatDir(ctx context.Context, dir CacheArea, cutoff time.Time, opts PruneOptions) (CacheArea, error) {
	l := logger.From(ctx)

	listing, err := os.ReadDir(dir.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CacheArea{Name: dir.Name, Kind: dir.Kind, Path: dir.Path}, nil
		}
		return CacheArea{}, fmt.Errorf("unable to read %s: %w", dir.Path, err)
	}

	out := CacheArea{Name: dir.Name, Kind: dir.Kind, Path: dir.Path}
	for _, entry := range listing {
		path := filepath.Join(dir.Path, entry.Name())
		if opts.OlderThan > 0 {
			info, err := entry.Info()
			if err != nil {
				l.Debug("unable to stat cache entry, keeping it", "path", path, "error", err)
				continue
			}
			if info.ModTime().After(cutoff) {
				continue
			}
		}
		size, count, err := dirUsage(path)
		if err != nil {
			return CacheArea{}, err
		}
		if !opts.DryRun {
			if err := os.RemoveAll(path); err != nil {
				return CacheArea{}, fmt.Errorf("unable to remove %s: %w", path, err)
			}
		}
		out.Size += size
		// A file counts as one entry; a directory counts as the one entry that was removed.
		if count == 0 {
			count = 1
		}
		out.Entries += count
	}
	return out, nil
}

// dirUsage returns the total bytes beneath path and the number of top-level entries in it. A
// plain file is one entry of its own size.
func dirUsage(path string) (int64, int, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("unable to stat %s: %w", path, err)
	}
	if !info.IsDir() {
		return info.Size(), 1, nil
	}

	var total int64
	err = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			// A cache entry removed by something else mid-walk is not an error worth
			// failing a prune over.
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		total += fi.Size()
		return nil
	})
	if err != nil {
		return 0, 0, fmt.Errorf("unable to measure %s: %w", path, err)
	}

	listing, err := os.ReadDir(path)
	if err != nil {
		return 0, 0, fmt.Errorf("unable to read %s: %w", path, err)
	}
	return total, len(listing), nil
}

// HumanBytes formats a byte count in the MiB-style units the rest of this project reports sizes
// in, so a prune's output can be compared with the binary size budget and with du.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.2f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.2f PiB", value/unit)
}
