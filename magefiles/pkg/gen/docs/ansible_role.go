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

	goyaml "github.com/goccy/go-yaml"
	"github.com/nao1215/markdown"
)

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
		if spec, actual := renderYAMLValue(o.Default), renderYAMLValue(item.Value); spec != actual {
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

// writeRoleDoc renders docs/ansible/role_<role>.md.
func writeRoleDoc(role string, entry roleEntrypoint, options []namedOption) error {
	return writeGeneratedPage(fmt.Sprintf("role_%s.md", role), func(md *markdown.Markdown) error {
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
				"`" + renderYAMLValue(o.Default) + "`",
				renderDescription(o),
			})
		}
		for _, line := range paddedTable([]string{"Variable", "Type", "Default", "Description"}, rows) {
			md.PlainText(line)
		}
		md.PlainText("")
		return nil
	})
}
