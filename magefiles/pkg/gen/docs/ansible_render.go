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

// Rendering helpers shared by ansible_role.go and ansible_module.go: decoding an options mapping,
// converting Ansible's documentation markup to Markdown, and laying out the aligned tables
// ansible/AGENTS.md requires.

package docs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	goyaml "github.com/goccy/go-yaml"
	"github.com/nao1215/markdown"
)

// orderedOptions decodes an options mapping while keeping declaration order, which is the row
// order of the generated table. Unmarshalling straight into a map would lose it.
func orderedOptions(options goyaml.MapSlice) ([]namedOption, error) {
	if len(options) == 0 {
		return nil, errors.New("no options are documented")
	}

	out := make([]namedOption, 0, len(options))
	for _, item := range options {
		name, ok := item.Key.(string)
		if !ok {
			return nil, fmt.Errorf("option name %v is not a string", item.Key)
		}
		// Re-encoded and decoded rather than type-asserted, so the option body is read through
		// the same tags whatever intermediate shape the parser chose for it.
		raw, err := goyaml.Marshal(item.Value)
		if err != nil {
			return nil, fmt.Errorf("re-encoding option %s: %w", name, err)
		}
		var o docOption
		if err := goyaml.Unmarshal(raw, &o); err != nil {
			return nil, fmt.Errorf("decoding option %s: %w", name, err)
		}
		if o.Type == "" {
			return nil, fmt.Errorf("option %s has no type", name)
		}
		if len(o.Description) == 0 {
			return nil, fmt.Errorf("option %s has no description", name)
		}
		out = append(out, namedOption{name: name, docOption: o})
	}
	return out, nil
}

// ansibleMarkup rewrites the semantic markup Ansible's documentation format uses into the Markdown
// the book renders. Only the four forms the collection writes are handled; an unhandled one comes
// through verbatim, which reads as a bug rather than disappearing silently.
var ansibleMarkup = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`\bC\(([^()]*)\)`), "`$1`"},
	{regexp.MustCompile(`\bM\(([^()]*)\)`), "`$1`"},
	{regexp.MustCompile(`\bB\(([^()]*)\)`), "**$1**"},
	{regexp.MustCompile(`\bI\(([^()]*)\)`), "*$1*"},
}

// renderProse converts one documentation string into Markdown.
func renderProse(text string) string {
	for _, m := range ansibleMarkup {
		text = m.re.ReplaceAllString(text, m.repl)
	}
	return text
}

// renderDescription joins an option's description into one table cell. The lines are sentences of
// one paragraph rather than separate paragraphs, which is what a cell can hold.
func renderDescription(o namedOption) string {
	parts := make([]string, 0, len(o.Description)+1)
	for _, line := range o.Description {
		parts = append(parts, renderProse(line))
	}
	if len(o.Choices) > 0 {
		quoted := make([]string, 0, len(o.Choices))
		for _, choice := range o.Choices {
			quoted = append(quoted, "`"+choice+"`")
		}
		parts = append(parts, fmt.Sprintf("One of %s.", strings.Join(quoted, ", ")))
	}
	return strings.Join(parts, " ")
}

// renderType renders an option's type, naming the element type of a list so that a reader knows
// what goes in it.
func renderType(o namedOption) string {
	if o.Elements != "" {
		return fmt.Sprintf("`%s` of `%s`", o.Type, o.Elements)
	}
	return "`" + o.Type + "`"
}

// renderYAMLValue renders a default value the way it is written in YAML, so that an empty string
// reads as "" rather than as an empty cell that could mean anything. A non-empty string is left
// unquoted, because the cell puts it in a code span already.
func renderYAMLValue(value any) string {
	if value == nil {
		return "null"
	}
	if s, ok := value.(string); ok {
		if s == "" {
			return `""`
		}
		return s
	}
	out, err := goyaml.Marshal(value)
	if err != nil {
		// A value that came out of a YAML document goes back into one. Reporting the failure in
		// the cell is better than failing the whole run over a default.
		return fmt.Sprintf("(unrenderable: %v)", err)
	}
	return strings.TrimSpace(string(out))
}

// renderCLIFlag renders the flag an option maps onto. "None" is the documented spelling for an
// option that renders no flag, and it is left as prose rather than dressed up as code, because
// there is no flag there to quote.
func renderCLIFlag(o namedOption) string {
	if o.CLIFlag == "" || o.CLIFlag == "None" {
		return "None"
	}
	return "`" + o.CLIFlag + "`"
}

// escapeAngleBrackets replaces literal "<" and ">" with their HTML entities, so that prose such as
// "node-role.kubernetes.io/<profile>" reads as text instead of an unclosed HTML tag once mdBook
// parses the generated table. This runs before markdown.EscapeTableCell, which is what writes a
// cell's own literal "<br>" for a line break, so that "<br>" is never touched by this replacement.
func escapeAngleBrackets(s string) string {
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// paddedTable renders a table in the aligned form ansible/AGENTS.md requires: every cell padded to
// the width of the widest cell in its column, one space of padding inside each pipe, and the
// delimiter row's dashes run out to that same width, so every line of the table is the same length.
//
// Neither writer in the markdown package produces that form. Table writes a fixed-width delimiter
// row and pads nothing at all. CustomTable pads, through tablewriter, but centres the header cells
// and writes the delimiter row with no space inside its pipes. Rendering the rows here is shorter
// than correcting either, and it does not change shape when tablewriter's formatting does.
func paddedTable(header []string, rows [][]string) []string {
	// Cells hold prose, which may contain a pipe or a line break; both have to be escaped before
	// their width is measured, or the padding is computed against the wrong string.
	escaped := make([][]string, 0, len(rows)+1)
	width := make([]int, len(header))
	for _, row := range append([][]string{header}, rows...) {
		cells := make([]string, len(header))
		for i := range cells {
			if i < len(row) {
				cells[i] = markdown.EscapeTableCell(escapeAngleBrackets(row[i]))
			}
			if n := utf8.RuneCountInString(cells[i]); n > width[i] {
				width[i] = n
			}
		}
		escaped = append(escaped, cells)
	}

	line := func(cells []string) string {
		var b strings.Builder
		b.WriteString("|")
		for i, cell := range cells {
			b.WriteString(" ")
			b.WriteString(cell)
			b.WriteString(strings.Repeat(" ", width[i]-utf8.RuneCountInString(cell)))
			b.WriteString(" |")
		}
		return b.String()
	}

	delimiter := make([]string, len(header))
	for i := range delimiter {
		delimiter[i] = strings.Repeat("-", width[i])
	}

	out := make([]string, 0, len(escaped)+1)
	out = append(out, line(escaped[0]), line(delimiter))
	for _, row := range escaped[1:] {
		out = append(out, line(row))
	}
	return out
}

// writeGeneratedPage opens a page under docs/ansible and hands it to write, with the banner that
// says the page is generated already in place.
func writeGeneratedPage(name string, write func(doc *markdown.Markdown) error) error {
	path := filepath.Join(ansibleDocsDir, name)
	fmt.Println(path)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			panic(err)
		}
	}()

	doc := markdown.NewMarkdown(f)
	doc.PlainText(generatedBanner)
	doc.PlainText("")

	if err := write(doc); err != nil {
		return err
	}
	return doc.Build()
}
