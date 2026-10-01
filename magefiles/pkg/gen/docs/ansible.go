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

// The pages under docs/ansible are generated from the collection itself: a role's interface from
// its meta/argument_specs.yml and defaults/main.yml (ansible_role.go), a module's from the
// DOCUMENTATION and EXAMPLES blocks in its action plugin (ansible_module.go). The two share their
// option types and rendering helpers (ansible_render.go). ansible/colonel_byte/cargoship/AGENTS.md
// is the contract these generators implement.

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

const (
	// collectionDir is the root of the colonel_byte.cargoship collection.
	collectionDir = "ansible/colonel_byte/cargoship"

	// ansibleDocsDir is where the generated pages land. It is deliberately not in docsDirs:
	// collection.md, roles.md and modules.md are hand-written overviews in the same directory,
	// and the RemoveAll that every docsDirs entry gets would take them with it.
	ansibleDocsDir = "docs/ansible"

	// collectionFQCN is the namespace a playbook writes a module or role under.
	collectionFQCN = "colonel_byte.cargoship"
)

// generatedAnsiblePages are the pages these generators own. They are removed before generation so
// that a deleted module or role does not leave its page behind; everything else in docs/ansible is
// hand-written and untouched.
var generatedAnsiblePages = []string{"module_*.md", "role_*.md"}

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

// generateAnsibleDocs writes the generated reference pages for every role and module in the
// collection.
func generateAnsibleDocs() error {
	if err := os.MkdirAll(ansibleDocsDir, 0o775); err != nil {
		return err
	}
	for _, pattern := range generatedAnsiblePages {
		matches, err := filepath.Glob(filepath.Join(ansibleDocsDir, pattern))
		if err != nil {
			return err
		}
		for _, match := range matches {
			if err := os.Remove(match); err != nil {
				return err
			}
		}
	}

	if err := generateRoleDocs(); err != nil {
		return err
	}
	return generateModuleDocs()
}

// ansibleSummary builds the entries of the Ansible chapter, nesting each generated page under the
// overview that introduces it.
//
// The generic folder walk cannot produce this. docs/ansible holds three hand-written overviews
// alongside the generated pages, and read in name order modules.md and roles.md would both land
// after the pages they introduce, in one flat list that says nothing about which is which.
func ansibleSummary() ([]string, error) {
	lines := []string{
		"- [collection](ansible/collection.md)",
	}

	// Globbed rather than listed, so a module or role added later appears here on its own.
	for _, section := range []struct {
		overview string
		pattern  string
		prefix   string
	}{
		{overview: "modules", pattern: "module_*.md", prefix: "module_"},
		{overview: "roles", pattern: "role_*.md", prefix: "role_"},
	} {
		lines = append(lines, fmt.Sprintf("- [%s](ansible/%s.md)", section.overview, section.overview))

		pages, err := filepath.Glob(filepath.Join(ansibleDocsDir, section.pattern))
		if err != nil {
			return nil, err
		}
		sort.Strings(pages)
		for _, page := range pages {
			name := filepath.Base(page)
			label := strings.TrimSuffix(strings.TrimPrefix(name, section.prefix), ".md")
			lines = append(lines, fmt.Sprintf("  - [%s](ansible/%s)", label, name))
		}
	}

	return lines, nil
}
