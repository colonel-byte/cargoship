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
	"os"
	"path/filepath"
	"testing"
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
