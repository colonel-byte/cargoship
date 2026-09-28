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

package golangdoc

import (
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/magefiles/pkg/repo"
	"github.com/stretchr/testify/require"
)

// TestSummaryLinesIndentsByDocumentedAncestor covers the rule the sidebar depends on: a page is
// stepped in once per ancestor that has a page of its own, not once per path segment. pkg/coci/zoci
// nests under pkg/coci because pkg/coci is documented; internal/foo/bar does not nest twice when
// internal/foo has no page.
func TestSummaryLinesIndentsByDocumentedAncestor(t *testing.T) {
	require.Equal(t, []string{
		"- [action](golang/pkg/action.md)",
		"- [coci](golang/pkg/coci.md)",
		"  - [zoci](golang/pkg/coci/zoci.md)",
		"- [bar](golang/pkg/foo/bar.md)",
	}, summaryLines([]string{
		"pkg/action",
		"pkg/coci",
		"pkg/coci/zoci",
		"pkg/foo/bar",
	}))
}

// TestSummaryLinesNestsRepeatedly checks the depth accumulates rather than being capped at one.
func TestSummaryLinesNestsRepeatedly(t *testing.T) {
	require.Equal(t, []string{
		"- [a](golang/a.md)",
		"  - [b](golang/a/b.md)",
		"    - [c](golang/a/b/c.md)",
	}, summaryLines([]string{"a", "a/b", "a/b/c"}))
}

func TestSummaryLinesEmpty(t *testing.T) {
	require.Empty(t, summaryLines(nil))
}

// TestSummaryReadsTheCheckedInTree runs the walk against the real docs/golang, so the pairing of
// the walk with summaryLines is covered and not just the indentation rule on its own.
func TestSummaryReadsTheCheckedInTree(t *testing.T) {
	repo.Chdir(t)

	lines, err := Summary()
	require.NoError(t, err)
	require.NotEmpty(t, lines, "docs/golang is checked in, so the walk should find pages")

	for _, line := range lines {
		require.True(t, strings.HasSuffix(line, ".md)"), "%q", line)
		require.Contains(t, line, "](golang/")
	}

	// Sorted on the key with .md stripped, so a package's own page precedes its subpackages'.
	require.True(t, sortedByIndent(lines), "a nested entry has to follow its parent")
}

// sortedByIndent reports whether every indented line follows a line less deeply indented than
// itself by at most one level, which is what "nested under its parent" looks like in the output.
func sortedByIndent(lines []string) bool {
	previous := 0
	for _, line := range lines {
		depth := (len(line) - len(strings.TrimLeft(line, " "))) / 2
		if depth > previous+1 {
			return false
		}
		previous = depth
	}
	return true
}

// TestRootsAreTheDocumentedTrees pins the set of module directories the chapter covers, so
// dropping one is a deliberate edit rather than a silent gap in the reference.
func TestRootsAreTheDocumentedTrees(t *testing.T) {
	require.ElementsMatch(t, []string{"pkg", "api", "types"}, Roots)
}
