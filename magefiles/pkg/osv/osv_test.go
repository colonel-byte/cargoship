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

package osv

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/colonel-byte/cargoship/magefiles/pkg/repo"
	"github.com/stretchr/testify/require"
)

// TestCommentWrapsAtTheDeclaredWidth checks the wrap the generated file depends on for
// readability. The paragraphs are built by substitution, so nothing in the source shows their
// eventual width and only a test can hold it.
func TestCommentWrapsAtTheDeclaredWidth(t *testing.T) {
	long := strings.TrimSpace(strings.Repeat("alpha bravo charlie delta echo foxtrot ", 12))

	got := osvComment(long)

	require.True(t, strings.HasSuffix(got, "\n"), "every comment line is terminated")
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	require.Greater(t, len(lines), 1, "a paragraph this long has to wrap")

	var words []string
	for _, line := range lines {
		require.True(t, strings.HasPrefix(line, "# "), "line %q is a TOML comment", line)
		require.LessOrEqual(t, len(line), osvCommentWidth, "line %q is within the width", line)
		words = append(words, strings.Fields(strings.TrimPrefix(line, "#"))...)
	}
	// Wrapping rearranges the whitespace and nothing else.
	require.Equal(t, strings.Fields(long), words)
}

// TestCommentSeparatesParagraphs pins the bare "#" between paragraphs. Without it the three
// paragraphs of an override render as one wall of text.
func TestCommentSeparatesParagraphs(t *testing.T) {
	require.Equal(t, "# one\n#\n# two\n", osvComment("one", "two"))
}

// TestCommentKeepsAWordLongerThanTheWidth checks the loop neither drops nor splits a word it
// cannot fit -- a long URL in a paragraph would otherwise be silently mangled. It overruns the
// width instead, and flushes the empty line it was holding first, so the output opens with a bare
// "#". Harmless in a comment block, and pinned here so the shape is a decision rather than a
// surprise.
func TestCommentKeepsAWordLongerThanTheWidth(t *testing.T) {
	word := strings.Repeat("x", osvCommentWidth*2)

	require.Equal(t, "#\n# "+word+"\n", osvComment(word))
}

// packageOverrides is the shape osv-scanner reads back out of a generated file.
type packageOverrides struct {
	PackageOverrides []struct {
		Ecosystem string `toml:"ecosystem"`
		Ignore    bool   `toml:"ignore"`
		Reason    string `toml:"reason"`
	} `toml:"PackageOverrides"`
}

// TestOverrideContentRoundTrips decodes a rendered override back through a TOML parser, so the
// comment wrapping and the quoting of the reason cannot produce a file that osv-scanner would
// reject or, worse, read as an empty config and scan anyway.
func TestOverrideContentRoundTrips(t *testing.T) {
	o := osvOverride{
		dir:       "vendor/example.com/mod",
		manifest:  "requirements.txt",
		ecosystem: "PyPI",
		what:      `the "docs" toolchain, which nothing here builds`,
	}

	var got packageOverrides
	_, err := toml.Decode(osvOverrideContent(o), &got)
	require.NoError(t, err)

	require.Len(t, got.PackageOverrides, 1)
	require.Equal(t, "PyPI", got.PackageOverrides[0].Ecosystem)
	require.True(t, got.PackageOverrides[0].Ignore)
	require.Contains(t, got.PackageOverrides[0].Reason, "Python tooling vendored alongside a Go module")

	// The manifest and its description are in the comment rather than the config, so the decode
	// above cannot see them.
	require.Contains(t, osvOverrideContent(o), "requirements.txt here is "+`the "docs" toolchain`)
}

// TestEcosystemLanguage covers both the two named ecosystems and the fallback, which is what a
// future entry for an ecosystem nobody has mapped yet renders as.
func TestEcosystemLanguage(t *testing.T) {
	require.Equal(t, "JavaScript", osvEcosystemLanguage("npm"))
	require.Equal(t, "Python", osvEcosystemLanguage("PyPI"))
	require.Equal(t, "CratesIO", osvEcosystemLanguage("CratesIO"))
}

// TestForeignManifest covers the classification Verify's second half rests on. A false negative
// is the expensive direction: an uncovered lockfile slips into vendor/ and the Scorecard score
// falls on some later run with nothing in that day's diff to explain it.
func TestForeignManifest(t *testing.T) {
	foreign := []string{
		"package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lock",
		"Pipfile.lock", "poetry.lock", "pdm.lock", "uv.lock",
		"Gemfile.lock", "Cargo.lock", "composer.lock", "mix.lock",
		"pubspec.lock", "conan.lock", "gradle.lockfile", "packages.lock.json",
		"pom.xml",
		// The requirements rule is a prefix-agnostic contains, so every spelling a module
		// has actually shipped is caught.
		"requirements.txt", "requirements-test.txt", "requirements-docs.txt",
		"test-requirements.txt",
	}
	for _, name := range foreign {
		require.True(t, osvForeignManifest(name), "%s is a non-Go manifest", name)
	}

	native := []string{
		"go.mod", "go.sum", "vendor.json", "LICENSE", "README.md", "main.go",
		// Not a lockfile: prose about requirements, and a .in that pip compiles from.
		"requirements.md", "requirements.in",
		// osv-scanner reads package.json only through its lockfile.
		"package.json",
	}
	for _, name := range native {
		require.False(t, osvForeignManifest(name), "%s is not a manifest osv-scanner extracts", name)
	}
}

// TestCheckedInOverridesAreCurrent runs the verification against the real vendor tree, which is
// the same check CI makes. It fails when an entry drifts from what the renderer produces, or when
// a dependency bump drags in a lockfile nothing covers.
func TestCheckedInOverridesAreCurrent(t *testing.T) {
	repo.Chdir(t)

	require.NoError(t, Verify())
}
