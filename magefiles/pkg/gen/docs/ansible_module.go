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

package docs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	goyaml "github.com/goccy/go-yaml"
	"github.com/nao1215/markdown"
)

// generateModuleDocs writes module_<action>.md for every action plugin of one collection.
func generateModuleDocs(collection ansibleCollection) error {
	plugins, err := filepath.Glob(filepath.Join(collection.dir, "plugins", "action",
		collection.modulePrefix+"*.py"))
	if err != nil {
		return err
	}
	sort.Strings(plugins)

	for _, pluginPath := range plugins {
		source, err := os.ReadFile(pluginPath)
		if err != nil {
			return err
		}

		block := documentationRe.FindSubmatch(source)
		if block == nil {
			return fmt.Errorf(`%s: no DOCUMENTATION = r""" block`, pluginPath)
		}
		var doc moduleDoc
		if err := goyaml.Unmarshal(block[1], &doc); err != nil {
			return fmt.Errorf("reading the DOCUMENTATION block in %s: %w", pluginPath, err)
		}

		name := strings.TrimSuffix(filepath.Base(pluginPath), ".py")
		if doc.Module != name {
			return fmt.Errorf("%s: documents module %q", pluginPath, doc.Module)
		}
		options, err := orderedOptions(doc.Options)
		if err != nil {
			return fmt.Errorf("reading the DOCUMENTATION block in %s: %w", pluginPath, err)
		}

		examples := examplesRe.FindSubmatch(source)
		if examples == nil {
			return fmt.Errorf(`%s: no EXAMPLES = r""" block`, pluginPath)
		}

		if err := writeModuleDoc(collection, doc, options, string(examples[1])); err != nil {
			return err
		}
	}
	return nil
}

// writeModuleDoc renders one collection's module_<action>.md.
func writeModuleDoc(collection ansibleCollection, doc moduleDoc, options []namedOption, examples string) error {
	// The page is named after the action rather than the module, so that module_apply.md sits
	// beside role_cluster.md without stuttering the collection's name through every filename.
	action := strings.TrimPrefix(doc.Module, collection.modulePrefix)

	return writeGeneratedPage(collection.docsDir, fmt.Sprintf("module_%s.md", action), func(md *markdown.Markdown) error {
		md.H2(doc.Module)
		md.PlainText("")
		md.PlainText(renderProse(doc.ShortDescription) + ".")
		md.PlainText("")
		md.PlainTextf("Write it as `%s.%s`. %s", collection.fqcn, doc.Module, collection.tagline)
		md.PlainText("")
		if line := renderVersionAdded(collection, doc.VersionAdded); line != "" {
			md.PlainText(line)
			md.PlainText("")
		}
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
		for _, line := range paddedTable([]string{"Parameter", "Type", "Required", "Flag", "Description"}, rows) {
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
