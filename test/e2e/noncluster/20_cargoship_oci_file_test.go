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

// TestCargoshipSha256SumOCIReference checks that `dev sha256sum` over an oci:// reference
// produces the digest the fixture declares for the same entry.
//
// Asserting against the fixture rather than against a constant is the point: these are the two
// halves of one workflow -- the command computes what an operator pastes into `shasum`, and the
// build refuses the entry if it does not match. Reading both from one file means they cannot
// drift apart without this failing.
func TestCargoshipSha256SumOCIReference(t *testing.T) {
	if os.Getenv("CARGOSHIP_E2E_SKIP_NETWORK") != "" {
		t.Skip("CARGOSHIP_E2E_SKIP_NETWORK is set; this test reads layers from ghcr.io")
	}

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "a titled layer",
			args: []string{"--layer", "cri-o/bin/pinns", "--arch", "amd64"},
			want: pinnsAMD64SHA,
		},
		{
			name: "a member of a layer that is an archive",
			args: []string{"--layer", "components/zarf-injector.tar", "--extract-path", "zarf-injector/files/0/zarf-injector", "--arch", "amd64"},
			want: injectorSHA,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := fixtureFileSource(t, tc.want)
			stdout, _, err := e2e.Cargoship(t, append([]string{"dev", "sha256sum", source}, tc.args...)...)
			require.NoError(t, err)
			require.Equal(t, tc.want, strings.TrimSpace(lastLine(stdout)),
				"the computed digest must equal the one the fixture declares for this entry")
		})
	}
}

// An index covering several architectures cannot be read without one being named, and the
// message has to name the flag: an operator computing a digest for an amd64 entry on an arm64
// workstation would otherwise get a plausible wrong answer with nothing to notice.
func TestCargoshipSha256SumOCIRequiresArch(t *testing.T) {
	if os.Getenv("CARGOSHIP_E2E_SKIP_NETWORK") != "" {
		t.Skip("CARGOSHIP_E2E_SKIP_NETWORK is set; this test reads an index from ghcr.io")
	}

	source := fixtureFileSource(t, pinnsAMD64SHA)
	_, stderr, err := e2e.Cargoship(t, "dev", "sha256sum", source, "--layer", "cri-o/bin/pinns")
	require.Error(t, err)
	require.Contains(t, stderr, "--arch")
	require.Contains(t, stderr, "amd64, arm64", "the error must name the architectures the index does hold")
}

// An artifact carrying many layers needs one named, and the message has to list what is there.
func TestCargoshipSha256SumOCIRequiresLayer(t *testing.T) {
	if os.Getenv("CARGOSHIP_E2E_SKIP_NETWORK") != "" {
		t.Skip("CARGOSHIP_E2E_SKIP_NETWORK is set; this test reads a manifest from ghcr.io")
	}

	source := fixtureFileSource(t, pinnsAMD64SHA)
	_, stderr, err := e2e.Cargoship(t, "dev", "sha256sum", source, "--arch", "amd64")
	require.Error(t, err)
	require.Contains(t, stderr, "--layer")
	require.Contains(t, stderr, "cri-o/bin/crio", "the error must list the titles the artifact carries")
}

// fixtureFileSource returns the source of the fixture entry declaring shasum, so a test reads
// the reference from the same place the build does rather than repeating it.
func fixtureFileSource(t *testing.T, shasum string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(ociFileDistroDir, "distro.yaml"))
	require.NoError(t, err)

	var source string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(trimmed, "source: "); ok {
			source = after
		}
		if trimmed == "shasum: "+shasum {
			require.NotEmpty(t, source, "found the shasum before any source line")
			return source
		}
	}
	t.Fatalf("no fixture entry declares shasum %s", shasum)
	return ""
}

// lastLine returns the final non-empty line of s, which is where the digest is printed.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// TestCargoshipSha256SumArchCompletion checks that --arch tab completion reads the reference
// being checksummed and offers the architectures it actually holds.
//
// The assertion is on ppc64le and s390x on purpose: cargoship's own architecture list is
// amd64, arm64 and riscv64, so those two can only have come from the artifact's index. A
// completion that had silently fallen back to the static list would still look plausible.
func TestCargoshipSha256SumArchCompletion(t *testing.T) {
	if os.Getenv("CARGOSHIP_E2E_SKIP_NETWORK") != "" {
		t.Skip("CARGOSHIP_E2E_SKIP_NETWORK is set; this test reads an index from ghcr.io")
	}

	source := fixtureFileSource(t, pinnsAMD64SHA)
	stdout, _, err := e2e.Cargoship(t, completionArgs(source)...)
	require.NoError(t, err)

	for _, want := range []string{"amd64\tlinux/amd64", "arm64\tlinux/arm64", "ppc64le\tlinux/ppc64le", "s390x\tlinux/s390x"} {
		require.Contains(t, stdout, want)
	}
	require.NotContains(t, stdout, "the x86-64, 64-bit AMD, architecture",
		"the static architecture list was offered, so the reference was not read")
}

// A zarf package index spells its platform "multi" and carries a "skeleton" child that is not
// an architecture any node runs. Completion reports what the artifact says rather than what
// cargoship would prefer it said, because the flag has to be given a value the index matches.
func TestCargoshipSha256SumArchCompletionZarfIndex(t *testing.T) {
	if os.Getenv("CARGOSHIP_E2E_SKIP_NETWORK") != "" {
		t.Skip("CARGOSHIP_E2E_SKIP_NETWORK is set; this test reads an index from ghcr.io")
	}

	source := fixtureFileSource(t, injectorSHA)
	stdout, _, err := e2e.Cargoship(t, completionArgs(source)...)
	require.NoError(t, err)

	require.Contains(t, stdout, "amd64\tmulti/amd64")
	require.Contains(t, stdout, "skeleton\tmulti/skeleton")
}

// completionArgs builds a cobra __complete invocation asking what --arch can take for source.
//
// --no-color is passed explicitly rather than left to the helper, which appends it: under
// __complete the final argument is the word being completed, so an appended flag makes cobra
// suggest flag names instead of ever reaching the callback.
func completionArgs(source string) []string {
	return []string{"__complete", "dev", "sha256sum", source, "--no-color", "--arch", ""}
}

// TestCargoshipSha256SumLayerCompletion checks that --layer tab completion reads the reference
// and offers the titles the artifact actually carries.
func TestCargoshipSha256SumLayerCompletion(t *testing.T) {
	if os.Getenv("CARGOSHIP_E2E_SKIP_NETWORK") != "" {
		t.Skip("CARGOSHIP_E2E_SKIP_NETWORK is set; this test reads a manifest from ghcr.io")
	}

	source := fixtureFileSource(t, pinnsAMD64SHA)
	stdout, _, err := e2e.Cargoship(t, "__complete", "dev", "sha256sum", source,
		"--arch", "amd64", "--no-color", "--layer", "cri-o/bin/p")
	require.NoError(t, err)

	require.Contains(t, stdout, "cri-o/bin/pinns")
	require.NotContains(t, stdout, "cri-o/README.md", "suggestions must be narrowed to what has been typed")
	require.NotContains(t, stdout, activeHelpPrefix, "an architecture was given, so nothing was assumed")
	require.NotContains(t, stdout, "pass --arch", "an architecture was given, so no entry should nag about it")
}

// With no --arch on a multi-platform reference there is no safe child to read, so one is read
// anyway and the operator is told which. An empty completion would be defensible and teaches
// nothing; the titles are identical across platforms on every artifact this has been used
// against, and a title that turns out wrong fails loudly at fetch time.
func TestCargoshipSha256SumLayerCompletionWithoutArch(t *testing.T) {
	if os.Getenv("CARGOSHIP_E2E_SKIP_NETWORK") != "" {
		t.Skip("CARGOSHIP_E2E_SKIP_NETWORK is set; this test reads a manifest from ghcr.io")
	}

	source := fixtureFileSource(t, pinnsAMD64SHA)
	stdout, _, err := e2e.Cargoship(t, "__complete", "dev", "sha256sum", source,
		"--no-color", "--layer", "cri-o/bin/p")
	require.NoError(t, err)

	// Said through both channels. The ActiveHelp line is the idiomatic one, and cobra's
	// generated fish script runs __complete with ACTIVE_HELP=0 -- so the description carried
	// on the suggestion itself is what a fish user sees, and it has to be there too.
	require.Contains(t, stdout, "cri-o/bin/pinns\tfrom the amd64 manifest; pass --arch")
	require.Contains(t, stdout, activeHelpPrefix+" these titles are from the amd64 manifest; pass --arch for consistent results")
}

// A reference cargoship cannot read has no layer titles to offer, and unlike --arch there is
// no generic list to fall back to: a title is entirely a property of the artifact.
func TestCargoshipSha256SumLayerCompletionOffersNothingForALocalPath(t *testing.T) {
	stdout, _, err := e2e.Cargoship(t, "__complete", "dev", "sha256sum", "./artifact.tar.gz",
		"--no-color", "--layer", "")
	require.NoError(t, err)
	require.Equal(t, ":4", strings.TrimSpace(stdout), "only cobra's directive line, with no suggestions")
}

// activeHelpPrefix is how cobra marks a line shells render as a non-selectable hint.
const activeHelpPrefix = "_activeHelp_"

// TestCargoshipSha256SumExtractPathCompletion checks that --extract-path completion lists the
// members of the chosen layer.
//
// The fixture's own extractPath is the assertion: the completion has to offer the path the
// definition declares, because those are the same string written twice -- once by an operator
// who tabbed for it, once by the build that extracts it.
func TestCargoshipSha256SumExtractPathCompletion(t *testing.T) {
	if os.Getenv("CARGOSHIP_E2E_SKIP_NETWORK") != "" {
		t.Skip("CARGOSHIP_E2E_SKIP_NETWORK is set; this test reads a layer from ghcr.io")
	}

	source := fixtureFileSource(t, injectorSHA)
	stdout, _, err := e2e.Cargoship(t, "__complete", "dev", "sha256sum", source,
		"--arch", "amd64", "--layer", "components/zarf-injector.tar",
		"--no-color", "--extract-path", "")
	require.NoError(t, err)
	require.Contains(t, stdout, "zarf-injector/files/0/zarf-injector")
}

// Without an architecture the platform would have to be assumed, and an assumed platform means
// downloading a layer the operator never asked for. Unlike --layer, which guesses and says so,
// this one declines.
func TestCargoshipSha256SumExtractPathCompletionNeedsBothFlags(t *testing.T) {
	source := fixtureFileSource(t, injectorSHA)

	for _, args := range [][]string{
		{"--layer", "components/zarf-injector.tar"},
		{"--arch", "amd64"},
	} {
		stdout, _, err := e2e.Cargoship(t, append(append([]string{"__complete", "dev", "sha256sum", source}, args...),
			"--no-color", "--extract-path", "")...)
		require.NoError(t, err)
		require.Equal(t, ":4", strings.TrimSpace(stdout), "only cobra's directive line, with no suggestions")
	}
}
