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
	"fmt"
	"os"
	"path/filepath"
)

// CacheDir is the subdirectory name used under the OS user cache directory
// for cargoship's shared cache.
const CacheDir = "cargoship"

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
