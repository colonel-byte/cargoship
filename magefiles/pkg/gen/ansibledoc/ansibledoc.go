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

// Package ansibledoc generates the reference pages under docs/ansible from the collection itself:
// a role's interface from its meta/argument_specs.yml and defaults/main.yml, a module's from the
// DOCUMENTATION and EXAMPLES blocks in its action plugin.
//
// ansible/colonel_byte/cargoship/AGENTS.md is the contract these generators implement. Discovery
// and parsing live here; how an option becomes a table cell lives in render.go.
package ansibledoc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/page"
	goyaml "github.com/goccy/go-yaml"
)

const (
	// collectionDir is the root of the colonel_byte.cargoship collection.
	collectionDir = "ansible/colonel_byte/cargoship"

	// Dir is where the generated pages land. It is deliberately not one of the directories the
	// docs run wipes: collection.md, roles.md and modules.md are hand-written overviews in the
	// same directory, and a RemoveAll would take them with it.
	Dir = "docs/ansible"

	// collectionFQCN is the namespace a playbook writes a module or role under.
	collectionFQCN = "colonel_byte.cargoship"
)

// generatedPages are the pages this package owns. They are removed before generation so that a
// deleted module or role does not leave its page behind; everything else in docs/ansible is
// hand-written and untouched.
var generatedPages = []string{"module_*.md", "role_*.md"}

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

// Generate writes the generated reference pages for every role and module in the collection.
func Generate() error {
	if err := os.MkdirAll(Dir, 0o775); err != nil {
		return err
	}
	for _, pattern := range generatedPages {
		matches, err := filepath.Glob(filepath.Join(Dir, pattern))
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

// generateRoleDocs writes docs/ansible/role_<role>.md for every role that documents itself.
//
// Discovery is a glob, so a role added later needs no change here -- but a role without an
// argument_specs.yml gets no page, which is why AGENTS.md asks for one first.
func generateRoleDocs() error {
	specs, err := filepath.Glob(filepath.Join(collectionDir, "roles", "*", "meta", "argument_specs.yml"))
	if err != nil {
		return err
	}
	sort.Strings(specs)

	for _, specPath := range specs {
		// roles/<role>/meta/argument_specs.yml
		role := filepath.Base(filepath.Dir(filepath.Dir(specPath)))

		raw, err := os.ReadFile(specPath)
		if err != nil {
			return err
		}
		var parsed roleSpecs
		if err := goyaml.Unmarshal(raw, &parsed); err != nil {
			return fmt.Errorf("reading %s: %w", specPath, err)
		}
		entry, ok := parsed.ArgumentSpecs["main"]
		if !ok {
			return fmt.Errorf("%s: no main entry point", specPath)
		}
		documented, err := orderedOptions(entry.Options)
		if err != nil {
			return fmt.Errorf("reading %s: %w", specPath, err)
		}

		defaultsPath := filepath.Join(collectionDir, "roles", role, "defaults", "main.yml")
		rawDefaults, err := os.ReadFile(defaultsPath)
		if err != nil {
			return err
		}
		var defaults goyaml.MapSlice
		if err := goyaml.Unmarshal(rawDefaults, &defaults); err != nil {
			return fmt.Errorf("reading %s: %w", defaultsPath, err)
		}

		ordered, err := reconcileRoleDefaults(role, documented, defaults)
		if err != nil {
			return err
		}
		if err := writeRoleDoc(role, entry, ordered); err != nil {
			return err
		}
	}
	return nil
}

// reconcileRoleDefaults puts the documented options into defaults/main.yml order, and reports the
// two ways the pair can disagree: a variable with a default and no documentation, or documentation
// and no default. Both would otherwise produce a page that is quietly missing a row.
//
// It also reports a default that the two files spell differently, because then the page would
// document a value the role does not actually use.
func reconcileRoleDefaults(role string, documented []namedOption, defaults goyaml.MapSlice) ([]namedOption, error) {
	byName := make(map[string]namedOption, len(documented))
	for _, o := range documented {
		byName[o.name] = o
	}

	ordered := make([]namedOption, 0, len(documented))
	for _, item := range defaults {
		name, ok := item.Key.(string)
		if !ok {
			return nil, fmt.Errorf("roles/%s/defaults/main.yml: variable name %v is not a string", role, item.Key)
		}
		o, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf(
				"roles/%s: %s has a default but no entry in meta/argument_specs.yml", role, name)
		}
		if spec, actual := page.RenderYAMLValue(o.Default), page.RenderYAMLValue(item.Value); spec != actual {
			return nil, fmt.Errorf(
				"roles/%s: %s defaults to %s in defaults/main.yml but %s in meta/argument_specs.yml",
				role, name, actual, spec)
		}
		// The value the role actually loads is the one that gets documented.
		o.Default = item.Value
		ordered = append(ordered, o)
		delete(byName, name)
	}

	for _, o := range documented {
		if _, unmatched := byName[o.name]; unmatched {
			return nil, fmt.Errorf(
				"roles/%s: %s is documented in meta/argument_specs.yml but has no default in defaults/main.yml",
				role, o.name)
		}
	}
	return ordered, nil
}

// generateModuleDocs writes docs/ansible/module_<action>.md for every action plugin.
func generateModuleDocs() error {
	plugins, err := filepath.Glob(filepath.Join(collectionDir, "plugins", "action", "cargoship_*.py"))
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

		if err := writeModuleDoc(doc, options, string(examples[1])); err != nil {
			return err
		}
	}
	return nil
}

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

// Summary builds the entries of the Ansible chapter, nesting each generated page under the
// overview that introduces it.
//
// The generic folder walk cannot produce this. docs/ansible holds three hand-written overviews
// alongside the generated pages, and read in name order modules.md and roles.md would both land
// after the pages they introduce, in one flat list that says nothing about which is which.
func Summary() ([]string, error) {
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

		pages, err := filepath.Glob(filepath.Join(Dir, section.pattern))
		if err != nil {
			return nil, err
		}
		sort.Strings(pages)
		for _, p := range pages {
			name := filepath.Base(p)
			label := strings.TrimSuffix(strings.TrimPrefix(name, section.prefix), ".md")
			lines = append(lines, fmt.Sprintf("  - [%s](ansible/%s)", label, name))
		}
	}

	return lines, nil
}
