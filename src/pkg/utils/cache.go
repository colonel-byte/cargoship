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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// CacheDir is the subdirectory name used under the OS user cache directory
// for cargoship's shared cache.
const CacheDir = "cargoship"

// DownloadToCache downloads a URL into the given relative path under cargoship's cache directory if not already cached.
// If expectedSha256 is provided (optional), it validates that the cached or downloaded file matches the sha256 checksum.
func DownloadToCache(url, relPath string, expectedSha256 ...string) (string, error) {
	cacheRoot, err := ResolveCachePath("")
	if err != nil {
		return "", err
	}
	target := filepath.Join(cacheRoot, relPath)
	var expected string
	if len(expectedSha256) > 0 {
		expected = strings.TrimSpace(expectedSha256[0])
	}

	if _, err := os.Stat(target); err == nil {
		if expected != "" {
			if matches, _ := verifyFileSHA256(target, expected); !matches {
				logger.Default().Warn("cached file checksum mismatch, re-downloading", "target", target, "expected", expected)
				_ = os.Remove(target)
			} else {
				logger.Default().Info("found file in cache, using cached copy", "target", target)
				return target, nil
			}
		} else {
			logger.Default().Info("found file in cache, using cached copy", "target", target)
			return target, nil
		}
	}

	logger.Default().Info("file not in cache, downloading", "url", url, "target", target)

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()

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
		f.Close()
		os.Remove(tmpFile)
		return "", fmt.Errorf("writing cache file: %w", err)
	}
	f.Close()

	if expected != "" {
		actualSha := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(actualSha, expected) {
			os.Remove(tmpFile)
			return "", fmt.Errorf("checksum mismatch for %s: expected %s, got %s", url, expected, actualSha)
		}
	}

	if err := os.Rename(tmpFile, target); err != nil {
		return "", fmt.Errorf("saving cached file: %w", err)
	}
	return target, nil
}

func verifyFileSHA256(path, expected string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

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
