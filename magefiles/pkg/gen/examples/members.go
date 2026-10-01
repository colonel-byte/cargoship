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

// This file holds the digests of files inside a remote archive. The cache they are kept in,
// and the digests of whole files, are in shasums.go.

package examples

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"path"
	"strings"
)

// memberKeySeparator joins an archive's name to a path inside it, making the cache key for one
// member. An archive and its members each get their own entry in the cache, since their digests
// are different files and a definition that extracts a member declares the member's.
const memberKeySeparator = "!"

// memberKey is the cache key for one file inside the archive url serves.
//
// The archive is named by the last two segments of the URL's path, not by its file name alone:
// every rke2 release publishes rke2.linux-amd64.tar.gz under that one name, so a key built from
// the name would be the same for all seventy of them. Each release would evict the last, the
// cache would hold one version's digests however many were rendered, and every run would
// re-download every tarball. The segment above the file name is the release tag, which is what
// makes the key specific to a version:
//
//	v1.37.0%2Brke2r1/rke2.linux-amd64.tar.gz!bin/rke2
//
// Whole-file entries do not need this -- an RPM or a deb carries its version in its own file
// name -- so they stay keyed by file name and the cache still reads as an inventory.
func memberKey(url, member string) string {
	return archiveKey(url) + memberKeySeparator + path.Clean(member)
}

// archiveKey names an archive by the last two segments of its URL path, falling back to the
// file name alone when there is no segment above it.
func archiveKey(rawURL string) string {
	p := rawURL
	if u, err := neturl.Parse(rawURL); err == nil {
		p = u.EscapedPath()
	}
	p = strings.TrimSuffix(p, "/")

	base := path.Base(p)
	parent := path.Base(path.Dir(p))
	if parent == "." || parent == "/" || parent == base {
		return base
	}
	return parent + "/" + base
}

// member is the template's sha256member function: the digest of one file inside the archive
// url serves, which is what a files entry extracting that file has to declare -- the archive
// around it hashes to something else entirely.
//
// Every member of an archive is hashed by the single pass that the first one costs, so a
// template pulling nine files out of one tarball downloads it once. As with get, a URL
// upstream no longer serves yields "" rather than an error, so the template leaves the shasum
// off that file instead of failing the whole render. A member the archive does not hold is an
// error: the archive answered, so the template asked for the wrong path.
func (s *exampleShasums) member(url, name string) (string, error) {
	name = path.Clean(name)
	key := memberKey(url, name)
	if e, ok := s.lookupKey(key, url); ok {
		return e.SHA256, nil
	}

	sums, ok := s.members[url]
	if !ok {
		var err error
		if sums, err = hashArchiveMembers(url); err != nil {
			return "", err
		}
		if sums == nil {
			fmt.Printf("warning: no shasum for %s: upstream no longer serves it\n", url)
			return "", nil
		}
		s.members[url] = sums
	}

	sum, ok := sums[name]
	if !ok {
		return "", fmt.Errorf("%s holds no %s", url, name)
	}

	s.storeKey(key, url, sum)
	return sum, nil
}

// hashArchiveMembers streams a remote tar archive once and returns the sha256 of every
// regular file in it, keyed by the cleaned path the archive holds it under. Nothing is
// written to disk and no member is held in memory: each one is hashed as it goes past.
//
// A 404 returns a nil map with no error -- see member.
func hashArchiveMembers(url string) (map[string]string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck // body is read to completion, nothing to do with a close error

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: %s", url, resp.Status)
	}

	// counter sits under the decompressor so the size reported is the transfer, which is what
	// the equivalent line from hashURL reports and what a reader would compare it against.
	counter := &countingReader{r: resp.Body}

	var body io.Reader = counter
	if isGzipURL(url) {
		gz, err := gzip.NewReader(counter)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", url, err)
		}
		defer gz.Close() //nolint:errcheck // read to completion, nothing to do with a close error
		body = gz
	}

	sums := map[string]string{}
	tr := tar.NewReader(body)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", url, err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		h := sha256.New()
		if _, err := io.Copy(h, tr); err != nil {
			return nil, fmt.Errorf("reading %s from %s: %w", hdr.Name, url, err)
		}
		sums[path.Clean(hdr.Name)] = hex.EncodeToString(h.Sum(nil))
	}

	fmt.Printf("Hashed %d file(s) in %s (%.1f MiB)\n", len(sums), url, float64(counter.n)/(1<<20))
	return sums, nil
}

// isGzipURL reports whether url names a gzipped tar. Every archive the examples extract from
// is one; anything else reaches the tar reader undecompressed and fails there, naming the URL,
// rather than being silently mis-read.
func isGzipURL(url string) bool {
	return strings.HasSuffix(url, ".tar.gz") || strings.HasSuffix(url, ".tgz")
}

// countingReader totals the bytes read through it.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
