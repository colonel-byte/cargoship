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
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
