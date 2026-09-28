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

package page

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nao1215/markdown"
	"github.com/stretchr/testify/require"
)

func TestRenderYAMLValue(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value any
		want  string
	}{
		// The empty string is the whole reason this function exists: an empty cell could mean
		// "no default" just as easily as "defaults to nothing".
		{name: "empty string", value: "", want: `""`},
		{name: "nil", value: nil, want: "null"},
		// A non-empty string is left unquoted, because the caller puts it in a code span.
		{name: "plain string", value: "cargoship", want: "cargoship"},
		{name: "string that looks like a bool", value: "true", want: "true"},
		{name: "bool", value: true, want: "true"},
		{name: "int", value: 7, want: "7"},
		{name: "empty list", value: []any{}, want: "[]"},
		{name: "list", value: []any{"a", "b"}, want: "- a\n- b"},
		{name: "map", value: map[string]any{"k": "v"}, want: "k: v"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, RenderYAMLValue(tt.value))
		})
	}
}

func TestEscapeAngleBrackets(t *testing.T) {
	require.Equal(t,
		"node-role.kubernetes.io/&lt;profile&gt;",
		EscapeAngleBrackets("node-role.kubernetes.io/<profile>"))
	require.Equal(t, "nothing to do", EscapeAngleBrackets("nothing to do"))
}

// TestPaddedTableAligns is the shape ansible/AGENTS.md requires: every line of the table the same
// length, one space of padding inside each pipe, and the delimiter dashes run out to the column
// width. Checking the lengths match rather than hard-coding one expected string keeps the test
// about the rule instead of about this particular table.
func TestPaddedTableAligns(t *testing.T) {
	lines := PaddedTable(
		[]string{"Parameter", "Type", "Description"},
		[][]string{
			{"`a`", "`string`", "short"},
			{"`a-much-longer-name`", "`list` of `string`", "a description that is longer still"},
		},
	)
	require.Len(t, lines, 4, "header, delimiter, and one line per row")

	want := utf8.RuneCountInString(lines[0])
	for i, line := range lines {
		require.Equal(t, want, utf8.RuneCountInString(line), "line %d is a different width", i)
		require.True(t, strings.HasPrefix(line, "| "), "line %d: %q", i, line)
		require.True(t, strings.HasSuffix(line, " |"), "line %d: %q", i, line)
	}
	// The delimiter row is padded out with dashes rather than spaces, which is what keeps the
	// table readable at a fixed width in a plain editor.
	for _, cell := range strings.Split(strings.Trim(lines[1], "|"), "|") {
		require.Equal(t, strings.Repeat("-", len(strings.TrimSpace(cell))), strings.TrimSpace(cell))
		require.NotEmpty(t, strings.TrimSpace(cell))
	}
}

// TestPaddedTableEscapesBeforeMeasuring covers the ordering the padding depends on: a cell that
// grows when it is escaped has to be measured after, or every line below it is short.
func TestPaddedTableEscapesBeforeMeasuring(t *testing.T) {
	lines := PaddedTable(
		[]string{"Cell"},
		[][]string{{"<profile>"}, {"x"}},
	)

	require.Contains(t, lines[2], "&lt;profile&gt;")
	width := utf8.RuneCountInString(lines[0])
	for _, line := range lines {
		require.Equal(t, width, utf8.RuneCountInString(line))
	}
}

// TestPaddedTableShortRow guards the bounds check: a row with fewer cells than the header pads out
// rather than panicking, since a generator that forgets a column should produce a visibly empty
// cell, not crash the docs run.
func TestPaddedTableShortRow(t *testing.T) {
	lines := PaddedTable([]string{"A", "B"}, [][]string{{"only-a"}})
	require.Len(t, lines, 3)
	require.Equal(t, utf8.RuneCountInString(lines[0]), utf8.RuneCountInString(lines[2]))
}

func TestWriteGeneratedOpensWithTheBanner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "page.md")

	require.NoError(t, WriteGenerated(path, func(doc *markdown.Markdown) error {
		doc.H2("Heading")
		return nil
	}))

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(content), Banner),
		"the banner has to be the first thing in the file: %q", string(content))
	require.Contains(t, string(content), "## Heading")
}

// TestWriteGeneratedPropagatesTheWriterError keeps a failing generator from being reported as a
// page that rendered fine.
func TestWriteGeneratedPropagatesTheWriterError(t *testing.T) {
	sentinel := errors.New("nothing to render")

	err := WriteGenerated(filepath.Join(t.TempDir(), "page.md"), func(*markdown.Markdown) error {
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
}

func TestWriteGeneratedReportsAnUncreatablePath(t *testing.T) {
	err := WriteGenerated(filepath.Join(t.TempDir(), "no-such-dir", "page.md"), func(*markdown.Markdown) error {
		return nil
	})
	require.Error(t, err)
}
