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

// This file holds the cache the example targets read a release's text assets from, and the
// line splitting the cache and the network path share. The targets that render examples
// live in their own gen-example*.go files.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/colonel-byte/cargoship/src/pkg/utils"
	"gopkg.in/yaml.v3"
)

// exampleCacheOff names the environment variable that turns the cache off for a run. A
// release's assets are immutable in the ordinary case, but Rancher does re-cut a build in
// place, and a cache with no way out would answer from the old bytes forever. Set it to
// anything to refetch, which also overwrites what was cached.
const exampleCacheOff = "CARGOSHIP_EXAMPLES_NO_CACHE"

// exampleConfigPath is the config file the cache takes its location from. It is read
// relative to the working directory, once per run, since mage is run from the repository
// root; resolving it once is what keeps every asset in a run agreeing on where the cache is.
const exampleConfigPath = "cargoship-config.yaml"

// releaseLinesEntry is one cached asset: the lines it held, and the URL they came from. An
// entry is written as JSON rather than the bytes upstream served, so that a file cut short
// fails to parse instead of reading back as a whole asset. The file name already encodes the
// URL, so the field is what makes an entry readable on its own -- and it is still checked on
// the way back out, so an entry recorded against some other URL is a miss rather than an
// answer. This is the rule example/shasums.json follows.
type releaseLinesEntry struct {
	URL   string   `json:"url"`
	Lines []string `json:"lines"`
}

// releaseLinesCache remembers the text assets a release publishes, so that re-rendering an
// example costs nothing upstream. Unlike example/shasums.json this cache is not committed:
// its contents are already in every rendered distro.yaml, and the backfill ExampleLine does
// would grow it without bound. What it borrows from that file is the part that matters --
// entries are only trusted when the URL still matches, and there is a way to force a refetch.
type releaseLinesCache struct {
	dir      string // where entries are written; empty when caching is off
	warnOnce sync.Once
}

var (
	releaseLinesOnce  sync.Once
	releaseLinesStore *releaseLinesCache
)

// releaseLines returns the cache every fetch in a run shares, working out where it lives on
// first use. A cache that cannot be located is not an error: the run falls back to fetching
// everything, which is what it did before there was a cache.
func releaseLines() *releaseLinesCache {
	releaseLinesOnce.Do(func() {
		dir, err := exampleCacheDir()
		if err != nil {
			fmt.Printf("warning: not caching release assets: %v\n", err)
		}
		releaseLinesStore = &releaseLinesCache{dir: dir}
	})
	return releaseLinesStore
}

// exampleCacheDir resolves the directory cached assets are written to: the `zarf_cache` the
// config names, or the same OS cache directory the binary falls back to, so that a developer
// with XDG_CACHE_HOME set does not end up with two cargoship caches. An empty path means the
// cache is off.
func exampleCacheDir() (string, error) {
	if os.Getenv(exampleCacheOff) != "" {
		return "", nil
	}

	configured, err := configuredCachePath()
	if err != nil {
		return "", err
	}
	root, err := utils.ResolveCachePath(configured)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "examples"), nil
}

// configuredCachePath reads `zarf_cache` out of the config, returning "" when there is no
// config to read so that the caller falls back to the OS cache directory. A config that
// cannot be parsed is an error rather than a silent fallback, since a developer who set a
// cache path meant it.
func configuredCachePath() (string, error) {
	data, err := os.ReadFile(exampleConfigPath)
	if err != nil {
		return "", nil
	}

	var cfg struct {
		ZarfCache string `yaml:"zarf_cache"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parsing %s: %w", exampleConfigPath, err)
	}
	return expandHome(cfg.ZarfCache)
}

// expandHome resolves a leading ~ against the home directory. A home directory that cannot
// be found is an error, rather than leaving the ~ in place for MkdirAll to take literally
// and create a directory named "~" in the repository.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expanding %q: %w", path, err)
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/")), nil
}

var (
	assetScheme = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)
	assetSep    = regexp.MustCompile(`/`)
)

// entryName flattens an asset URL into one file name, the way image tarballs are named from
// image references in phase 50: drop the scheme, and every separator becomes an underscore.
// The whole URL is in the name, so two repositories serving the same asset on the same tag
// cache apart, and no part of a URL can name a directory of its own or walk out of the cache.
func entryName(assetURL string) string {
	return assetSep.ReplaceAllLiteralString(assetScheme.ReplaceAllLiteralString(assetURL, ""), "_")
}

// entryPath is where the asset assetURL serves is cached: one flat file per asset, under the
// one cache directory.
func (c *releaseLinesCache) entryPath(assetURL string) string {
	if c.dir == "" {
		return ""
	}
	return filepath.Join(c.dir, entryName(assetURL))
}

// lookup returns what was cached for assetURL. Anything unreadable, unparsable, empty, or
// recorded against a different URL is a miss, so a half-written or superseded entry costs a
// refetch rather than yielding a partial image list.
func (c *releaseLinesCache) lookup(assetURL string) ([]string, bool) {
	path := c.entryPath(assetURL)
	if path == "" {
		return nil, false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var entry releaseLinesEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}
	if entry.URL != assetURL || len(entry.Lines) == 0 {
		return nil, false
	}
	return entry.Lines, true
}

// store records what assetURL served. A cache that cannot be written is reported once and
// then left alone: it costs a refetch, not a render, and repeating the same warning for
// every asset of every tag would bury the output it is printed among.
func (c *releaseLinesCache) store(assetURL string, lines []string) {
	path := c.entryPath(assetURL)
	if path == "" || len(lines) == 0 {
		return
	}

	data, err := json.MarshalIndent(releaseLinesEntry{URL: assetURL, Lines: lines}, "", "  ")
	if err == nil {
		err = writeFileAtomic(path, append(data, '\n'))
	}
	if err != nil {
		c.warnOnce.Do(func() {
			fmt.Printf("warning: not caching release assets: %v\n", err)
		})
	}
}

// writeFileAtomic writes through a uniquely named temporary file in the target's own
// directory and renames it into place. An interrupted run, or a second run alongside this
// one, then leaves either the old entry or the new one rather than a truncated file that
// every later run would read as the whole asset.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	f, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)

	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("saving %s: %w", path, err)
	}
	return nil
}

// splitReleaseLines renders a release's text asset as its non-empty lines, in file order.
func splitReleaseLines(body []byte) []string {
	var lines []string
	for line := range strings.SplitSeq(string(body), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
