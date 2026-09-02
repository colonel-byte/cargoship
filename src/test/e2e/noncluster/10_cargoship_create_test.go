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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/pkg/archive"
)

// TestCargoshipCreate exercises the `create` command against the image-free testdata
// package: output location, reproducible builds, signing, registry-override parsing, and
// the argument error paths. The real distros under example/ are covered by
// TestCargoshipCreateExample, which is skipped in -short mode.
func TestCargoshipCreate(t *testing.T) {
	t.Run("creates a package named after its metadata", func(t *testing.T) {
		outDir := t.TempDir()
		_, _, err := e2e.Cargoship(t, "create", minimalDistroDir, "-o", outDir)
		require.NoError(t, err)

		matches, err := filepath.Glob(filepath.Join(outDir, "*.tar.zst"))
		require.NoError(t, err)
		require.Len(t, matches, 1)
		require.Equal(t, "cargoship-e2e-minimal-"+e2e.Arch+"-0.0.1.tar.zst", filepath.Base(matches[0]))

		// The archive must carry the package definition and its checksum manifest.
		extractDir := t.TempDir()
		require.NoError(t, archive.Decompress(t.Context(), matches[0], extractDir, archive.DecompressOpts{
			Files: []string{"distro.yaml"},
		}))
		distroYAML, err := os.ReadFile(filepath.Join(extractDir, "distro.yaml"))
		require.NoError(t, err)
		require.Contains(t, string(distroYAML), "name: e2e-minimal")
	})

	t.Run("reproducible builds are byte-identical", func(t *testing.T) {
		first := t.TempDir()
		second := t.TempDir()

		_, _, err := e2e.Cargoship(t, "create", minimalDistroDir, "-o", first, "--reproducible")
		require.NoError(t, err)
		_, _, err = e2e.Cargoship(t, "create", minimalDistroDir, "-o", second, "--reproducible")
		require.NoError(t, err)

		firstBytes := readSinglePackage(t, first)
		secondBytes := readSinglePackage(t, second)
		require.Equal(t, firstBytes, secondBytes, "--reproducible must pin the build timestamp")
	})

	t.Run("signing key produces a signed package", func(t *testing.T) {
		privPath, _ := cosignKeyPair(t)
		outDir := t.TempDir()

		_, _, err := e2e.Cargoship(t, "create", minimalDistroDir, "-o", outDir,
			"--signing-key", privPath, "--signing-key-pass", cosignKeyPassword)
		require.NoError(t, err)

		matches, err := filepath.Glob(filepath.Join(outDir, "*.tar.zst"))
		require.NoError(t, err)
		require.Len(t, matches, 1)

		// A signed package carries the cosign bundle alongside a distro.yaml marked
		// build.signed, and verifies against the matching public key.
		extractDir := t.TempDir()
		require.NoError(t, archive.Decompress(t.Context(), matches[0], extractDir, archive.DecompressOpts{
			Files: []string{"distro.bundle.sig", "distro.yaml"},
		}))
		distroYAML, err := os.ReadFile(filepath.Join(extractDir, "distro.yaml"))
		require.NoError(t, err)
		require.Contains(t, string(distroYAML), "signed: true")
		// The signature itself is verified cryptographically in TestCargoshipSign.
	})

	t.Run("registry override without an equals sign errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "create", minimalDistroDir, "-o", t.TempDir(),
			"--registry-override", "docker.io")
		require.Error(t, err)
	})

	t.Run("registry override with an empty source errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "create", minimalDistroDir, "-o", t.TempDir(),
			"--registry-override", "=registry.example.com")
		require.Error(t, err)
	})

	t.Run("registry override with a duplicate source errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "create", minimalDistroDir, "-o", t.TempDir(),
			"--registry-override", "docker.io=a.example.com,docker.io=b.example.com")
		require.Error(t, err)
	})

	// --skip-sbom was removed because distro.Create hardcoded SkipSBOM and never read
	// the flag. Assert it is gone rather than silently accepted again.
	t.Run("skip-sbom is not a flag", func(t *testing.T) {
		_, stderr, err := e2e.Cargoship(t, "create", minimalDistroDir, "-o", t.TempDir(), "--skip-sbom")
		require.Error(t, err)
		require.Contains(t, stderr, "unknown flag: --skip-sbom")
	})

	t.Run("nonexistent directory errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "create", filepath.Join(t.TempDir(), "nope"), "-o", t.TempDir())
		require.Error(t, err)
	})

	t.Run("too many args errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "create", minimalDistroDir, minimalDistroDir, "-o", t.TempDir())
		require.Error(t, err)
	})
}

// TestCargoshipCreateExample builds a real distro from example/, which downloads the rke2
// release tarball and every pinned image -- roughly 1.5GB. Skipped under -short so PR CI
// can run the rest of the suite in seconds.
func TestCargoshipCreateExample(t *testing.T) {
	if testing.Short() {
		t.Skip("example packages download ~1.5GB of engine artifacts and images")
	}
	t.Setenv("CARGOSHIP_CONFIG", "src/test/e2e/cargoship-config.yaml")

	_, _, err := e2e.Cargoship(t, "create", "example/rke2-cilium/v1_35/v1.35.0-rke2r1")
	require.NoError(t, err)
}

// readSinglePackage returns the bytes of the one archive expected in dir.
func readSinglePackage(t *testing.T, dir string) []byte {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(dir, "*.tar.zst"))
	require.NoError(t, err)
	require.Len(t, matches, 1)
	data, err := os.ReadFile(matches[0])
	require.NoError(t, err)

	return data
}
