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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	retry "github.com/avast/retry-go/v4"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/helpers"
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

// ParseChecksum splits an inline checksum off the end of a source string, of the form
// URL@checksum. A single '@' is left alone when the URL parses with userinfo (user@host), so a
// credential embedded in the URL is never mistaken for a checksum suffix.
func ParseChecksum(src string) (string, string, error) {
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
	src, checksum, err := ParseChecksum(src)
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

// archiveCacheKeyPrefix marks a cache entry keyed on a URL rather than on a content
// digest, so the two kinds are telling apart at a glance in a directory listing and an
// entry written by one scheme is never read by the other.
const archiveCacheKeyPrefix = "url-"

// CachedArchive returns the path to the archive at src inside the shared file cache,
// downloading it there first if it is not already present. The returned path is the
// cache entry itself, so a caller extracting from it reads the cache in place rather
// than copying a multi-hundred-megabyte archive to use it once.
//
// It exists because an archive carrying an extractPath file declares no digest of its
// own: with extractPath set, the file's Shasum is the digest of the extracted member,
// not of the archive it came out of, so the shasum-keyed cache behind
// DownloadToFileWithChecksum has no key to store one under. This keys on the resolved
// URL instead.
//
// A cached archive is therefore trusted on its URL rather than on its content, which is
// weaker than what this repository does everywhere else and is worth stating rather than
// leaving for a reader to notice. What makes it safe enough is that the member extracted
// from the archive is still checked against the shasum the definition declares, so a bad
// entry here cannot produce a bad package -- and where a definition declares no shasum,
// the extracted file is equally unverified whether or not the archive came from a cache,
// so nothing is weakened relative to fetching it every time.
func CachedArchive(ctx context.Context, src, baseName string) (string, error) {
	l := logger.From(ctx)

	cacheRoot, err := config.GetAbsCachePath()
	if err != nil {
		return "", fmt.Errorf("unable to resolve the cache directory for %s: %w", src, err)
	}

	// Keyed on a digest of the whole URL and not on baseName: two releases very commonly
	// end in the same file name -- every rke2 and etcd tarball does -- and keying on that
	// would serve one version's archive for another's.
	sum := sha256.Sum256([]byte(src))
	name := archiveCacheKeyPrefix + hex.EncodeToString(sum[:])
	if baseName != "" {
		name += "_" + baseName
	}
	cachePath := filepath.Join(cacheRoot, cacheFilesSubdir, name)

	if _, err := os.Stat(cachePath); err == nil {
		l.Debug("found archive in cache, using cached copy", "url", src, "path", cachePath)
		return cachePath, nil
	}

	if err := helpers.CreateDirectory(filepath.Dir(cachePath), helpers.ReadWriteExecuteUser); err != nil {
		return "", fmt.Errorf(lang.ErrCreatingDir, filepath.Dir(cachePath), err)
	}

	l.Info("archive not in cache, downloading", "url", src, "path", cachePath)

	// Downloaded beside the entry and renamed into place, so a failed or partial download
	// never becomes a cache hit for the next build, and two builds racing on the same
	// archive cannot interleave writes into one entry.
	tmp, err := os.CreateTemp(filepath.Dir(cachePath), filepath.Base(cachePath)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("unable to create a temporary file for %s: %w", src, err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		return "", errors.Join(fmt.Errorf("unable to create a temporary file for %s: %w", src, err), os.Remove(tmpPath))
	}
	defer os.Remove(tmpPath) //nolint:errcheck // a no-op once renamed into place below

	// An empty checksum is passed deliberately: there is none to check against, per the
	// comment above. It also disables the shasum-keyed caching inside this call, which
	// would otherwise have nothing to key on either.
	if err := DownloadToFileWithChecksum(ctx, src, tmpPath, "", ""); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		return "", fmt.Errorf("unable to move the downloaded archive into the cache: %w", err)
	}
	return cachePath, nil
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
			if d := ParseRetryAfter(resp.Header.Get("Retry-After")); d > 0 {
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

// ParseRetryAfter parses the Retry-After header value into a duration.
// It supports both delay-seconds (integer) and HTTP-date formats.
func ParseRetryAfter(value string) time.Duration {
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

// memberCacheKeyPrefix marks a cache entry holding one member extracted out of an archive,
// keyed on a digest of that archive rather than on the member's own content.
const memberCacheKeyPrefix = "member-"

// memberCachePath returns the cache path for the member at extractPath inside the archive with
// the given content digest, or "" when caching does not apply.
//
// The key is a digest of the archive plus the path taken out of it. That is a stronger key
// than the one CachedArchive can manage: an archive reached by URL carries no digest of its
// own, so that cache is keyed on the URL and trusts an archive by its name. An archive that is
// an OCI layer does carry one, verified against the registry on fetch, and extraction is
// deterministic -- so an entry here is as trustworthy as the archive it came out of.
//
// Like the archive cache it is not re-verified on read. What makes that safe enough is the
// same thing: the member is still checked against the shasum the definition declares, and an
// entry that declares none is equally unverified whether or not a cache was involved.
func memberCachePath(ctx context.Context, archiveDigest, extractPath string) string {
	if archiveDigest == "" || extractPath == "" {
		return ""
	}
	cacheRoot, err := config.GetAbsCachePath()
	if err != nil || cacheRoot == "" {
		logger.From(ctx).Debug("cache root unavailable, skipping member cache", "error", err)
		return ""
	}
	sum := sha256.Sum256([]byte(archiveDigest + "\x00" + extractPath))
	name := memberCacheKeyPrefix + hex.EncodeToString(sum[:])
	if base := path.Base(filepath.ToSlash(extractPath)); base != "" && base != "." && base != "/" {
		name += "_" + base
	}
	return filepath.Join(cacheRoot, cacheFilesSubdir, name)
}

// CachedMember returns the path to the cached copy of the member at extractPath inside the
// archive with the given content digest, and whether there was one.
func CachedMember(ctx context.Context, archiveDigest, extractPath string) (string, bool) {
	cachePath := memberCachePath(ctx, archiveDigest, extractPath)
	if cachePath == "" {
		return "", false
	}
	if _, err := os.Stat(cachePath); err != nil {
		return "", false
	}
	logger.From(ctx).Debug("found extracted member in cache", "member", extractPath, "path", cachePath)
	return cachePath, true
}

// CacheMember copies the extracted member at src into the shared cache, so the next build
// takes it from there rather than fetching and decompressing the archive again.
//
// A failure to cache is not a failure to build: the member is already staged, and the only
// cost is doing this work again next time.
func CacheMember(ctx context.Context, archiveDigest, extractPath, src string) {
	cachePath := memberCachePath(ctx, archiveDigest, extractPath)
	if cachePath == "" {
		return
	}
	if err := helpers.CreateDirectory(filepath.Dir(cachePath), helpers.ReadWriteExecuteUser); err != nil {
		logger.From(ctx).Debug("unable to create the member cache directory", "error", err)
		return
	}
	// Written beside the entry and renamed into place, so a partial copy never becomes a
	// cache hit for the next build.
	tmp, err := os.CreateTemp(filepath.Dir(cachePath), filepath.Base(cachePath)+".tmp-*")
	if err != nil {
		logger.From(ctx).Debug("unable to stage a member cache entry", "error", err)
		return
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		logger.From(ctx).Debug("unable to stage a member cache entry", "error", err)
		return
	}
	defer os.Remove(tmpPath) //nolint:errcheck // a no-op once renamed into place below

	if err := helpers.CreatePathAndCopy(src, tmpPath); err != nil {
		logger.From(ctx).Debug("unable to copy a member into the cache", "error", err)
		return
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		logger.From(ctx).Debug("unable to move a member into the cache", "error", err)
		return
	}
	logger.From(ctx).Debug("cached extracted member", "member", extractPath, "path", cachePath)
}
