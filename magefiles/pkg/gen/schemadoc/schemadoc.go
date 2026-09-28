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

// Package schemadoc renders docs/schema/*.md from the JSON schemas gen/schema reflects.
//
// The page is built from the reflection in memory rather than from the schema/*.json files on
// disk, so it is correct whether or not the schema target has been run in this checkout, and the
// two can never disagree. How one property becomes a table cell lives in render.go.
package schemadoc

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/page"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/schema"
	"github.com/nao1215/markdown"
)

// Dir is where Generate renders one page per schema.Targets() entry. It is separate from
// schema.Dir: those *.json files are pinned by URL and cannot move, while this is a plain docs/
// page named for readability rather than for a stable link.
const Dir = "docs/schema"

// Generate renders docs/schema/*.md, one page per schema target.
func Generate() error {
	for _, t := range schema.Targets() {
		raw, err := schema.Reflect(t.Struct, t.StructPath, t.Namer())
		if err != nil {
			return fmt.Errorf("unable to generate %s: %w", t.Path, err)
		}

		var root map[string]any
		if err := json.Unmarshal(raw, &root); err != nil {
			return fmt.Errorf("unable to read back %s as a schema doc: %w", t.Path, err)
		}

		if err := writeDoc(t, root); err != nil {
			return err
		}
	}
	return nil
}

// writeDoc renders one docs/schema/<name>.md: the root object's own properties, followed by
// one section per $defs entry the root, or another $defs entry, points at through a "$ref".
func writeDoc(t schema.Target, root map[string]any) error {
	return page.WriteGenerated(filepath.Join(Dir, t.DocFile), func(md *markdown.Markdown) error {
		md.H2(t.DocTitle)
		md.PlainText("")
		if desc, ok := root["description"].(string); ok && desc != "" {
			md.PlainText(desc)
			md.PlainText("")
		}
		md.PlainTextf("Rendered from `schema/%s`.", t.Path)
		md.PlainText("")

		writePropertiesTable(md, root)

		defs := asObject(root["$defs"])
		names := make([]string, 0, len(defs))
		for name := range defs {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			def := asObject(defs[name])
			if def == nil {
				continue
			}
			md.H3(name)
			md.PlainText("")
			if desc, ok := def["description"].(string); ok && desc != "" {
				md.PlainText(desc)
				md.PlainText("")
			}
			writePropertiesTable(md, def)
		}
		return nil
	})
}
