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

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nao1215/markdown"
)

// schemaDocsDir is where generateSchemaDocs renders one page per schemaTargets() entry. It is
// separate from schemaDir: schemaDir's *.json files are pinned by URL and cannot move, while this
// is a plain docs/ page named for readability rather than for a stable link.
const schemaDocsDir = "docs/schema"

// generateSchemaDocs renders docs/schema/*.md from the same reflection schemaTargets() feeds to
// Generate.Schema, rather than from the schema/*.json files on disk -- so the page is correct
// whether or not Generate.Schema has been run in this checkout, and the two can never disagree.
func generateSchemaDocs() error {
	for _, s := range schemaTargets() {
		raw, err := generateV1Alpha1Schema(s.schemaStruct, s.structPath, s.namer())
		if err != nil {
			return fmt.Errorf("unable to generate %s: %w", s.schemaPath, err)
		}

		var root map[string]any
		if err := json.Unmarshal(raw, &root); err != nil {
			return fmt.Errorf("unable to read back %s as a schema doc: %w", s.schemaPath, err)
		}

		if err := writeSchemaDoc(s, root); err != nil {
			return err
		}
	}
	return nil
}

// writeSchemaDoc renders one docs/schema/<name>.md: the root object's own properties, followed by
// one section per $defs entry the root, or another $defs entry, points at through a "$ref".
func writeSchemaDoc(s schema, root map[string]any) error {
	path := filepath.Join(schemaDocsDir, s.docFile)
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

	md := markdown.NewMarkdown(f)
	md.PlainText(generatedBanner)
	md.PlainText("")
	md.H2(s.docTitle)
	md.PlainText("")
	if desc, ok := root["description"].(string); ok && desc != "" {
		md.PlainText(desc)
		md.PlainText("")
	}
	md.PlainTextf("Rendered from `schema/%s`.", s.schemaPath)
	md.PlainText("")

	writeSchemaPropertiesTable(md, root)

	defs, _ := root["$defs"].(map[string]any)
	names := make([]string, 0, len(defs))
	for name := range defs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		def, ok := defs[name].(map[string]any)
		if !ok {
			continue
		}
		md.H3(name)
		md.PlainText("")
		if desc, ok := def["description"].(string); ok && desc != "" {
			md.PlainText(desc)
			md.PlainText("")
		}
		writeSchemaPropertiesTable(md, def)
	}

	return md.Build()
}

// writeSchemaPropertiesTable renders one object's "properties" as a table, or nothing for an
// object with none -- a map type such as RegistryOverrideMap, whose "properties" are suggested
// keys rather than a fixed schema, is left to its own description to explain.
func writeSchemaPropertiesTable(md *markdown.Markdown, obj map[string]any) {
	props, ok := obj[propertiesKey].(map[string]any)
	if !ok || len(props) == 0 {
		return
	}

	required := map[string]bool{}
	if req, ok := obj["required"].([]any); ok {
		for _, r := range req {
			if name, ok := r.(string); ok {
				required[name] = true
			}
		}
	}

	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([][]string, 0, len(names))
	for _, name := range names {
		prop, _ := props[name].(map[string]any)

		isRequired := "no"
		if required[name] {
			isRequired = "yes"
		}

		def := ""
		if v, ok := prop["default"]; ok {
			def = "`" + renderYAMLValue(v) + "`"
		}

		rows = append(rows, []string{
			"`" + name + "`",
			schemaPropertyType(prop),
			isRequired,
			def,
			schemaPropertyDescription(prop),
		})
	}

	// Wrapped in a div so docs/css/root.css can force every schema table to the same column
	// widths, rather than each table sizing itself to its own content. A blank line on each side of
	// the table is required: it is what ends the raw-HTML block pulldown-cmark opened on the "<div>"
	// line, so the table itself is still parsed as Markdown rather than swallowed as HTML text.
	md.PlainText(`<div class="schema-table">`)
	md.PlainText("")
	for _, line := range paddedTable([]string{"Property", "Type", "Required", "Default", "Description"}, rows) {
		md.PlainText(line)
	}
	md.PlainText("")
	md.PlainText("</div>")
	md.PlainText("")
}

// schemaPropertyType renders a property's type, following "$ref" to the $defs section its own
// page links to, and "items" or "additionalProperties" one level into an array or map.
func schemaPropertyType(prop map[string]any) string {
	if ref, ok := prop["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/$defs/")
		return fmt.Sprintf("[%s](#%s)", name, strings.ToLower(name))
	}

	t, _ := prop["type"].(string)
	switch t {
	case "array":
		items, _ := prop["items"].(map[string]any)
		return "array of " + schemaPropertyType(items)
	case "object":
		if ap, ok := prop["additionalProperties"].(map[string]any); ok {
			return "map[string]" + schemaPropertyType(ap)
		}
		return "object"
	case "":
		if alt, ok := prop["oneOf"].([]any); ok {
			return schemaPropertyAlternatives(alt)
		}
		if alt, ok := prop["anyOf"].([]any); ok {
			return schemaPropertyAlternatives(alt)
		}
		return "any"
	default:
		return "`" + t + "`"
	}
}

// schemaPropertyAlternatives renders the branches of a "oneOf"/"anyOf" property, such as
// worker_concurrency's "oneof_type=string;integer", jsonschema tag.
func schemaPropertyAlternatives(alternatives []any) string {
	types := make([]string, 0, len(alternatives))
	for _, a := range alternatives {
		if branch, ok := a.(map[string]any); ok {
			types = append(types, schemaPropertyType(branch))
		}
	}
	return strings.Join(types, " or ")
}

// schemaPropertyDescription renders a property's description, noting its allowed values when the
// schema restricts it to an enum -- the same way renderDescription lists an Ansible option's
// choices.
func schemaPropertyDescription(prop map[string]any) string {
	desc, _ := prop["description"].(string)

	// A property with no "type" key, no "$ref", and no oneOf/anyOf alternatives (see
	// schemaPropertyType) is a Go "any" field with no doc comment reachable from here -- typically
	// a field defined outside this module, whose comment generateV1Alpha1Schema has no way to walk.
	// Note why "any" shows up rather than leaving the cell's Description blank with no explanation.
	if desc == "" {
		_, hasType := prop["type"]
		_, hasRef := prop["$ref"]
		_, hasOneOf := prop["oneOf"]
		_, hasAnyOf := prop["anyOf"]
		if !hasType && !hasRef && !hasOneOf && !hasAnyOf {
			desc = "Accepts any value; cargoship does not constrain its shape here."
		}
	}

	enum, ok := prop["enum"].([]any)
	if !ok || len(enum) == 0 {
		return desc
	}

	quoted := make([]string, 0, len(enum))
	for _, e := range enum {
		quoted = append(quoted, fmt.Sprintf("`%v`", e))
	}
	note := fmt.Sprintf("One of %s.", strings.Join(quoted, ", "))
	if desc == "" {
		return note
	}
	return desc + " " + note
}
