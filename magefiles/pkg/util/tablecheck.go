// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/magefile/mage/sh"
)

// tableCheckSkip is skipped by CheckRepoMarkdownTables. The root AGENTS.md is exempt by
// request; CLAUDE.md is a symlink to it, so it carries the same tables.
var tableCheckSkip = map[string]bool{
	"AGENTS.md": true,
	"CLAUDE.md": true,
}

// CheckRepoMarkdownTables checks every tracked Markdown file outside vendor/ and reports each
// misaligned table line on stdout, returning an error when any were found.
func CheckRepoMarkdownTables() error {
	out, err := sh.Output("git", "ls-files", "-z", "*.md")
	if err != nil {
		return err
	}

	var files []string
	for _, f := range strings.Split(out, "\x00") {
		if f == "" || tableCheckSkip[f] || strings.HasPrefix(f, "vendor/") {
			continue
		}
		files = append(files, f)
	}

	findings, err := CheckMarkdownTables(files)
	if err != nil {
		return err
	}

	for _, f := range findings {
		fmt.Printf("%s:%d: table cell padding\n  got:  %s\n  want: %s\n", f.File, f.Line, f.Got, f.Want)
	}

	if len(findings) > 0 {
		return fmt.Errorf("%d misaligned table line(s) in %d file(s)", len(findings), countFiles(findings))
	}

	fmt.Printf("checked %d markdown files, all tables padded\n", len(files))

	return nil
}

// countFiles is the number of distinct files the findings cover.
func countFiles(findings []TableFinding) int {
	seen := map[string]bool{}
	for _, f := range findings {
		seen[f.File] = true
	}
	return len(seen)
}

// delimiterCell matches a table delimiter cell, with the optional alignment colons GitHub
// understands. No table in the tree uses them, but one that did would otherwise be reported
// as misaligned however it was padded.
var delimiterCell = regexp.MustCompile(`^(:?)-+(:?)$`)

// TableFinding is one line of a Markdown table that is not padded to its column widths.
type TableFinding struct {
	File string
	Line int // 1-indexed
	Got  string
	Want string
}

// CheckMarkdownTables reports every table line in the given files that is not padded the way
// the repository style guide requires: one space inside each pipe, every cell padded to the
// width of the widest cell in its column, and the delimiter row's dashes run out to that same
// width. Tables inside fenced code blocks are skipped, since the style guide and several
// AGENTS.md files show deliberately unpadded tables as examples.
func CheckMarkdownTables(files []string) ([]TableFinding, error) {
	var findings []TableFinding

	for _, file := range files {
		data, err := os.ReadFile(file) //nolint:gosec // paths come from the caller's own file list
		if err != nil {
			return nil, err
		}

		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		fenced := false

		for i := 0; i < len(lines); i++ {
			trimmed := strings.TrimSpace(lines[i])
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				fenced = !fenced
				continue
			}
			if fenced || !strings.HasPrefix(trimmed, "|") {
				continue
			}

			end := i
			for end < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[end]), "|") {
				end++
			}
			block := lines[i:end]
			if len(block) >= 2 && isDelimiterRow(block[1]) {
				findings = append(findings, checkBlock(file, i+1, block)...)
			}
			i = end - 1
		}
	}

	return findings, nil
}

// isDelimiterRow reports whether a line is a table's delimiter row.
func isDelimiterRow(line string) bool {
	cells := splitRow(line)
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		if !delimiterCell.MatchString(strings.TrimSpace(cell)) {
			return false
		}
	}
	return true
}

// splitRow splits a table row into its cells on unescaped pipes, dropping the leading and
// trailing delimiters. Cells keep whatever padding they were written with.
func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")

	var (
		cells []string
		cur   strings.Builder
	)
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line):
			cur.WriteByte(line[i])
			cur.WriteByte(line[i+1])
			i++
		case line[i] == '|':
			cells = append(cells, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(line[i])
		}
	}
	// Content after the last pipe means the row has no trailing delimiter.
	if strings.TrimSpace(cur.String()) != "" {
		cells = append(cells, cur.String())
	}

	return cells
}

// checkBlock compares every line of one table against its padded form. start is the 1-indexed
// line number of the block's first row.
func checkBlock(file string, start int, block []string) []TableFinding {
	rows := make([][]string, len(block))
	var width []int
	for i, line := range block {
		rows[i] = splitRow(line)
		delimiter := isDelimiterRow(line)
		for j, cell := range rows[i] {
			for len(width) <= j {
				width = append(width, 0)
			}
			if n := utf8.RuneCountInString(strings.TrimSpace(cell)); n > width[j] && !delimiter {
				width[j] = n
			}
		}
	}

	var findings []TableFinding
	for i, cells := range rows {
		want := renderRow(cells, width, isDelimiterRow(block[i]))
		if want != block[i] {
			findings = append(findings, TableFinding{
				File: file,
				Line: start + i,
				Got:  block[i],
				Want: want,
			})
		}
	}

	return findings
}

// renderRow writes one row padded to the column widths, matching PaddedTable's output.
func renderRow(cells []string, width []int, delimiter bool) string {
	var b strings.Builder
	b.WriteString("|")
	for i, cell := range cells {
		cell = strings.TrimSpace(cell)
		if delimiter {
			m := delimiterCell.FindStringSubmatch(cell)
			dashes := width[i] - len(m[1]) - len(m[2])
			if dashes < 1 {
				dashes = 1
			}
			cell = m[1] + strings.Repeat("-", dashes) + m[2]
		}
		pad := width[i] - utf8.RuneCountInString(cell)
		if pad < 0 {
			pad = 0
		}
		b.WriteString(" ")
		b.WriteString(cell)
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(" |")
	}
	return b.String()
}
