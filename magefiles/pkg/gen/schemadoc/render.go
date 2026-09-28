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

package schemadoc

import (
	"fmt"
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/page"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/schema"
	"github.com/nao1215/markdown"
)

// writePropertiesTable renders one object's "properties" as a table, or nothing for an
// object with none -- a map type such as RegistryOverrideMap, whose "properties" are suggested
// keys rather than a fixed schema, is left to its own description to explain.
func writePropertiesTable(md *markdown.Markdown, obj map[string]any) {
	props, ok := obj[schema.PropertiesKey].(map[string]any)
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
		prop := asObject(props[name])

		isRequired := "no"
		if required[name] {
			isRequired = "yes"
		}

		def := ""
		if v, ok := prop["default"]; ok {
			def = "`" + page.RenderYAMLValue(v) + "`"
		}

		rows = append(rows, []string{
			"`" + name + "`",
			propertyType(prop),
			isRequired,
			def,
			propertyDescription(prop),
		})
	}

	// Wrapped in a div so docs/css/root.css can force every schema table to the same column
	// widths, rather than each table sizing itself to its own content. A blank line on each side of
	// the table is required: it is what ends the raw-HTML block pulldown-cmark opened on the "<div>"
	// line, so the table itself is still parsed as Markdown rather than swallowed as HTML text.
	md.PlainText(`<div class="schema-table">`)
	md.PlainText("")
	for _, line := range page.PaddedTable([]string{"Property", "Type", "Required", "Default", "Description"}, rows) {
		md.PlainText(line)
	}
	md.PlainText("")
	md.PlainText("</div>")
	md.PlainText("")
}

// asObject reads one schema field as an object, yielding nil for a field that is absent or of
// another type. A schema here is a decoded JSON document, so every read is a type assertion, and
// nil is what each caller wants for a field it does not find: propertyType renders it as "any".
func asObject(v any) map[string]any {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return obj
}

// asString is asObject for a string field.
func asString(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// propertyType renders a property's type, following "$ref" to the $defs section its own
// page links to, and "items" or "additionalProperties" one level into an array or map.
func propertyType(prop map[string]any) string {
	if ref, ok := prop["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/$defs/")
		return fmt.Sprintf("[%s](#%s)", name, strings.ToLower(name))
	}

	t := asString(prop["type"])
	switch t {
	case "array":
		return "array of " + propertyType(asObject(prop["items"]))
	case "object":
		if ap, ok := prop["additionalProperties"].(map[string]any); ok {
			return "map[string]" + propertyType(ap)
		}
		return "object"
	case "":
		if alt, ok := prop["oneOf"].([]any); ok {
			return propertyAlternatives(alt)
		}
		if alt, ok := prop["anyOf"].([]any); ok {
			return propertyAlternatives(alt)
		}
		return "any"
	default:
		return "`" + t + "`"
	}
}

// propertyAlternatives renders the branches of a "oneOf"/"anyOf" property, such as
// worker_concurrency's "oneof_type=string;integer", jsonschema tag.
func propertyAlternatives(alternatives []any) string {
	types := make([]string, 0, len(alternatives))
	for _, a := range alternatives {
		if branch, ok := a.(map[string]any); ok {
			types = append(types, propertyType(branch))
		}
	}
	return strings.Join(types, " or ")
}

// propertyDescription renders a property's description, noting its allowed values when the
// schema restricts it to an enum -- the same way ansibledoc's renderDescription lists an Ansible
// option's choices.
func propertyDescription(prop map[string]any) string {
	desc := asString(prop["description"])

	// A property with no "type" key, no "$ref", and no oneOf/anyOf alternatives (see
	// propertyType) is a Go "any" field with no doc comment reachable from here -- typically
	// a field defined outside this module, whose comment schema.Reflect has no way to walk.
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
