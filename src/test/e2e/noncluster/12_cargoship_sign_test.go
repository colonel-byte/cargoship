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

	"github.com/colonel-byte/cargoship/src/test"
	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/pkg/archive"
)

// TestCargoshipSign exercises the `sign` command against a local package: producing a
// signature, refusing to re-sign without --overwrite, verifying an existing signature via
// --verify, and its flag error paths.
//
// Signing keys are generated per subtest by cosignKeyPair, so nothing is committed and the
// "wrong key" cases get genuinely unrelated material.
func TestCargoshipSign(t *testing.T) {
	t.Run("signs a local package into an output directory", func(t *testing.T) {
		privPath, _ := cosignKeyPair(t)
		outDir := t.TempDir()

		_, _, err := e2e.Cargoship(t, "sign", minimalPackage(t),
			"--signing-key", privPath, "--signing-key-pass", cosignKeyPassword, "-o", outDir)
		require.NoError(t, err)

		signed := requireSinglePackage(t, outDir)
		extractDir := t.TempDir()
		require.NoError(t, archive.Decompress(t.Context(), signed, extractDir, archive.DecompressOpts{
			Files: []string{"distro.bundle.sig", "distro.yaml"},
		}))
		distroYAML, err := os.ReadFile(filepath.Join(extractDir, "distro.yaml"))
		require.NoError(t, err)
		require.Contains(t, string(distroYAML), "signed: true")
	})

	t.Run("defaults the output to the source directory", func(t *testing.T) {
		privPath, _ := cosignKeyPair(t)
		// Copy first: signing in place rewrites the package next to the source.
		pkgPath := copyPackage(t, minimalPackage(t))
		before, err := os.ReadFile(pkgPath)
		require.NoError(t, err)

		_, _, err = e2e.Cargoship(t, "sign", pkgPath,
			"--signing-key", privPath, "--signing-key-pass", cosignKeyPassword)
		require.NoError(t, err)

		after, err := os.ReadFile(pkgPath)
		require.NoError(t, err)
		require.NotEqual(t, before, after, "signing in place must rewrite the source package")
	})

	t.Run("re-signing requires overwrite", func(t *testing.T) {
		privPath, pubPath := cosignKeyPair(t)
		signedDir := t.TempDir()

		_, _, err := e2e.Cargoship(t, "sign", minimalPackage(t),
			"--signing-key", privPath, "--signing-key-pass", cosignKeyPassword, "-o", signedDir)
		require.NoError(t, err)
		signed := requireSinglePackage(t, signedDir)

		_, _, err = e2e.Cargoship(t, "sign", signed,
			"--signing-key", privPath, "--signing-key-pass", cosignKeyPassword, "-o", t.TempDir())
		require.Error(t, err, "an already-signed package must refuse a re-sign without --overwrite")

		// --verify=always makes the CLI check the existing signature before re-signing, so
		// this run only succeeds if the signature validates against the matching key.
		// The flag takes an optional value, so it must be passed as --verify=always.
		_, _, err = e2e.Cargoship(t, "sign", signed,
			"--signing-key", privPath, "--signing-key-pass", cosignKeyPassword, "-o", t.TempDir(),
			"--overwrite", "--verify=always", "-k", pubPath)
		require.NoError(t, err)
	})

	t.Run("verification against an unrelated key fails", func(t *testing.T) {
		privPath, _ := cosignKeyPair(t)
		_, otherPubPath := cosignKeyPair(t)
		signedDir := t.TempDir()

		_, _, err := e2e.Cargoship(t, "sign", minimalPackage(t),
			"--signing-key", privPath, "--signing-key-pass", cosignKeyPassword, "-o", signedDir)
		require.NoError(t, err)

		_, _, err = e2e.Cargoship(t, "sign", requireSinglePackage(t, signedDir),
			"--signing-key", privPath, "--signing-key-pass", cosignKeyPassword, "-o", t.TempDir(),
			"--overwrite", "--verify=always", "-k", otherPubPath)
		require.Error(t, err)
	})

	t.Run("wrong key password errors", func(t *testing.T) {
		privPath, _ := cosignKeyPair(t)

		_, _, err := e2e.Cargoship(t, "sign", minimalPackage(t),
			"--signing-key", privPath, "--signing-key-pass", "not-the-password", "-o", t.TempDir())
		require.Error(t, err)
	})

	t.Run("no signing key and no keyless errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "sign", minimalPackage(t), "-o", t.TempDir())
		require.Error(t, err)
	})

	t.Run("keyless and signing-key are mutually exclusive", func(t *testing.T) {
		privPath, _ := cosignKeyPair(t)

		_, _, err := e2e.Cargoship(t, "sign", minimalPackage(t), "--keyless",
			"--signing-key", privPath, "-o", t.TempDir())
		require.Error(t, err)
	})

	t.Run("missing source package errors", func(t *testing.T) {
		privPath, _ := cosignKeyPair(t)

		_, _, err := e2e.Cargoship(t, "sign", filepath.Join(t.TempDir(), "nope.tar.zst"),
			"--signing-key", privPath, "--signing-key-pass", cosignKeyPassword, "-o", t.TempDir())
		require.Error(t, err)
	})

	t.Run("no args errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "sign")
		require.Error(t, err)
	})
}

// TestCargoshipSignedRoundTrip signs a package, publishes it to an in-memory registry, and
// pulls it back with signature verification enforced -- the only path where the CLI both
// fetches and cryptographically verifies a signature.
func TestCargoshipSignedRoundTrip(t *testing.T) {
	privPath, pubPath := cosignKeyPair(t)
	_, otherPubPath := cosignKeyPair(t)

	signedDir := t.TempDir()
	_, _, err := e2e.Cargoship(t, "sign", minimalPackage(t),
		"--signing-key", privPath, "--signing-key-pass", cosignKeyPassword, "-o", signedDir)
	require.NoError(t, err)
	signed := requireSinglePackage(t, signedDir)

	addr := test.SetupInMemoryRegistry(t)
	dst, src := ociRefs(addr, "e2e-signed")

	_, _, err = e2e.Cargoship(t, "publish", signed, dst, "--plain-http", "--confirm")
	require.NoError(t, err)

	t.Run("pull verifies against the signing key", func(t *testing.T) {
		pullDir := t.TempDir()
		_, _, err := e2e.Cargoship(t, "pull", src, "--plain-http", "-o", pullDir, "--verify=always", "-k", pubPath)
		require.NoError(t, err)
		requireSinglePackage(t, pullDir)
	})

	t.Run("signing straight to an OCI destination publishes a verifiable package", func(t *testing.T) {
		ociDst, ociSrc := ociRefs(addr, "e2e-sign-to-oci")

		_, _, err := e2e.Cargoship(t, "sign", minimalPackage(t), "-o", ociDst, "--plain-http",
			"--signing-key", privPath, "--signing-key-pass", cosignKeyPassword)
		require.NoError(t, err)

		pullDir := t.TempDir()
		_, _, err = e2e.Cargoship(t, "pull", ociSrc, "--plain-http", "-o", pullDir, "--verify=always", "-k", pubPath)
		require.NoError(t, err)
		requireSinglePackage(t, pullDir)
	})

	t.Run("pull rejects an unrelated key", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "pull", src, "--plain-http", "-o", t.TempDir(), "--verify=always", "-k", otherPubPath)
		require.Error(t, err)
	})
}

// requireSinglePackage asserts dir holds exactly one package archive and returns its path.
func requireSinglePackage(t *testing.T, dir string) string {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(dir, "*.tar.zst"))
	require.NoError(t, err)
	require.Len(t, matches, 1)

	return matches[0]
}
