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

// Package summary assembles docs/SUMMARY.md, the table of contents mdBook builds the book's
// sidebar and page order from.
package summary

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/ansibledoc"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/golangdoc"
	"github.com/nao1215/markdown"
)

// printExcludedNote marks the sections that docs/css/print.css hides from
// print.html, so the omission is visible to anyone reading SUMMARY.md.
const printExcludedNote = "<!-- Excluded from the print page (print.html) by docs/css/print.css. -->"

// Path is the file this package writes.
const Path = "docs/SUMMARY.md"

// section is one chapter of the book.
type section struct {
	title  string
	folder string
	regex  string
	indent bool
	extra  string
	note   string
	// lines supplies the section's entries itself, for a section the folder walk below
	// cannot produce. It replaces regex and folder rather than adding to them.
	lines func() ([]string, error)
}

// sections is the book, in order.
//
// Section order matters beyond the sidebar: docs/css/print.css drops everything from the first
// Development chapter to the end of the document out of the print page, so Development, Agent and
// Misc have to stay last, in that order. Anything added after them is excluded from print too.
func sections() []section {
	return []section{
		{
			// Both entries are prefix chapters: mdBook gives a line before the first list
			// item no number and places it ahead of every numbered chapter, which is where
			// the front page and the security policy belong.
			title: "Index",
			extra: "[readme](index.md)\n[security](security.md)",
		},
		{
			title:  "Guides",
			regex:  `(.+)\.md`,
			folder: "guides",
		},
		{
			title:  "Commands",
			extra:  "- [cargoship](commands/cargoship.md)",
			regex:  `cargoship_(.+)\.md`,
			folder: "commands",
			indent: true,
		},
		{
			title:  "Schema",
			regex:  `(.+)\.md`,
			folder: "schema",
		},
		{
			title: "Ansible",
			lines: ansibledoc.Summary,
		},
		{
			title:  "Phases",
			regex:  `(.+)\.md`,
			folder: "phases",
		},
		{
			title: "Golang",
			lines: golangdoc.Summary,
		},
		{
			title:  "Development",
			folder: "dev",
			regex:  `(.+)\.md`,
			note:   printExcludedNote,
		},
		{
			title:  "Workflows",
			folder: "workflows",
			regex:  `(.+)\.md`,
			note:   printExcludedNote,
		},
		{
			title:  "Agent",
			folder: "agent",
			regex:  `(.+)\.md`,
			note:   printExcludedNote,
		},
		{
			title:  "Misc",
			folder: "misc",
			regex:  `(.+)\.md`,
			note:   printExcludedNote,
		},
	}
}

// Generate writes docs/SUMMARY.md from what is on disk under docs/, plus the two sections that
// supply their own entries.
func Generate() error {
	fmt.Println(Path)

	content, err := render()
	if err != nil {
		return err
	}

	return os.WriteFile(Path, []byte(content), 0644)
}

// render builds the file's content. Kept apart from the write so a test can read it without a
// docs/SUMMARY.md on disk to compare against.
func render() (string, error) {
	var builder strings.Builder
	md := markdown.NewMarkdown(&builder)

	summary := sections()
	for i, item := range summary {
		md = md.H1(item.title)
		md = md.PlainText("")
		if item.note != "" {
			md = md.PlainText(item.note)
			md = md.PlainText("")
		}
		if item.extra != "" {
			md = md.PlainText(item.extra)
		}
		if item.lines != nil {
			// Unlike the folder walk below, this error is returned rather than swallowed: a
			// section that supplies its own entries has nothing to fall back to, and an empty
			// chapter is not something anyone would notice in the diff.
			mark, err := item.lines()
			if err != nil {
				return "", err
			}
			for _, p := range mark {
				md = md.PlainText(p)
			}
		}
		if item.regex != "" && item.folder != "" {
			if mark, err := entries(item.regex, item.folder, item.indent); err == nil {
				for _, p := range mark {
					md = md.PlainText(p)
				}
			}
		}
		if i != len(summary)-1 {
			md = md.PlainText("\n-----------\n")
		} else {
			md = md.PlainText("")
		}
	}

	if err := md.Build(); err != nil {
		return "", err
	}

	return builder.String(), nil
}

// entries lists one folder under docs/ as sidebar bullets, one per file whose name matches
// pattern. It was getMarkdown in the flat magefiles tree.
func entries(pattern string, directory string, indent bool) ([]string, error) {
	list := []string{}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return []string{}, err
	}

	dirEntries, err := os.ReadDir(fmt.Sprintf("docs/%s", directory))
	if err != nil {
		return []string{}, err
	}

	for _, entry := range dirEntries {
		if !entry.IsDir() && re.MatchString(entry.Name()) {
			com := re.FindStringSubmatch(entry.Name())
			if indent {
				// Cobra names a subcommand's page after its whole command path, joined with
				// underscores, so "vault encrypt" lands in cargoship_vault_encrypt.md. Step
				// the bullet in one level per ancestor and label it with the last word, so
				// that the sidebar nests a command under the group it belongs to.
				path := strings.Split(com[1], "_")
				name := path[len(path)-1]
				list = append(list, fmt.Sprintf("%s- [%s](%s/%s)", strings.Repeat("  ", len(path)), name, directory, entry.Name()))
			} else {
				list = append(list, fmt.Sprintf("- [%s](%s/%s)", com[1], directory, entry.Name()))
			}
		}
	}

	return list, nil
}
