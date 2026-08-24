// Copyright 2021 zarf authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from zarf:
// https://github.com/zarf-dev/zarf
//
// Modifications Copyright 2026 colonel-byte.
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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	retry "github.com/avast/retry-go/v4"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/pkg/helpers"
	zconfig "github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// retryAfterDuration is returned on a 429 so the custom DelayType can use it
// instead of stacking on top of the normal backoff.
type retryAfterDuration time.Duration

func (d retryAfterDuration) Error() string {
	return fmt.Sprintf("rate limited (HTTP 429), retry after %s", time.Duration(d))
}

func parseChecksum(src string) (string, string, error) {
	atSymbolCount := strings.Count(src, "@")
	var checksum string
	if atSymbolCount > 0 {
		parsed, err := url.Parse(src)
		if err != nil {
			return src, checksum, fmt.Errorf("unable to parse the URL: %s", src)
		}
		if atSymbolCount == 1 && parsed.User != nil {
			return src, checksum, nil
		}

		index := strings.LastIndex(src, "@")
		checksum = src[index+1:]
		src = src[:index]
	}
	return src, checksum, nil
}

// DownloadToFile downloads a given URL to the target filepath (including the cosign key if necessary).
// When src contains an inline checksum in the form URL@sha256, that checksum is used.
// For struct-based callers, prefer DownloadToFileWithChecksum.
func DownloadToFile(ctx context.Context, src, dst string) (err error) {
	// check if the parsed URL has a checksum
	// if so, remove it and use the checksum to validate the file
	src, checksum, err := parseChecksum(src)
	if err != nil {
		return err
	}

	return DownloadToFileWithChecksum(ctx, src, dst, checksum, "")
}

// DownloadToFileWithChecksum downloads src to dst, using checksum (if non-empty) for validation and caching.
// checksum SHOULD come from structured config (e.g. ZarfFile.Shasum) rather than being encoded into src.
func DownloadToFileWithChecksum(ctx context.Context, src, dst, checksum, cacheBaseName string) (err error) {
	if err := helpers.CreateDirectory(filepath.Dir(dst), helpers.ReadWriteExecuteUser); err != nil {
		return fmt.Errorf(lang.ErrCreatingDir, filepath.Dir(dst), err)
	}

	l := logger.From(ctx)
	cachePath := cacheFilePath(ctx, checksum, cacheBaseName)
	if cachePath != "" {
		hit, err := tryServeFromCache(ctx, cachePath, checksum, dst)
		if err != nil {
			return err
		}
		if hit {
			return nil
		}
	}

	err = retry.Do(
		func() error {
			// Create the file
			file, createErr := os.Create(dst)
			if createErr != nil {
				return retry.Unrecoverable(fmt.Errorf(lang.ErrWritingFile, dst, createErr))
			}
			getErr := httpGetFile(ctx, src, file)
			closeErr := file.Close()
			return errors.Join(getErr, closeErr)
		},
		retry.Attempts(uint(zconfig.ZarfDefaultRetries)),
		retry.Delay(zconfig.ZarfDefaultRetryDelay),
		retry.MaxDelay(zconfig.ZarfDefaultRetryMaxDelay),
		retry.DelayType(func(n uint, err error, rc *retry.Config) time.Duration {
			var rlErr retryAfterDuration
			if errors.As(err, &rlErr) {
				return time.Duration(rlErr)
			}
			return retry.BackOffDelay(n, err, rc)
		}),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.OnRetry(func(n uint, err error) {
			if zconfig.ZarfDefaultRetries > 1 && n+1 < uint(zconfig.ZarfDefaultRetries) {
				l.Warn("retrying download",
					"attempt", n+1,
					"maxAttempts", zconfig.ZarfDefaultRetries,
					"url", src,
					"error", err,
				)
			}
		}),
	)
	if err != nil {
		return err
	}

	// If the file has a checksum, validate it
	if 0 < len(checksum) {
		received, err := helpers.GetSHA256OfFile(dst)
		if err != nil {
			return err
		}
		if received != checksum {
			return fmt.Errorf("shasum mismatch for file %s: expected %s, got %s ", dst, checksum, received)
		}

		// update cache on successful download
		if cachePath != "" {
			updateCache(ctx, cachePath, dst)
		}
	}

	return nil
}

// cacheFilesSubdir is the subdirectory under the cache root where downloaded files
// are cached by checksum.
const cacheFilesSubdir = "files"

// cacheFilePath returns the on-disk cache path for checksum, or "" if caching isn't
// applicable (no checksum given, or the cache root can't be resolved).
func cacheFilePath(ctx context.Context, checksum, cacheBaseName string) string {
	if checksum == "" {
		return ""
	}
	cacheRoot, err := config.GetAbsCachePath()
	if err != nil || cacheRoot == "" {
		logger.From(ctx).Debug("cache root unavailable, skipping file cache", "error", err)
		return ""
	}
	cacheFileName := checksum
	if cacheBaseName != "" {
		cacheFileName = fmt.Sprintf("%s_%s", checksum, cacheBaseName)
	}
	return filepath.Join(cacheRoot, cacheFilesSubdir, cacheFileName)
}

// tryServeFromCache copies cachePath to dst if it exists and its content matches
// checksum. It reports (true, nil) when dst was populated from the cache. A cache
// miss (file absent, unreadable, or checksum mismatch) is not an error: it just
// means the caller should fall back to downloading, so it's logged at debug level
// and reported as (false, nil).
func tryServeFromCache(ctx context.Context, cachePath, checksum, dst string) (bool, error) {
	l := logger.From(ctx)
	if _, err := os.Stat(cachePath); err != nil {
		return false, nil
	}
	received, err := helpers.GetSHA256OfFile(cachePath)
	if err != nil {
		l.Debug("unable to checksum cache file, ignoring cache entry", "path", cachePath, "error", err)
		return false, nil
	}
	if received != checksum {
		l.Debug("cache file checksum mismatch, ignoring cache entry", "path", cachePath)
		return false, nil
	}

	srcFile, err := os.Open(cachePath)
	if err != nil {
		return false, err
	}
	defer srcFile.Close() //nolint:errcheck
	file, err := os.Create(dst)
	if err != nil {
		return false, fmt.Errorf(lang.ErrWritingFile, dst, err)
	}
	defer file.Close() //nolint:errcheck
	if _, err := io.Copy(file, srcFile); err != nil {
		return false, fmt.Errorf("unable to copy cached file %s: %w", cachePath, err)
	}
	return true, nil
}

// updateCache best-effort copies dst into the file cache at cachePath. It writes to
// a temp file in the same directory first and renames it into place, so concurrent
// downloads of the same checksum (this package is used under --oci-concurrency-style
// parallelism) can't interleave writes and corrupt the shared cache entry. Every
// failure is non-fatal to the caller, which already has a valid dst -- caching is
// purely an optimization for next time, so failures are logged at debug and dropped.
func updateCache(ctx context.Context, cachePath, dst string) {
	l := logger.From(ctx)
	if err := helpers.CreateDirectory(filepath.Dir(cachePath), helpers.ReadWriteExecuteUser); err != nil {
		l.Debug("unable to create cache directory, skipping cache update", "error", err)
		return
	}
	srcFile, err := os.Open(dst)
	if err != nil {
		l.Debug("unable to reopen downloaded file for caching, skipping cache update", "error", err)
		return
	}
	defer srcFile.Close() //nolint:errcheck

	tmp, err := os.CreateTemp(filepath.Dir(cachePath), filepath.Base(cachePath)+".tmp-*")
	if err != nil {
		l.Debug("unable to create temp cache file, skipping cache update", "error", err)
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) //nolint:errcheck // no-op once successfully renamed below

	if _, err := io.Copy(tmp, srcFile); err != nil {
		tmp.Close() //nolint:errcheck
		l.Debug("unable to write temp cache file, skipping cache update", "error", err)
		return
	}
	if err := tmp.Close(); err != nil {
		l.Debug("unable to close temp cache file, skipping cache update", "error", err)
		return
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		l.Debug("unable to move temp cache file into place, skipping cache update", "error", err)
	}
}

func httpGetFile(ctx context.Context, url string, destinationFile *os.File) (err error) {
	l := logger.From(ctx)
	l.Info("download start", "url", url)
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return retry.Unrecoverable(fmt.Errorf("unable to create request for %s: %w", url, err))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("unable to download the file %s: %w", url, err)
	}
	defer func() {
		err2 := resp.Body.Close()
		err = errors.Join(err, err2)
	}()

	// Check server response
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			if d := parseRetryAfter(resp.Header.Get("Retry-After")); d > 0 {
				const maxRetryAfter = 60 * time.Second
				if d > maxRetryAfter {
					return retry.Unrecoverable(fmt.Errorf("rate limited (HTTP 429) with Retry-After %s exceeding %s: %s", d, maxRetryAfter, resp.Status))
				}
				return retryAfterDuration(d)
			}
			return fmt.Errorf("rate limited (HTTP 429): %s", resp.Status)
		}
		if resp.StatusCode >= 500 {
			return fmt.Errorf("server error: %s", resp.Status)
		}
		return retry.Unrecoverable(fmt.Errorf("bad HTTP status: %s", resp.Status))
	}

	// Copy response body to file
	if _, err = io.Copy(destinationFile, resp.Body); err != nil {
		return fmt.Errorf("unable to save the file %s: %w", destinationFile.Name(), err)
	}
	l.Debug("download successful", "url", url, "size", resp.ContentLength, "duration", time.Since(start))
	return nil
}

// parseRetryAfter parses the Retry-After header value into a duration.
// It supports both delay-seconds (integer) and HTTP-date formats.
func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Duration(seconds) * time.Second
	}
	if t, err := http.ParseTime(value); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
