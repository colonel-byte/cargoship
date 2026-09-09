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

// This file holds the sha256 cache the example templates hash their remote files against.
// The targets that render examples live in their own gen-example*.go files.

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
)

// exampleShasumsPath is the committed url -> sha256 cache. Hashing an RPM means downloading
// it, and rke2-common alone is ~29MB, so without a cache every example regeneration would
// pull roughly a gigabyte. With one, only genuinely new releases are fetched.
const exampleShasumsPath = "example/shasums.json"

// exampleShasum is one hashed file: the URL it was fetched from, and what it hashed to.
type exampleShasum struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// exampleShasums resolves a remote file's sha256, remembering what it has already hashed.
// Entries are keyed by file name rather than URL, so the cache reads as a list of the RPMs
// the examples install; the URL each was fetched from is kept in the entry, and an entry
// whose URL no longer matches is re-hashed rather than trusted.
type exampleShasums struct {
	sums  map[string]exampleShasum
	dirty bool
}

// lookup finds a cached entry for url, treating a name cached from some other URL as a miss.
func (s *exampleShasums) lookup(url string) (exampleShasum, bool) {
	e, ok := s.sums[path.Base(url)]
	return e, ok && e.URL == url
}

// loadExampleShasums reads the cache, treating a missing file as an empty one.
func loadExampleShasums() (*exampleShasums, error) {
	s := &exampleShasums{sums: map[string]exampleShasum{}}

	data, err := os.ReadFile(exampleShasumsPath)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", exampleShasumsPath, err)
	}
	if err := json.Unmarshal(data, &s.sums); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", exampleShasumsPath, err)
	}
	return s, nil
}

// get is the template's sha256 function: the digest of what url serves, downloading and
// hashing it the first time and reading the cache every time after.
//
// A URL upstream no longer serves yields "" rather than an error, so the template can leave
// the shasum off that file rather than one missing RPM failing the whole render. A build
// whose RPMs were pulled wholesale never reaches here -- writeExample skips it -- so this
// covers the odd file a still-published build lost. Misses are not cached, so a restored URL
// is picked up on the next run.
func (s *exampleShasums) get(url string) (string, error) {
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

	s.sums[path.Base(url)] = exampleShasum{URL: url, SHA256: sum}
	s.dirty = true
	return sum, nil
}

// published reports whether upstream still serves url. Anything already hashed is, by
// definition, so only URLs the cache has never seen cost a request -- and that request is a
// HEAD, since nothing here needs the bytes. Nothing is remembered either way: a build
// Rancher pulls stops being published between one run and the next, and a URL that comes
// back should be picked up just as readily.
func (s *exampleShasums) published(url string) (bool, error) {
	if _, ok := s.lookup(url); ok {
		return true, nil
	}

	resp, err := http.Head(url)
	if err != nil {
		return false, fmt.Errorf("checking %s: %w", url, err)
	}
	defer resp.Body.Close()

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

// save rewrites the cache when anything was added to it. Keys are marshaled sorted, so the
// committed file stays stable across runs.
func (s *exampleShasums) save() error {
	if !s.dirty {
		return nil
	}

	data, err := json.MarshalIndent(s.sums, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", exampleShasumsPath, err)
	}
	if err := os.WriteFile(exampleShasumsPath, append(data, '\n'), 0o644); err != nil {
		return err
	}

	s.dirty = false
	fmt.Printf("Wrote %s (%d file(s))\n", exampleShasumsPath, len(s.sums))
	return nil
}

// hashURL streams a remote file through sha256 without holding it in memory. A 404 returns
// "" with no error -- see get.
func hashURL(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

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
