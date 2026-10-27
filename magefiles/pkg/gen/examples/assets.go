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

package examples

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sync"
)

// exampleAssetsPath is the committed url -> lines cache of the release text assets the examples
// read. Unlike the on-disk cache in release_lines.go this one is committed, so that a fresh
// clone can re-render every example already on disk without reaching upstream at all -- which
// is the only way an air-gapped management node can regenerate them.
const exampleAssetsPath = "example/assets.json"

// exampleAssets is the committed url -> lines release text asset cache.
type exampleAssets struct {
	assets map[string][]string
	dirty  bool
}

var (
	assetsOnce  sync.Once
	assetsStore *exampleAssets
)

func committedAssets() *exampleAssets {
	assetsOnce.Do(func() {
		assetsStore = loadCommittedAssetsFrom(exampleAssetsPath)
	})
	return assetsStore
}

// loadCommittedAssetsFrom reads the cache, falling back to an empty one. A missing file is the
// first run; a corrupt one is a warning rather than an error, since everything in it can be
// fetched again.
func loadCommittedAssetsFrom(path string) *exampleAssets {
	store := &exampleAssets{assets: map[string][]string{}}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &store.assets); err != nil {
			fmt.Printf("warning: parsing %s: %v\n", path, err)
		}
	}
	return store
}

func (c *exampleAssets) lookup(url string) ([]string, bool) {
	lines, ok := c.assets[url]
	return lines, ok && len(lines) > 0
}

// store records an asset's lines, leaving the cache clean when they match what is already
// there so a re-render that changes nothing does not rewrite the file.
func (c *exampleAssets) store(url string, lines []string) {
	if len(lines) == 0 || slices.Equal(c.assets[url], lines) {
		return
	}
	c.assets[url] = lines
	c.dirty = true
}

func (c *exampleAssets) save() error {
	return c.saveTo(exampleAssetsPath)
}

// saveTo rewrites the cache when anything was added to it. Keys are marshaled sorted, so the
// committed file stays stable across runs.
func (c *exampleAssets) saveTo(path string) error {
	if !c.dirty {
		return nil
	}
	data, err := json.MarshalIndent(c.assets, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return err
	}
	c.dirty = false
	fmt.Printf("Wrote %s (%d asset(s))\n", path, len(c.assets))
	return nil
}
