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

package book

import (
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/magefiles/pkg/repo"
	"github.com/stretchr/testify/require"
)

// TestRebaseLinkLeavesNonPathsAlone covers the targets whose meaning does not depend on which
// directory the page is served from. Rewriting one of these would break a link that was fine.
func TestRebaseLinkLeavesNonPathsAlone(t *testing.T) {
	for _, target := range []string{
		"https://example.com/x",
		"http://example.com/x",
		"//example.com/x",
		"mailto:security@example.com",
		"#a-heading",
	} {
		t.Run(target, func(t *testing.T) {
			got, ok := rebaseLink(".", "docs", target)
			require.True(t, ok, "%s should pass through as resolvable", target)
			require.Equal(t, target, got)
		})
	}
}

// TestRebaseLinkRefusesWhatCannotResolve covers the targets that would 404 once the page is served
// from the site root. Each is refused rather than rewritten, because a link that silently points
// nowhere on a generated page is the failure this generator exists to prevent.
func TestRebaseLinkRefusesWhatCannotResolve(t *testing.T) {
	repo.Chdir(t)

	for _, tt := range []struct {
		name   string
		target string
	}{
		{name: "empty", target: ""},
		{name: "server-rooted", target: "/docs/dev/mage.md"},
		{name: "outside docs", target: "magefiles/build.go"},
		{name: "climbs out of the repository", target: "../../etc/passwd"},
		{name: "inside docs but absent", target: "docs/no-such-page.md"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := rebaseLink(".", "docs", tt.target)
			require.False(t, ok)
			require.Equal(t, tt.target, got, "a refused target is returned unchanged")
		})
	}
}

// TestRebaseLinkRewritesIntoDocs is the case the generator exists for: README.md links to
// docs/dev/mage.md relative to the repository root, and docs/index.md has to link to dev/mage.md
// relative to docs/.
func TestRebaseLinkRewritesIntoDocs(t *testing.T) {
	repo.Chdir(t)

	got, ok := rebaseLink(".", "docs", "docs/dev/mage.md")
	require.True(t, ok)
	require.Equal(t, "dev/mage.md", got)

	// A fragment travels with the link but is not part of the path being checked.
	got, ok = rebaseLink(".", "docs", "docs/dev/mage.md#build-namespace")
	require.True(t, ok)
	require.Equal(t, "dev/mage.md#build-namespace", got)
}

// TestRebaseLinkFromASubdirectory covers .github/SECURITY.md, whose links are relative to
// .github/ rather than to the repository root.
func TestRebaseLinkFromASubdirectory(t *testing.T) {
	repo.Chdir(t)

	got, ok := rebaseLink(".github", "docs", "../docs/dev/mage.md")
	require.True(t, ok)
	require.Equal(t, "dev/mage.md", got)
}

func TestRebaseContentRewritesLinksAndImages(t *testing.T) {
	repo.Chdir(t)

	got, err := rebaseContent("README.md", "docs/index.md", strings.Join([]string{
		"[mage](docs/dev/mage.md)",
		`[titled](docs/dev/mage.md "The mage reference")`,
		"![badge](https://example.com/badge.svg)",
		"[anchor](#section)",
	}, "\n"))
	require.NoError(t, err)

	require.Equal(t, strings.Join([]string{
		"[mage](dev/mage.md)",
		`[titled](dev/mage.md "The mage reference")`,
		"![badge](https://example.com/badge.svg)",
		"[anchor](#section)",
	}, "\n"), got)
}

// TestRebaseContentReportsEveryUnresolvedTarget checks the error names all of them at once and
// says what to do, so one run of the generator is enough to fix a page rather than one per link.
func TestRebaseContentReportsEveryUnresolvedTarget(t *testing.T) {
	repo.Chdir(t)

	_, err := rebaseContent("README.md", "docs/index.md",
		"[a](magefiles/build.go) and [b](hack/config.yaml)")
	require.Error(t, err)
	require.Contains(t, err.Error(), "magefiles/build.go")
	require.Contains(t, err.Error(), "hack/config.yaml")
	require.Contains(t, err.Error(), "docs")
}

// TestBookPagesAreTheTwoPagesOutsideDocs pins the set, since each exists because a reader other
// than mdBook looks for it at a fixed path.
func TestBookPagesAreTheTwoPagesOutsideDocs(t *testing.T) {
	repo.Chdir(t)

	require.Len(t, bookPages, 2)
	for _, p := range bookPages {
		require.FileExists(t, p.source)
		require.FileExists(t, p.page)
	}
}
