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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tarGzFixture builds a gzipped tar holding one regular file per entry, plus a directory and a
// symlink that nothing should hash.
func tarGzFixture(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "bin/",
		Typeflag: tar.TypeDir,
		Mode:     0o755,
	}))
	for name, content := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     name,
			Typeflag: tar.TypeReg,
			Mode:     0o755,
			Size:     int64(len(content)),
		}))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "bin/rke2-link",
		Typeflag: tar.TypeSymlink,
		Linkname: "rke2",
	}))

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	return buf.Bytes()
}

// sha256Hex is the digest a files entry would have to declare for content.
func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// serveArchive serves body at any path ending in the tarball name, counting requests.
func serveArchive(t *testing.T, body []byte, requests *int) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*requests++
		if _, err := w.Write(body); err != nil {
			t.Errorf("writing archive: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestHashArchiveMembers(t *testing.T) {
	files := map[string]string{
		"bin/rke2":                               "the rke2 binary\n",
		"bin/rke2-killall.sh":                    "#!/bin/sh\nkillall\n",
		"lib/systemd/system/rke2-server.service": "[Unit]\n",
	}

	var requests int
	srv := serveArchive(t, tarGzFixture(t, files), &requests)

	sums, err := hashArchiveMembers(srv.URL + "/releases/download/v1.37.0%2Brke2r1/rke2.linux-amd64.tar.gz")
	require.NoError(t, err)

	want := map[string]string{}
	for name, content := range files {
		want[name] = sha256Hex(content)
	}
	assert.Equal(t, want, sums, "every regular file, and only the regular files, should be hashed")
	assert.Equal(t, 1, requests)
}

// TestHashArchiveMembersNotPublished covers a build whose tarball upstream has removed: the
// render leaves that file's shasum off rather than failing, the same as a whole-file miss.
func TestHashArchiveMembersNotPublished(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	sums, err := hashArchiveMembers(srv.URL + "/releases/download/v1.37.0%2Brke2r1/rke2.linux-amd64.tar.gz")
	require.NoError(t, err)
	assert.Nil(t, sums)
}

// TestMemberHashesOneArchiveOnce is the point of the in-run member map: nine files out of one
// rke2 tarball cost one download, not nine.
func TestMemberHashesOneArchiveOnce(t *testing.T) {
	var requests int
	srv := serveArchive(t, tarGzFixture(t, map[string]string{
		"bin/rke2":            "the rke2 binary\n",
		"bin/rke2-killall.sh": "#!/bin/sh\nkillall\n",
	}), &requests)
	url := srv.URL + "/releases/download/v1.37.0%2Brke2r1/rke2.linux-amd64.tar.gz"

	s, err := loadExampleShasumsFrom(t)
	require.NoError(t, err)

	binary, err := s.member(url, "bin/rke2")
	require.NoError(t, err)
	assert.Equal(t, sha256Hex("the rke2 binary\n"), binary)

	killall, err := s.member(url, "bin/rke2-killall.sh")
	require.NoError(t, err)
	assert.Equal(t, sha256Hex("#!/bin/sh\nkillall\n"), killall)

	assert.Equal(t, 1, requests, "both members come out of a single pass over the archive")
}

// TestMemberCachesEachMemberSeparately covers what the committed cache has to hold for the
// next run to fetch nothing: one entry per member, keyed by the archive and the path in it.
func TestMemberCachesEachMemberSeparately(t *testing.T) {
	var requests int
	srv := serveArchive(t, tarGzFixture(t, map[string]string{
		"bin/rke2":            "the rke2 binary\n",
		"bin/rke2-killall.sh": "#!/bin/sh\nkillall\n",
	}), &requests)
	url := srv.URL + "/releases/download/v1.37.0%2Brke2r1/rke2.linux-amd64.tar.gz"

	s, err := loadExampleShasumsFrom(t)
	require.NoError(t, err)
	for _, name := range []string{"bin/rke2", "bin/rke2-killall.sh"} {
		_, err := s.member(url, name)
		require.NoError(t, err)
	}

	assert.Equal(t, sha256Hex("the rke2 binary\n"),
		s.sums["v1.37.0%2Brke2r1/rke2.linux-amd64.tar.gz!bin/rke2"].SHA256)
	assert.Equal(t, sha256Hex("#!/bin/sh\nkillall\n"),
		s.sums["v1.37.0%2Brke2r1/rke2.linux-amd64.tar.gz!bin/rke2-killall.sh"].SHA256)
	assert.True(t, s.dirty)

	// A later run starts with the committed entries and no in-run map, which is the state
	// that has to answer without touching the network.
	next := &exampleShasums{
		sums:    s.sums,
		members: map[string]map[string]string{},
	}
	requests = 0
	sum, err := next.member(url, "bin/rke2")
	require.NoError(t, err)
	assert.Equal(t, sha256Hex("the rke2 binary\n"), sum)
	assert.Equal(t, 0, requests, "a cached member must not re-download the archive")
}

// TestMemberIsNotTheArchiveDigest is the defect this whole mechanism exists for: a files entry
// with extractPath declares the digest of what it installs, which is not the digest of the
// archive it came out of.
func TestMemberIsNotTheArchiveDigest(t *testing.T) {
	var requests int
	archive := tarGzFixture(t, map[string]string{"bin/rke2": "the rke2 binary\n"})
	srv := serveArchive(t, archive, &requests)
	url := srv.URL + "/releases/download/v1.37.0%2Brke2r1/rke2.linux-amd64.tar.gz"

	s, err := loadExampleShasumsFrom(t)
	require.NoError(t, err)

	whole, err := s.get(url)
	require.NoError(t, err)
	inside, err := s.member(url, "bin/rke2")
	require.NoError(t, err)

	assert.Equal(t, sha256Hex(string(archive)), whole)
	assert.Equal(t, sha256Hex("the rke2 binary\n"), inside)
	assert.NotEqual(t, whole, inside)

	// Both are kept, under keys that cannot collide.
	assert.Equal(t, whole, s.sums["rke2.linux-amd64.tar.gz"].SHA256)
	assert.Equal(t, inside, s.sums["v1.37.0%2Brke2r1/rke2.linux-amd64.tar.gz!bin/rke2"].SHA256)
}

// TestMemberMissingFromArchive is a template bug, not an upstream change, so it fails the
// render rather than quietly omitting the shasum.
func TestMemberMissingFromArchive(t *testing.T) {
	var requests int
	srv := serveArchive(t, tarGzFixture(t, map[string]string{"bin/rke2": "the rke2 binary\n"}), &requests)

	s, err := loadExampleShasumsFrom(t)
	require.NoError(t, err)

	_, err = s.member(srv.URL+"/rke2.linux-amd64.tar.gz", "bin/rke2-typo.sh")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bin/rke2-typo.sh")
}

// TestMemberNotPublished leaves the shasum off rather than failing the render.
func TestMemberNotPublished(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	s, err := loadExampleShasumsFrom(t)
	require.NoError(t, err)

	sum, err := s.member(srv.URL+"/rke2.linux-amd64.tar.gz", "bin/rke2")
	require.NoError(t, err)
	assert.Empty(t, sum)
	assert.False(t, s.dirty, "a miss is not cached, so a restored URL is picked up next run")
}

// loadExampleShasumsFrom is loadExampleShasums against a cache this test owns, so a test never
// reads or writes the committed example/shasums.json.
func loadExampleShasumsFrom(t *testing.T) (*exampleShasums, error) {
	t.Helper()

	t.Chdir(t.TempDir())
	return loadExampleShasums()
}

func TestArchiveKey(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "release asset keeps the tag above it",
			url:  "https://github.com/rancher/rke2/releases/download/v1.37.0%2Brke2r1/rke2.linux-amd64.tar.gz",
			want: "v1.37.0%2Brke2r1/rke2.linux-amd64.tar.gz",
		},
		{
			name: "a different release of the same asset differs",
			url:  "https://github.com/rancher/rke2/releases/download/v1.36.5%2Brke2r1/rke2.linux-amd64.tar.gz",
			want: "v1.36.5%2Brke2r1/rke2.linux-amd64.tar.gz",
		},
		{
			name: "architecture still differs within one release",
			url:  "https://github.com/rancher/rke2/releases/download/v1.37.0%2Brke2r1/rke2.linux-arm64.tar.gz",
			want: "v1.37.0%2Brke2r1/rke2.linux-arm64.tar.gz",
		},
		{
			name: "no segment above the file name",
			url:  "https://example.com/archive.tar.gz",
			want: "archive.tar.gz",
		},
		{
			name: "query string is not part of the path",
			url:  "https://example.com/v1/archive.tar.gz?token=abc",
			want: "v1/archive.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, archiveKey(tt.url))
		})
	}
}

// TestMemberKeysDoNotCollideAcrossReleases is the defect this key scheme exists for: every rke2
// release publishes its tarball under the same file name, so a key built from the name alone
// would let each version evict the last and leave the cache holding only whichever ran last.
func TestMemberKeysDoNotCollideAcrossReleases(t *testing.T) {
	v1370 := tarGzFixture(t, map[string]string{"bin/rke2": "the v1.37.0 binary\n"})
	v1365 := tarGzFixture(t, map[string]string{"bin/rke2": "the v1.36.5 binary\n"})

	var requests1370, requests1365 int
	srv1370 := serveArchive(t, v1370, &requests1370)
	srv1365 := serveArchive(t, v1365, &requests1365)

	// Both URLs end in the same file name, which is what upstream actually does.
	url1370 := srv1370.URL + "/releases/download/v1.37.0%2Brke2r1/rke2.linux-amd64.tar.gz"
	url1365 := srv1365.URL + "/releases/download/v1.36.5%2Brke2r1/rke2.linux-amd64.tar.gz"

	s, err := loadExampleShasumsFrom(t)
	require.NoError(t, err)

	sum1370, err := s.member(url1370, "bin/rke2")
	require.NoError(t, err)
	sum1365, err := s.member(url1365, "bin/rke2")
	require.NoError(t, err)

	assert.Equal(t, sha256Hex("the v1.37.0 binary\n"), sum1370)
	assert.Equal(t, sha256Hex("the v1.36.5 binary\n"), sum1365)

	// Neither release evicted the other, so a later run answers both without a download.
	assert.Equal(t, sum1370, s.sums["v1.37.0%2Brke2r1/rke2.linux-amd64.tar.gz!bin/rke2"].SHA256)
	assert.Equal(t, sum1365, s.sums["v1.36.5%2Brke2r1/rke2.linux-amd64.tar.gz!bin/rke2"].SHA256)

	next := &exampleShasums{
		sums:    s.sums,
		members: map[string]map[string]string{},
	}
	requests1370, requests1365 = 0, 0
	for url, want := range map[string]string{url1370: sum1370, url1365: sum1365} {
		sum, err := next.member(url, "bin/rke2")
		require.NoError(t, err)
		assert.Equal(t, want, sum)
	}
	assert.Equal(t, 0, requests1370, "v1.37.0's tarball must not be re-downloaded")
	assert.Equal(t, 0, requests1365, "v1.36.5's tarball must not be re-downloaded")
}
