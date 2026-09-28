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

// Package shasums is the sha256 cache in example/shasums.json that the example templates hash
// their remote files against.
//
// A digest is downloaded once and committed, so regenerating the examples neither re-downloads
// gigabytes of engine artifacts nor produces a different file on a machine with a different
// network. See magefiles/pkg/example for the templates that consume it.
package shasums

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"slices"
	"sort"
)

// Path is the committed url -> sha256 cache. Hashing an RPM means downloading
// it, and rke2-common alone is ~29MB, so without a cache every example regeneration would
// pull roughly a gigabyte. With one, only genuinely new releases are fetched.
const Path = "example/shasums.json"

// Entry is one hashed file: every URL known to serve it, and what it hashed to.
//
// pkgs.k8s.io mirrors a package into every minor line's repo that has shipped since, unchanged,
// under that line's own URL path -- kubernetes-cni and cri-tools do not rev on most k8s minors,
// so the same filename+digest is genuinely reachable at several different URLs at once. One URL
// per entry cannot represent that: the second minor line rendered would just overwrite the
// first's recorded URL, flipping it back and forth on every regeneration for no reason.
type Entry struct {
	URLs   []string `json:"urls"`
	SHA256 string   `json:"sha256"`
}

// UnmarshalJSON accepts either the current "urls" list or the single "url" string the cache
// used before entries could hold more than one URL, so an older committed shasums.json does not
// need a one-off migration and does not lose its cached digests.
func (e *Entry) UnmarshalJSON(data []byte) error {
	var raw struct {
		URL    string   `json:"url"`
		URLs   []string `json:"urls"`
		SHA256 string   `json:"sha256"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	e.SHA256 = raw.SHA256
	e.URLs = raw.URLs
	if raw.URL != "" && !slices.Contains(e.URLs, raw.URL) {
		e.URLs = append(e.URLs, raw.URL)
	}
	return nil
}

// Cache resolves a remote file's sha256, remembering what it has already hashed.
// Entries are keyed by file name rather than URL, so the cache reads as a list of the RPMs
// the examples install; every URL an entry was ever fetched from is kept alongside it, and a
// file name whose digest changes under a URL not already recorded is re-hashed rather than
// trusted, since that means the filename was reused for different content.
type Cache struct {
	sums  map[string]Entry
	dirty bool
}

// lookup finds a cached entry that url is one of the recorded URLs for.
func (s *Cache) lookup(url string) (Entry, bool) {
	e, ok := s.sums[path.Base(url)]
	return e, ok && slices.Contains(e.URLs, url)
}

// store records that url serves a file whose digest is sha256, adding url as one more known
// location for it when the digest matches what is already cached, or replacing the entry
// outright when it does not (the same file name serving different content under this url).
// A no-op when url is already recorded against this digest, so a re-render that changes nothing
// does not mark the cache dirty.
func (s *Cache) store(url, sha256 string) {
	name := path.Base(url)
	e, ok := s.sums[name]
	if ok && e.SHA256 == sha256 {
		if slices.Contains(e.URLs, url) {
			return
		}
		e.URLs = append(e.URLs, url)
		s.sums[name] = e
		s.dirty = true
		return
	}
	s.sums[name] = Entry{URLs: []string{url}, SHA256: sha256}
	s.dirty = true
}

// Load reads the cache, treating a missing file as an empty one.
func Load() (*Cache, error) {
	s := &Cache{sums: map[string]Entry{}}

	data, err := os.ReadFile(Path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", Path, err)
	}
	if err := json.Unmarshal(data, &s.sums); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", Path, err)
	}
	return s, nil
}

// Get is the template's sha256 function: the digest of what url serves, downloading and
// hashing it the first time and reading the cache every time after.
//
// A URL upstream no longer serves yields "" rather than an error, so the template can leave
// the shasum off that file rather than one missing RPM failing the whole render. A build
// whose RPMs were pulled wholesale never reaches here -- writeExample skips it -- so this
// covers the odd file a still-published build lost. Misses are not cached, so a restored URL
// is picked up on the next run.
func (s *Cache) Get(url string) (string, error) {
	if e, ok := s.lookup(url); ok {
		return e.SHA256, nil
	}

	sum, err := hashURL(url)
	if err != nil {
		return "", err
	}
	if sum == "" {
		fmt.Printf("warning: no shasum for %s: upstream no longer serves it\n", url)
		return "", nil
	}

	s.store(url, sum)
	return sum, nil
}

// Record stores a digest the caller already knows to be correct -- a repo's own package index
// publishes a sha256 for each file it lists, so upstream's example never has to download and
// hash what it can just trust and cache.
func (s *Cache) Record(url, sha256 string) {
	s.store(url, sha256)
}

// Published reports whether upstream still serves url. Anything already hashed is, by
// definition, so only URLs the cache has never seen cost a request -- and that request is a
// HEAD, since nothing here needs the bytes. Nothing is remembered either way: a build
// Rancher pulls stops being published between one run and the next, and a URL that comes
// back should be picked up just as readily.
func (s *Cache) Published(url string) (bool, error) {
	if _, ok := s.lookup(url); ok {
		return true, nil
	}

	resp, err := http.Head(url)
	if err != nil {
		return false, fmt.Errorf("checking %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body, nothing to do with a close error

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound, http.StatusForbidden:
		// Object stores answer a deleted object with either.
		return false, nil
	default:
		return false, fmt.Errorf("checking %s: %s", url, resp.Status)
	}
}

// Save rewrites the cache when anything was added to it. Keys are marshaled sorted, so the
// committed file stays stable across runs.
func (s *Cache) Save() error {
	if !s.dirty {
		return nil
	}

	for name, e := range s.sums {
		sort.Strings(e.URLs)
		s.sums[name] = e
	}

	data, err := json.MarshalIndent(s.sums, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", Path, err)
	}
	if err := os.WriteFile(Path, append(data, '\n'), 0o644); err != nil {
		return err
	}

	s.dirty = false
	fmt.Printf("Wrote %s (%d file(s))\n", Path, len(s.sums))
	return nil
}

// hashURL streams a remote file through sha256 without holding it in memory. A 404 returns
// "" with no error -- see get.
func hashURL(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body, nothing to do with a close error

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching %s: %s", url, resp.Status)
	}

	h := sha256.New()
	n, err := io.Copy(h, resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", url, err)
	}

	fmt.Printf("Hashed %s (%.1f MiB)\n", url, float64(n)/(1<<20))
	return hex.EncodeToString(h.Sum(nil)), nil
}
