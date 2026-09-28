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

package rpmrepo

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

const testPrimaryXML = `<?xml version="1.0" encoding="UTF-8"?>
<metadata xmlns="http://linux.duke.edu/metadata/common" packages="3">
  <package type="rpm">
    <name>kubelet</name>
    <arch>x86_64</arch>
    <version epoch="0" ver="1.35.2" rel="0"/>
    <checksum type="sha256" pkgid="YES">dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd</checksum>
    <location href="Packages/k/kubelet-1.35.2-0.x86_64.rpm"/>
  </package>
  <package type="rpm">
    <name>kubelet</name>
    <arch>x86_64</arch>
    <version epoch="0" ver="1.35.3" rel="0"/>
    <checksum type="sha256" pkgid="YES">eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee</checksum>
    <location href="Packages/k/kubelet-1.35.3-0.x86_64.rpm"/>
  </package>
  <package type="rpm">
    <name>kubelet</name>
    <arch>aarch64</arch>
    <version epoch="0" ver="1.35.1" rel="0"/>
    <checksum type="sha256" pkgid="YES">ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff</checksum>
    <location href="Packages/k/kubelet-1.35.1-0.aarch64.rpm"/>
  </package>
</metadata>
`

// compressor encodes the body of a served primary.xml, however the test needs it. The encoding
// happens before the server starts rather than inside the handler, so a failure is reported on
// the test's own goroutine where t.Fatal works.
type compressor func(t *testing.T, b []byte) []byte

// newTestRepo serves repomd.xml plus a primary.xml compressed the way name's extension says, in
// the directory shape FetchPrimary expects (baseURL/repodata/*).
func newTestRepo(t *testing.T, name string, compress compressor) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repodata/repomd.xml", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<repomd xmlns="http://linux.duke.edu/metadata/repo">
  <data type="primary">
    <location href="repodata/%s"/>
  </data>
</repomd>
`, name)
	})
	body := compress(t, []byte(testPrimaryXML))
	mux.HandleFunc("/repodata/"+name, func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body) //nolint:errcheck // a client that hung up is not this test's business
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func gzipTo(t *testing.T, b []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err := gz.Write(b)
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func zstdTo(t *testing.T, b []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	require.NoError(t, err)
	_, err = zw.Write(b)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// TestFetchPrimary covers both compressions, because pkgs.k8s.io publishes gzip and
// download.docker.com's centos repo publishes zstd.
func TestFetchPrimary(t *testing.T) {
	for name, compress := range map[string]compressor{
		"primary.xml.gz":  gzipTo,
		"primary.xml.zst": zstdTo,
	} {
		t.Run(name, func(t *testing.T) {
			srv := newTestRepo(t, name, compress)

			primary, err := FetchPrimary(t.Context(), srv.Client(), srv.URL)
			require.NoError(t, err)
			require.Len(t, primary.Packages, 3)
			require.Equal(t, "kubelet", primary.Packages[0].Name)
		})
	}
}

func TestFetchPrimaryReportsAnUnusableRepo(t *testing.T) {
	t.Run("an unsupported compression", func(t *testing.T) {
		// Served uncompressed: FetchPrimary has to reject the extension before it reads a byte.
		srv := newTestRepo(t, "primary.xml.bz2", func(_ *testing.T, b []byte) []byte { return b })
		_, err := FetchPrimary(t.Context(), srv.Client(), srv.URL)
		require.ErrorContains(t, err, "unsupported primary metadata compression")
	})

	t.Run("no primary element", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `<repomd><data type="filelists"><location href="x.gz"/></data></repomd>`)
		}))
		t.Cleanup(srv.Close)

		_, err := FetchPrimary(t.Context(), srv.Client(), srv.URL)
		require.ErrorContains(t, err, "no primary data element")
	})

	t.Run("a missing repository", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		t.Cleanup(srv.Close)

		_, err := FetchPrimary(t.Context(), srv.Client(), srv.URL)
		require.ErrorContains(t, err, "404")
	})
}

func testPrimary(t *testing.T) *Primary {
	t.Helper()
	var primary Primary
	require.NoError(t, xml.Unmarshal([]byte(testPrimaryXML), &primary))
	return &primary
}

func TestFindLatest(t *testing.T) {
	primary := testPrimary(t)

	// No architecture filter, so the newest of any architecture wins.
	pkg, err := FindLatest(primary, "kubelet")
	require.NoError(t, err)
	require.Equal(t, "1.35.3", pkg.Version.Ver)

	_, err = FindLatest(primary, "nothing-here")
	require.ErrorContains(t, err, `package "nothing-here" not found`)
}

func TestFindLatestArch(t *testing.T) {
	primary := testPrimary(t)

	pkg, err := FindLatestArch(primary, "kubelet", "x86_64")
	require.NoError(t, err)
	require.Equal(t, "1.35.3", pkg.Version.Ver, "the newer of the two x86_64 builds")
	require.Equal(t, "Packages/k/kubelet-1.35.3-0.x86_64.rpm", pkg.Location.Href)

	// The architecture narrows the search rather than just ordering it.
	pkg, err = FindLatestArch(primary, "kubelet", "aarch64")
	require.NoError(t, err)
	require.Equal(t, "1.35.1", pkg.Version.Ver)

	_, err = FindLatestArch(primary, "kubelet", "riscv64")
	require.ErrorContains(t, err, "not found for riscv64")
}

// TestPickArch is what a backfill uses: the newest package at or before a tag, rather than the
// newest on the line.
func TestPickArch(t *testing.T) {
	primary := testPrimary(t)

	pkg, err := PickArch(primary, "kubelet", "x86_64", "1.35.2")
	require.NoError(t, err)
	require.Equal(t, "1.35.2", pkg.Version.Ver, "1.35.3 is newer than the target, so it is skipped")

	pkg, err = PickArch(primary, "kubelet", "x86_64", "1.35.9")
	require.NoError(t, err)
	require.Equal(t, "1.35.3", pkg.Version.Ver, "nothing reaches the target, so the newest below it wins")

	_, err = PickArch(primary, "kubelet", "x86_64", "1.34.0")
	require.ErrorContains(t, err, "at or before 1.34.0")
}
