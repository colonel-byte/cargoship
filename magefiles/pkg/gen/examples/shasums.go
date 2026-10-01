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

package examples

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

// exampleShasumsPath is the committed url -> sha256 cache. Hashing an RPM means downloading
// it, and rke2-common alone is ~29MB, so without a cache every example regeneration would
// pull roughly a gigabyte. With one, only genuinely new releases are fetched.
const exampleShasumsPath = "example/shasums.json"

// exampleShasum is one hashed file: every URL known to serve it, and what it hashed to.
//
// pkgs.k8s.io mirrors a package into every minor line's repo that has shipped since, unchanged,
// under that line's own URL path -- kubernetes-cni and cri-tools do not rev on most k8s minors,
// so the same filename+digest is genuinely reachable at several different URLs at once. One URL
// per entry cannot represent that: the second minor line rendered would just overwrite the
// first's recorded URL, flipping it back and forth on every regeneration for no reason.
type exampleShasum struct {
	URLs   []string `json:"urls"`
	SHA256 string   `json:"sha256"`
}

// UnmarshalJSON accepts either the current "urls" list or the single "url" string the cache
// used before entries could hold more than one URL, so an older committed shasums.json does not
// need a one-off migration and does not lose its cached digests.
func (e *exampleShasum) UnmarshalJSON(data []byte) error {
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

// exampleShasums resolves a remote file's sha256, remembering what it has already hashed.
// Entries are keyed by file name rather than URL, so the cache reads as a list of the files
// the examples install; every URL an entry was ever fetched from is kept alongside it, and a
// file name whose digest changes under a URL not already recorded is re-hashed rather than
// trusted, since that means the filename was reused for different content.
//
// A file extracted out of an archive is keyed by the archive's name and the path inside it,
// joined by memberKeySeparator -- rke2.linux-amd64.tar.gz!bin/rke2. That is a separate entry
// from the archive itself, because they are separate files with separate digests, and a
// definition that sets extractPath declares the member's.
type exampleShasums struct {
	sums  map[string]exampleShasum
	dirty bool

	// members holds the digests of every file inside an archive already streamed this run,
	// keyed by the archive's URL and then by the path inside it. One pass over a tarball
	// hashes all of it, so a template extracting nine files from one archive downloads it
	// once however many of those files the committed cache is still missing. Not persisted:
	// what is worth keeping between runs lands in sums, one entry per member asked for.
	members map[string]map[string]string
}

// lookup finds a cached entry that url is one of the recorded URLs for.
func (s *exampleShasums) lookup(url string) (exampleShasum, bool) {
	return s.lookupKey(path.Base(url), url)
}

// lookupKey is lookup against an explicit cache key, for entries not keyed by file name alone.
func (s *exampleShasums) lookupKey(key, url string) (exampleShasum, bool) {
	e, ok := s.sums[key]
	return e, ok && slices.Contains(e.URLs, url)
}

// store records that url serves a file whose digest is sha256, under the file's own name.
func (s *exampleShasums) store(url, sha256 string) {
	s.storeKey(path.Base(url), url, sha256)
}

// storeKey records that url serves a file whose digest is sha256, under an explicit cache key
// -- the file's own name for a whole file, the archive's name and the path inside it for a
// member. url is added as one more known location for the entry when the digest matches what
// is already cached, or replaces the entry outright when it does not (the same key serving
// different content under this url). A no-op when url is already recorded against this digest,
// so a re-render that changes nothing does not mark the cache dirty.
func (s *exampleShasums) storeKey(key, url, sha256 string) {
	e, ok := s.sums[key]
	if ok && e.SHA256 == sha256 {
		if slices.Contains(e.URLs, url) {
			return
		}
		e.URLs = append(e.URLs, url)
		s.sums[key] = e
		s.dirty = true
		return
	}
	s.sums[key] = exampleShasum{URLs: []string{url}, SHA256: sha256}
	s.dirty = true
}

// loadExampleShasums reads the cache, treating a missing file as an empty one.
func loadExampleShasums() (*exampleShasums, error) {
	s := &exampleShasums{
		sums:    map[string]exampleShasum{},
		members: map[string]map[string]string{},
	}

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

	s.store(url, sum)
	return sum, nil
}

// record stores a digest the caller already knows to be correct -- a repo's own package index
// publishes a sha256 for each file it lists, so upstream's example never has to download and
// hash what it can just trust and cache.
func (s *exampleShasums) record(url, sha256 string) {
	s.store(url, sha256)
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
	defer resp.Body.Close() //nolint:errcheck // body is read to completion, nothing to do with a close error

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

	for name, e := range s.sums {
		sort.Strings(e.URLs)
		s.sums[name] = e
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
	defer resp.Body.Close() //nolint:errcheck // body is read to completion, nothing to do with a close error

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
