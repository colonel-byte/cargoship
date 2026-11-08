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
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/pkg/archive"
)

// ociFileDistroDir declares one file whose source is an oci:// reference into a real registry.
const ociFileDistroDir = "test/e2e/noncluster/testdata/oci-file"

// The layer the fixture selects, and its size on each platform. The sizes are the assertion
// that matters: they are what makes resolving the wrong platform child a failure rather than a
// pass, and cri-o/bin/pinns is the smallest layer in that artifact whose bytes differ at all
// between platforms.
const (
	pinnsAMD64Size = 794592
	pinnsARM64Size = 663232
	pinnsAMD64SHA  = "055b0f31258293f0ebbfc48e5b26fea552f0a540bfd902902ef1501ff25b41a5"
)

// The zarf injector the fixture's second entry extracts out of components/zarf-injector.tar.
const (
	injectorSize = 1003544
	injectorSHA  = "54c37ca19a101c795786510cb89cf4fe2aa7504d4c399985606a3d8f58f42119"
)

// TestCargoshipCreateOCIFileSource builds a package whose file comes from a vendor-published
// OCI artifact, against the real ghcr.io/cri-o/bundle rather than a synthetic one.
//
// It is the only test here that reaches the network on purpose. The artifact is pinned by the
// digest of its image index, so what it resolves cannot change underneath the suite, and the
// one layer it fetches is 794 KiB.
func TestCargoshipCreateOCIFileSource(t *testing.T) {
	if os.Getenv("CARGOSHIP_E2E_SKIP_NETWORK") != "" {
		t.Skip("CARGOSHIP_E2E_SKIP_NETWORK is set; this test fetches a layer from ghcr.io")
	}

	outDir := t.TempDir()
	_, _, err := e2e.Cargoship(t, "create", ociFileDistroDir, "-o", outDir, "--architecture", "amd64")
	require.NoError(t, err)

	matches, err := filepath.Glob(filepath.Join(outDir, "*.tar.zst"))
	require.NoError(t, err)
	require.Len(t, matches, 1)

	extractDir := t.TempDir()
	const staged = "files/0/pinns"
	require.NoError(t, archive.Decompress(t.Context(), matches[0], extractDir, archive.DecompressOpts{
		Files: []string{staged},
	}))

	got, err := os.ReadFile(filepath.Join(extractDir, filepath.FromSlash(staged)))
	require.NoError(t, err)

	// Asserted on the digest and the size rather than only on the build succeeding: the build
	// already checks the declared shasum, so a size assertion is what distinguishes "the
	// shasum matched" from "the arm64 child was resolved and the shasum happened to match".
	sum := sha256.Sum256(got)
	require.Equal(t, pinnsAMD64SHA, hex.EncodeToString(sum[:]))
	require.Len(t, got, pinnsAMD64Size,
		"staged %d bytes; arm64's copy of this layer is %d, so the wrong platform child was resolved", len(got), pinnsARM64Size)

	// The second entry is a zarf component tarball: an index spelling its platform OS "multi",
	// a titled .tar layer, and a member inside it whose leading path segment is the target's
	// own base name.
	const stagedInjector = "files/1/zarf-injector"
	require.NoError(t, archive.Decompress(t.Context(), matches[0], extractDir, archive.DecompressOpts{
		Files: []string{stagedInjector},
	}))
	injector, err := os.ReadFile(filepath.Join(extractDir, filepath.FromSlash(stagedInjector)))
	require.NoError(t, err)
	injectorSum := sha256.Sum256(injector)
	require.Equal(t, injectorSHA, hex.EncodeToString(injectorSum[:]))
	require.Len(t, injector, injectorSize)
}

// A reference whose layer the artifact does not carry must say so, and say what it does carry.
// Getting this wrong is how an operator spends an afternoon on a typo.
func TestCargoshipCreateOCIFileUnknownLayer(t *testing.T) {
	if os.Getenv("CARGOSHIP_E2E_SKIP_NETWORK") != "" {
		t.Skip("CARGOSHIP_E2E_SKIP_NETWORK is set; this test reads a manifest from ghcr.io")
	}

	defDir := copyDistroDefinition(t, ociFileDistroDir)
	path := filepath.Join(defDir, "distro.yaml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	rewritten := strings.Replace(string(data), "layer: cri-o/bin/pinns", "layer: cri-o/bin/nothing-like-this", 1)
	require.NotEqual(t, string(data), rewritten, "the fixture no longer declares the layer this test rewrites")
	require.NoError(t, os.WriteFile(path, []byte(rewritten), 0o600))

	_, stderr, err := e2e.Cargoship(t, "create", defDir, "-o", t.TempDir(), "--architecture", "amd64")
	require.Error(t, err)
	require.Contains(t, stderr, "no layer titled")
	// The artifact carries 46 titled layers and the message lists the first eight, so this
	// asserts on one that is listed plus the count of the rest -- not on cri-o/bin/pinns,
	// which sorts past the cap.
	require.Contains(t, stderr, "cri-o/bin/crio", "the error must list the titles the artifact does carry")
	require.Contains(t, stderr, "and 38 more")
}
