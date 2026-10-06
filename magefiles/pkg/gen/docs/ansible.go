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

// The pages under docs/ansible are generated from the collections themselves: a role's interface
// from its meta/argument_specs.yml and defaults/main.yml (ansible_role.go), a module's from the
// DOCUMENTATION and EXAMPLES blocks in its action plugin (ansible_module.go). The two share their
// option types and rendering helpers (ansible_render.go). ansible/colonel_byte/cargoship/AGENTS.md
// is the contract these generators implement.
//
// Every collection under ansible/ is generated the same way, from the table in
// ansibleCollections. Adding one is an entry there plus the hand-written overviews its entry
// names; nothing else here knows how many there are.

package docs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	goyaml "github.com/goccy/go-yaml"
)

// ansibleDocsDir is where the Ansible chapter lives: one subdirectory per collection. It is
// deliberately not in docsDirs: each collection's collection.md, modules.md and roles.md are
// hand-written overviews in the same directory as its generated pages, and the RemoveAll that
// every docsDirs entry gets would take them with it.
const ansibleDocsDir = "docs/ansible"

// generatedAnsiblePages are the pages these generators own. They are removed from a collection's
// own directory before generation, so that a deleted module or role does not leave its page
// behind; everything else under docs/ansible is hand-written and untouched.
var generatedAnsiblePages = []string{"module_*.md", "role_*.md"}

// ansibleCollection is one collection the generators cover.
type ansibleCollection struct {
	// dir is the collection's root in the source tree.
	dir string
	// docsDir is where its generated pages land, and where the hand-written overviews that
	// introduce them live.
	docsDir string
	// fqcn is the name a playbook writes a module or role under.
	fqcn string
	// modulePrefix is the prefix every action plugin's filename and module name carries. It comes
	// off the page name, so that module_apply.md sits beside role_cluster.md without stuttering
	// the collection's name through every filename.
	modulePrefix string
	// tagline is the sentence that follows the "write it as" line on a module page. It differs
	// per collection because what the module does with the node it runs on differs.
	tagline string
	// roles is whether to look for roles at all. A collection with none is not an error; it just
	// has no roles/ directory to walk.
	roles bool
	// summaryLabel prefixes this collection's entries in docs/SUMMARY.md. Every collection names
	// itself there, including the first: two bare "modules" entries in one chapter say nothing
	// about which collection either belongs to.
	summaryLabel string
}

// ansibleCollections is every collection under ansible/, in the order their sections appear in the
// Ansible chapter.
//
// Every collection gets a directory of its own under docs/ansible, named after it. The overviews
// are per collection, so two of them cannot both be docs/ansible/cargoship/collection.md -- and a chapter
// where one collection sits at the root and the others are nested reads as though the first were
// the real one.
var ansibleCollections = []ansibleCollection{
	{
		dir:          "ansible/colonel_byte/cargoship",
		docsDir:      filepath.Join(ansibleDocsDir, "cargoship"),
		fqcn:         "colonel_byte.cargoship",
		modulePrefix: "cargoship_",
		tagline:      "It runs on the management node; nothing is installed on the fleet.",
		roles:        true,
		summaryLabel: "cargoship",
	},
	{
		dir:          "ansible/colonel_byte/zarf",
		docsDir:      filepath.Join(ansibleDocsDir, "zarf"),
		fqcn:         "colonel_byte.zarf",
		modulePrefix: "zarf_",
		tagline:      "It runs the installed zarf on the node the task is delegated to; nothing is installed on the cluster's nodes.",
		roles:        true,
		summaryLabel: "zarf",
	},
}

var (
	// The DOCUMENTATION and EXAMPLES blocks are pulled out of the plugin by pattern rather than by
	// importing Python, the same way internal/ansibleinv/plugin_test.go reads the variable
	// allowlist out of the projection plugin.
	documentationRe = regexp.MustCompile(`(?sm)^DOCUMENTATION = r"""\n(.*?)\n"""$`)
	examplesRe      = regexp.MustCompile(`(?sm)^EXAMPLES = r"""\n(.*?)\n"""$`)
)

// docOption is one documented parameter, from either a module's DOCUMENTATION block or a role's
// argument_specs.yml. The two formats agree on every key used here except cli_flag, which only a
// module has.
type docOption struct {
	Type        string   `yaml:"type"`
	Elements    string   `yaml:"elements"`
	Required    bool     `yaml:"required"`
	Default     any      `yaml:"default"`
	Choices     []string `yaml:"choices"`
	Description []string `yaml:"description"`

	// CLIFlag is the flag the parameter renders on the cargoship command line, or "None" when it
	// renders none. It is a cargoship extension rather than a standard Ansible documentation key;
	// see the comment above DOCUMENTATION in any of the action plugins.
	CLIFlag string `yaml:"cli_flag"`
}

// namedOption is an option and the name it was declared under, kept as a slice rather than a map
// because declaration order is the row order of the generated table.
type namedOption struct {
	name string
	docOption
}

// moduleDoc is a module's DOCUMENTATION block.
type moduleDoc struct {
	Module           string          `yaml:"module"`
	ShortDescription string          `yaml:"short_description"`
	Description      []string        `yaml:"description"`
	VersionAdded     string          `yaml:"version_added"`
	Author           []string        `yaml:"author"`
	Notes            []string        `yaml:"notes"`
	Options          goyaml.MapSlice `yaml:"options"`
}

// roleEntrypoint is one entry point in a role's argument_specs.yml. Only main is documented; a
// role with a second entry point would need a page section of its own.
type roleEntrypoint struct {
	ShortDescription string          `yaml:"short_description"`
	Description      []string        `yaml:"description"`
	Options          goyaml.MapSlice `yaml:"options"`
}

// roleSpecs is a role's meta/argument_specs.yml.
type roleSpecs struct {
	ArgumentSpecs map[string]roleEntrypoint `yaml:"argument_specs"`
}

// generateAnsibleDocs writes the generated reference pages for every role and module of every
// collection.
func generateAnsibleDocs() error {
	for _, collection := range ansibleCollections {
		if err := generateCollectionDocs(collection); err != nil {
			return fmt.Errorf("generating the %s reference: %w", collection.fqcn, err)
		}
	}
	return nil
}

// generateCollectionDocs writes one collection's pages.
func generateCollectionDocs(collection ansibleCollection) error {
	if err := os.MkdirAll(collection.docsDir, 0o775); err != nil {
		return err
	}
	for _, pattern := range generatedAnsiblePages {
		matches, err := filepath.Glob(filepath.Join(collection.docsDir, pattern))
		if err != nil {
			return err
		}
		for _, match := range matches {
			if err := os.Remove(match); err != nil {
				return err
			}
		}
	}

	if collection.roles {
		if err := generateRoleDocs(collection); err != nil {
			return err
		}
	}
	return generateModuleDocs(collection)
}

// ansibleSummary builds the entries of the Ansible chapter, nesting each generated page under the
// overview that introduces it, one group per collection.
//
// The generic folder walk cannot produce this. Each collection's directory holds hand-written
// overviews alongside the generated pages, and read in name order modules.md and roles.md would
// both land after the pages they introduce, in one flat list that says nothing about which is
// which -- and with more than one collection, nothing about which collection either.
func ansibleSummary() ([]string, error) {
	var lines []string
	for _, collection := range ansibleCollections {
		group, err := collectionSummary(collection)
		if err != nil {
			return nil, err
		}
		lines = append(lines, group...)
	}
	return lines, nil
}

// collectionSummary builds one collection's entries.
func collectionSummary(collection ansibleCollection) ([]string, error) {
	// A link in SUMMARY.md is relative to docs/, which is the book's source directory.
	linkDir, err := filepath.Rel(docsDir, collection.docsDir)
	if err != nil {
		return nil, err
	}

	lines := []string{
		fmt.Sprintf("- [%s](%s)", collection.summaryEntry("collection"),
			filepath.Join(linkDir, "collection.md")),
	}

	// Globbed rather than listed, so a module or role added later appears here on its own.
	sections := []struct {
		overview string
		pattern  string
		prefix   string
	}{
		{overview: "modules", pattern: "module_*.md", prefix: "module_"},
	}
	if collection.roles {
		sections = append(sections,
			struct {
				overview string
				pattern  string
				prefix   string
			}{overview: "roles", pattern: "role_*.md", prefix: "role_"})
	}

	for _, section := range sections {
		lines = append(lines, fmt.Sprintf("- [%s](%s)", collection.summaryEntry(section.overview),
			filepath.Join(linkDir, section.overview+".md")))

		pages, err := filepath.Glob(filepath.Join(collection.docsDir, section.pattern))
		if err != nil {
			return nil, err
		}
		sort.Strings(pages)
		for _, page := range pages {
			name := filepath.Base(page)
			label := strings.TrimSuffix(strings.TrimPrefix(name, section.prefix), ".md")
			lines = append(lines, fmt.Sprintf("  - [%s](%s)", label, filepath.Join(linkDir, name)))
		}
	}

	return lines, nil
}

// summaryEntry is what one of this collection's sections is called in the sidebar.
func (c ansibleCollection) summaryEntry(section string) string {
	return c.summaryLabel + " " + section
}
