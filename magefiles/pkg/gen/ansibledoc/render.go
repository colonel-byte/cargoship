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

package ansibledoc

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/page"
	"github.com/nao1215/markdown"
)

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

// renderCLIFlag renders the flag an option maps onto. "None" is the documented spelling for an
// option that renders no flag, and it is left as prose rather than dressed up as code, because
// there is no flag there to quote.
func renderCLIFlag(o namedOption) string {
	if o.CLIFlag == "" || o.CLIFlag == "None" {
		return "None"
	}
	return "`" + o.CLIFlag + "`"
}

// writeModuleDoc renders docs/ansible/module_<action>.md.
func writeModuleDoc(doc moduleDoc, options []namedOption, examples string) error {
	// The page is named after the action rather than the module, so that module_apply.md sits
	// beside role_cluster.md without stuttering cargoship_ through every filename.
	action := strings.TrimPrefix(doc.Module, "cargoship_")

	path := filepath.Join(Dir, fmt.Sprintf("module_%s.md", action))
	return page.WriteGenerated(path, func(md *markdown.Markdown) error {
		md.H2(doc.Module)
		md.PlainText("")
		md.PlainText(renderProse(doc.ShortDescription) + ".")
		md.PlainText("")
		md.PlainTextf("Write it as `%s.%s`. It runs on the management node; nothing is installed on the fleet.", collectionFQCN, doc.Module)
		md.PlainText("")
		for _, line := range doc.Description {
			md.PlainText(renderProse(line))
			md.PlainText("")
		}

		md.H3("Parameters")
		md.PlainText("")
		rows := make([][]string, 0, len(options))
		for _, o := range options {
			required := "no"
			if o.Required {
				required = "yes"
			}
			rows = append(rows, []string{
				"`" + o.name + "`",
				renderType(o),
				required,
				renderCLIFlag(o),
				renderDescription(o),
			})
		}
		for _, line := range page.PaddedTable([]string{"Parameter", "Type", "Required", "Flag", "Description"}, rows) {
			md.PlainText(line)
		}
		md.PlainText("")

		if len(doc.Notes) > 0 {
			md.H3("Notes")
			md.PlainText("")
			notes := make([]string, 0, len(doc.Notes))
			for _, note := range doc.Notes {
				notes = append(notes, renderProse(note))
			}
			md.BulletList(notes...)
			md.PlainText("")
		}

		md.H3("Example")
		md.PlainText("")
		md.CodeBlocks(markdown.SyntaxHighlightYAML, strings.TrimSpace(examples))
		md.PlainText("")
		return nil
	})
}

// writeRoleDoc renders docs/ansible/role_<role>.md.
func writeRoleDoc(role string, entry roleEntrypoint, options []namedOption) error {
	path := filepath.Join(Dir, fmt.Sprintf("role_%s.md", role))
	return page.WriteGenerated(path, func(md *markdown.Markdown) error {
		md.H2(role)
		md.PlainText("")
		md.PlainText(renderProse(entry.ShortDescription) + ".")
		md.PlainText("")
		md.PlainTextf("Include it as `%s.%s`.", collectionFQCN, role)
		md.PlainText("")
		for _, line := range entry.Description {
			md.PlainText(renderProse(line))
			md.PlainText("")
		}

		md.H3("Variables")
		md.PlainText("")
		rows := make([][]string, 0, len(options))
		for _, o := range options {
			rows = append(rows, []string{
				"`" + o.name + "`",
				renderType(o),
				"`" + page.RenderYAMLValue(o.Default) + "`",
				renderDescription(o),
			})
		}
		for _, line := range page.PaddedTable([]string{"Variable", "Type", "Default", "Description"}, rows) {
			md.PlainText(line)
		}
		md.PlainText("")
		return nil
	})
}
