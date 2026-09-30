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

// This file renders a generated schema as markdown under docs/schema/. It reads the schema
// document the generation half produces rather than the Go types, so what is documented is
// what the schema actually says.

package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/magefiles/pkg/util"
	"github.com/nao1215/markdown"
)

// GenerateDocs renders docs/schema/*.md from the reflected Go structs.
func GenerateDocs() error {
	for _, s := range Targets() {
		raw, err := GenerateV1Alpha1Schema(s.SchemaStruct, s.StructPath, s.namer())
		if err != nil {
			return fmt.Errorf("unable to generate %s: %w", s.SchemaPath, err)
		}

		var root map[string]any
		if err := json.Unmarshal(raw, &root); err != nil {
			return fmt.Errorf("unable to read back %s as a schema doc: %w", s.SchemaPath, err)
		}

		if err := writeSchemaDoc(s, root); err != nil {
			return err
		}
	}
	return nil
}

func writeSchemaDoc(s Target, root map[string]any) error {
	path := filepath.Join(SchemaDocsDir, s.DocFile)
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
	md.PlainText(util.GeneratedBanner)
	md.PlainText("")
	md.H2(s.DocTitle)
	md.PlainText("")
	if desc, ok := root["description"].(string); ok && desc != "" {
		md.PlainText(desc)
		md.PlainText("")
	}
	md.PlainTextf("Rendered from `schema/%s`.", s.SchemaPath)
	md.PlainText("")

	writeSchemaPropertiesTable(md, root)

	defs, _ := root["$defs"].(map[string]any) //nolint:errcheck // zero value is the intended fallback for a key the schema does not carry
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
		prop, _ := props[name].(map[string]any) //nolint:errcheck // zero value is the intended fallback for a key the schema does not carry

		isRequired := "no"
		if required[name] {
			isRequired = "yes"
		}

		def := ""
		if v, ok := prop["default"]; ok {
			def = "`" + util.RenderYAMLValue(v) + "`"
		}

		rows = append(rows, []string{
			"`" + name + "`",
			schemaPropertyType(prop),
			isRequired,
			def,
			schemaPropertyDescription(prop),
		})
	}

	md.PlainText(`<div class="schema-table">`)
	md.PlainText("")
	for _, line := range util.PaddedTable([]string{"Property", "Type", "Required", "Default", "Description"}, rows) {
		md.PlainText(line)
	}
	md.PlainText("")
	md.PlainText("</div>")
	md.PlainText("")
}

func schemaPropertyType(prop map[string]any) string {
	if ref, ok := prop["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/$defs/")
		return fmt.Sprintf("[%s](#%s)", name, strings.ToLower(name))
	}

	t, _ := prop["type"].(string) //nolint:errcheck // zero value is the intended fallback for a key the schema does not carry
	switch t {
	case "array":
		items, _ := prop["items"].(map[string]any) //nolint:errcheck // zero value is the intended fallback for a key the schema does not carry
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

func schemaPropertyAlternatives(alternatives []any) string {
	types := make([]string, 0, len(alternatives))
	for _, a := range alternatives {
		if branch, ok := a.(map[string]any); ok {
			types = append(types, schemaPropertyType(branch))
		}
	}
	return strings.Join(types, " or ")
}

func schemaPropertyDescription(prop map[string]any) string {
	desc, _ := prop["description"].(string) //nolint:errcheck // zero value is the intended fallback for a key the schema does not carry

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
