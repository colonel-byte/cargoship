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

package noncluster

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sigstore/cosign/v3/pkg/cosign"
	"github.com/stretchr/testify/require"
)

// minimalDistroDir is the tiny, image-free distro definition used by the package-group
// tests. Paths are relative to the repo root, which TestMain chdirs into.
const minimalDistroDir = "src/test/e2e/noncluster/testdata/minimal"

// cosignKeyPassword is the passphrase protecting the ephemeral signing keys handed out
// by cosignKeyPair.
const cosignKeyPassword = "e2e-password"

var (
	minimalPkgOnce sync.Once //nolint:gochecknoglobals
	minimalPkgDir  string    //nolint:gochecknoglobals
	minimalPkgPath string    //nolint:gochecknoglobals
	minimalPkgErr  error     //nolint:gochecknoglobals
)

// minimalPackage builds testdata/minimal once per test binary and returns the path to the
// resulting archive. The package is a few hundred bytes, so every caller sharing one build
// costs less than a second in total; the directory is removed by TestMain after the run.
//
// Callers must treat the returned path as read-only: copy it into t.TempDir() first if a
// test mutates or re-signs the package.
func minimalPackage(t *testing.T) string {
	t.Helper()

	minimalPkgOnce.Do(func() {
		minimalPkgDir, minimalPkgErr = os.MkdirTemp(os.Getenv("CARGOSHIP_E2E_TMPDIR"), "cargoship-e2e-minimal")
		if minimalPkgErr != nil {
			return
		}
		if _, _, minimalPkgErr = e2e.Cargoship(t, "create", minimalDistroDir, "-o", minimalPkgDir); minimalPkgErr != nil {
			return
		}
		var matches []string
		matches, minimalPkgErr = filepath.Glob(filepath.Join(minimalPkgDir, "*.tar.zst"))
		if minimalPkgErr != nil {
			return
		}
		if len(matches) != 1 {
			t.Fatalf("expected exactly one package in %s, got %v", minimalPkgDir, matches)
		}
		minimalPkgPath = matches[0]
	})
	require.NoError(t, minimalPkgErr)

	return minimalPkgPath
}

// copyPackage copies src into a fresh t.TempDir() and returns the new path, so a test can
// sign or overwrite a package without disturbing the shared one from minimalPackage.
func copyPackage(t *testing.T, src string) string {
	t.Helper()

	data, err := os.ReadFile(src)
	require.NoError(t, err)
	dst := filepath.Join(t.TempDir(), filepath.Base(src))
	require.NoError(t, os.WriteFile(dst, data, 0o600))

	return dst
}

// signedPackage signs the shared testdata package with a fresh key pair and returns the
// path to the signed copy together with the public key that verifies it. The signed copy
// lives in t.TempDir(), so callers may re-sign or overwrite it freely.
func signedPackage(t *testing.T) (pkgPath string, pubPath string) {
	t.Helper()

	privPath, pubPath := cosignKeyPair(t)
	outDir := t.TempDir()
	_, _, err := e2e.Cargoship(t, "sign", minimalPackage(t),
		"--signing-key", privPath, "--signing-key-pass", cosignKeyPassword, "-o", outDir)
	require.NoError(t, err)

	return requireSinglePackage(t, outDir), pubPath
}

// cosignKeyPair generates an ephemeral cosign key pair in t.TempDir() and returns the
// private and public key paths. Keys are generated per call rather than committed, so the
// signing tests carry no key material in the repo and two calls yield unrelated keys --
// which is what the "wrong key fails verification" cases rely on.
func cosignKeyPair(t *testing.T) (privPath string, pubPath string) {
	t.Helper()

	keys, err := cosign.GenerateKeyPair(func(bool) ([]byte, error) { return []byte(cosignKeyPassword), nil })
	require.NoError(t, err)

	dir := t.TempDir()
	privPath = filepath.Join(dir, "cosign.key")
	pubPath = filepath.Join(dir, "cosign.pub")
	require.NoError(t, os.WriteFile(privPath, keys.PrivateBytes, 0o600))
	require.NoError(t, os.WriteFile(pubPath, keys.PublicBytes, 0o644))

	return privPath, pubPath
}
