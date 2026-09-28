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

package summary

import (
	"os"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/magefiles/pkg/repo"
	"github.com/stretchr/testify/require"
)

// TestRenderMatchesTheCheckedInFile is the strongest check available here: docs/SUMMARY.md is
// committed, so rendering it again has to produce the same bytes. It covers the folder walk, both
// sections that supply their own entries, and the separators between chapters in one assertion.
func TestRenderMatchesTheCheckedInFile(t *testing.T) {
	repo.Chdir(t)

	got, err := render()
	require.NoError(t, err)

	want, err := os.ReadFile(Path)
	require.NoError(t, err)
	require.Equal(t, string(want), got)
}

// TestPrintExcludedSectionsComeLast pins the ordering docs/css/print.css depends on: it drops
// everything from the first Development chapter to the end of the document out of print.html, so
// a section added after those would silently vanish from the print page.
func TestPrintExcludedSectionsComeLast(t *testing.T) {
	all := sections()

	first := -1
	for i, s := range all {
		if s.note == printExcludedNote {
			first = i
			break
		}
	}
	require.NotEqual(t, -1, first, "some section has to carry the note")

	for _, s := range all[first:] {
		require.Equal(t, printExcludedNote, s.note,
			"%s follows the first print-excluded chapter but is not marked as excluded", s.title)
	}
}

// TestEverySectionProducesEntries guards against a chapter that renders as a bare heading: either
// it walks a folder, supplies its own lines, or carries literal entries.
func TestEverySectionProducesEntries(t *testing.T) {
	for _, s := range sections() {
		require.NotEmpty(t, s.title)

		hasWalk := s.regex != "" && s.folder != ""
		require.True(t, hasWalk || s.lines != nil || s.extra != "",
			"%s would render as an empty chapter", s.title)
	}
}

// TestEntriesIndentsCommandsByDepth covers the one non-obvious rule in the folder walk: Cobra
// names a subcommand's page after its whole command path, so the bullet is stepped in one level
// per ancestor and labelled with the last word.
func TestEntriesIndentsCommandsByDepth(t *testing.T) {
	repo.Chdir(t)

	lines, err := entries(`cargoship_(.+)\.md`, "commands", true)
	require.NoError(t, err)
	require.NotEmpty(t, lines)

	var nested bool
	for _, line := range lines {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		require.Zero(t, indent%2, "indent is two spaces per level: %q", line)
		require.GreaterOrEqual(t, indent, 2, "a command page is always at least one level in")
		if indent > 2 {
			nested = true
		}
	}
	require.True(t, nested, "docs/commands has subcommands, so something should be nested")
}

// TestEntriesWithoutIndent is the shape every other chapter uses.
func TestEntriesWithoutIndent(t *testing.T) {
	repo.Chdir(t)

	lines, err := entries(`(.+)\.md`, "phases", false)
	require.NoError(t, err)
	require.NotEmpty(t, lines)

	for _, line := range lines {
		require.True(t, strings.HasPrefix(line, "- ["), "%q", line)
		require.Contains(t, line, "](phases/")
	}
}

// TestEntriesOnAMissingFolder returns the error rather than an empty list, which is what lets
// render decide to fall back for a folder-walked chapter and fail for one that cannot.
func TestEntriesOnAMissingFolder(t *testing.T) {
	repo.Chdir(t)

	_, err := entries(`(.+)\.md`, "no-such-folder", false)
	require.Error(t, err)
}

func TestEntriesOnABadPattern(t *testing.T) {
	_, err := entries(`(unclosed`, "phases", false)
	require.Error(t, err)
}
