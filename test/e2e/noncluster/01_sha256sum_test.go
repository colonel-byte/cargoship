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

// Package test provides e2e tests for cargoship
package noncluster

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/pkg/archive"
)

// TestCargoshipSha256Sum exercises the `sha256sum` command against a plain file, a URL, its
// `sum` alias, extracting a member out of an archive, and its error paths.
func TestCargoshipSha256Sum(t *testing.T) {
	t.Run("hashes a plain file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "hello.txt")
		require.NoError(t, os.WriteFile(path, []byte("hello cargoship\n"), 0o644))

		stdout, _, err := e2e.Cargoship(t, "sha256sum", path)
		require.NoError(t, err)
		require.Equal(t, sha256File(t, path), strings.TrimSpace(stdout))
	})

	t.Run("sum alias produces the same hash", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "hello.txt")
		require.NoError(t, os.WriteFile(path, []byte("same result via alias\n"), 0o644))

		stdout, _, err := e2e.Cargoship(t, "sum", path)
		require.NoError(t, err)
		require.Equal(t, sha256File(t, path), strings.TrimSpace(stdout))
	})

	t.Run("hashes a remote file over http", func(t *testing.T) {
		const body = "downloaded by the sha256sum url branch\n"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			//nolint:errcheck // test server write to an in-process client
			w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)

		sum := sha256.Sum256([]byte(body))

		stdout, _, err := e2e.Cargoship(t, "sha256sum", srv.URL+"/artifact.txt")
		require.NoError(t, err)
		require.Equal(t, hex.EncodeToString(sum[:]), strings.TrimSpace(stdout))
	})

	t.Run("extract path hashes a file inside the archive", func(t *testing.T) {
		archivePath := minimalPackage(t)

		extractDir := t.TempDir()
		require.NoError(t, archive.Decompress(t.Context(), archivePath, extractDir, archive.DecompressOpts{
			Files: []string{"checksums.txt"},
		}))
		want := sha256File(t, filepath.Join(extractDir, "checksums.txt"))

		stdout, _, err := e2e.Cargoship(t, "sha256sum", "--extract-path", "checksums.txt", archivePath)
		require.NoError(t, err)
		require.Equal(t, want, strings.TrimSpace(stdout))
	})

	t.Run("-e shorthand hashes the same member", func(t *testing.T) {
		archivePath := minimalPackage(t)

		want, _, err := e2e.Cargoship(t, "sha256sum", "--extract-path", "distro.yaml", archivePath)
		require.NoError(t, err)

		stdout, _, err := e2e.Cargoship(t, "sha256sum", "-e", "distro.yaml", archivePath)
		require.NoError(t, err)
		require.Equal(t, strings.TrimSpace(want), strings.TrimSpace(stdout))
	})

	t.Run("extract path naming a missing member errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "sha256sum", "--extract-path", "not-in-the-archive.txt", minimalPackage(t))
		require.Error(t, err)
	})

	t.Run("missing file errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "sha256sum", filepath.Join(t.TempDir(), "does-not-exist"))
		require.Error(t, err)
	})

	t.Run("no args errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "sha256sum")
		require.Error(t, err)
	})
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
