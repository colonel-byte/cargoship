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

package distro

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// makeTarBytes returns the bytes of a minimal, valid tar archive containing one empty
// file, so mimetype.DetectFile can recognize it as application/x-tar via its checksum.
func makeTarBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "file.txt", Size: 0, Mode: 0o600}))
	require.NoError(t, tw.Close())
	return buf.Bytes()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestPullValidation(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		destination string
		wantErr     string
	}{
		{
			name:        "empty destination",
			source:      "http://example.com/package.tar.zst",
			destination: "",
			wantErr:     "no output directory specified",
		},
		{
			name:        "missing scheme",
			source:      "example.com/package.tar.zst",
			destination: t.TempDir(),
			wantErr:     "scheme must be either oci:// or http(s)://",
		},
		{
			name:        "empty host",
			source:      "oci://",
			destination: t.TempDir(),
			wantErr:     "host cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Pull(context.Background(), tt.source, tt.destination, PullOptions{})
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestPullHTTPFile(t *testing.T) {
	t.Run("downloads file contents", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if _, err := w.Write([]byte("package-bytes")); err != nil {
				t.Fatal(err)
			}
		}))
		defer srv.Close()

		dst := filepath.Join(t.TempDir(), "data")
		err := pullHTTPFile(context.Background(), srv.URL, dst, false)
		require.NoError(t, err)

		got, err := os.ReadFile(dst)
		require.NoError(t, err)
		require.Equal(t, "package-bytes", string(got))
	})

	t.Run("non-200 status returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		dst := filepath.Join(t.TempDir(), "data")
		err := pullHTTPFile(context.Background(), srv.URL, dst, false)
		require.Error(t, err)
		require.ErrorContains(t, err, "404")
	})
}

func TestPullHTTP(t *testing.T) {
	t.Run("empty shasum errors before any request", func(t *testing.T) {
		_, err := pullHTTP(context.Background(), "http://example.invalid/pkg.tar", t.TempDir(), "", false)
		require.Error(t, err)
		require.ErrorContains(t, err, "shasum cannot be empty")
	})

	t.Run("shasum mismatch errors", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if _, err := w.Write([]byte("some tar content")); err != nil {
				t.Fatal(err)
			}
		}))
		defer srv.Close()

		_, err := pullHTTP(context.Background(), srv.URL, t.TempDir(), "deadbeef", false)
		require.Error(t, err)
		require.ErrorContains(t, err, "shasum mismatch")
	})

	t.Run("tar content is renamed to data.tar", func(t *testing.T) {
		content := makeTarBytes(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if _, err := w.Write(content); err != nil {
				t.Fatal(err)
			}
		}))
		defer srv.Close()

		sum := sha256Hex(content)

		tarDir := t.TempDir()
		path, err := pullHTTP(context.Background(), srv.URL, tarDir, sum, false)
		require.NoError(t, err)
		require.Equal(t, filepath.Join(tarDir, "data.tar"), path)
	})

	t.Run("unsupported file type errors", func(t *testing.T) {
		content := []byte("not a tar or zst file, just text")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if _, err := w.Write(content); err != nil {
				t.Fatal(err)
			}
		}))
		defer srv.Close()

		sum := sha256Hex(content)

		_, err := pullHTTP(context.Background(), srv.URL, t.TempDir(), sum, false)
		require.Error(t, err)
		require.ErrorContains(t, err, "unsupported file type")
	})
}
