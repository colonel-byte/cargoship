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
	"bytes"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/schema"
	"github.com/nao1215/markdown"
	"github.com/stretchr/testify/require"
)

func TestPropertyType(t *testing.T) {
	for name, tc := range map[string]struct {
		prop map[string]any
		want string
	}{
		// A $ref becomes a link to the section this same page renders for that $defs entry, and
		// the anchor is the lowercased heading mdBook generates.
		"a reference": {
			prop: map[string]any{"$ref": "#/$defs/ZarfHost"},
			want: "[ZarfHost](#zarfhost)",
		},
		"a scalar": {
			prop: map[string]any{"type": "string"},
			want: "`string`",
		},
		"an array": {
			prop: map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			want: "array of `string`",
		},
		"an array of references": {
			prop: map[string]any{"type": "array", "items": map[string]any{"$ref": "#/$defs/ZarfFile"}},
			want: "array of [ZarfFile](#zarffile)",
		},
		"a map": {
			prop: map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
			want: "map[string]`string`",
		},
		"an object with a fixed shape": {
			prop: map[string]any{"type": "object"},
			want: "object",
		},
		// oneof_type=string;integer on a Go field renders as alternatives rather than a type.
		"alternatives": {
			prop: map[string]any{"oneOf": []any{
				map[string]any{"type": "string"},
				map[string]any{"type": "integer"},
			}},
			want: "`string` or `integer`",
		},
		"anyOf alternatives": {
			prop: map[string]any{"anyOf": []any{map[string]any{"type": "boolean"}}},
			want: "`boolean`",
		},
		// A Go "any" field whose schema carries nothing at all.
		"an unconstrained field": {
			prop: map[string]any{},
			want: "any",
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, propertyType(tc.prop))
		})
	}
}

func TestPropertyDescription(t *testing.T) {
	for name, tc := range map[string]struct {
		prop map[string]any
		want string
	}{
		"prose is passed through": {
			prop: map[string]any{"type": "string", "description": "What it does."},
			want: "What it does.",
		},
		"an enum is listed after the prose": {
			prop: map[string]any{
				"type": "string", "description": "Pick one.",
				"enum": []any{"a", "b"},
			},
			want: "Pick one. One of `a`, `b`.",
		},
		"an enum with no prose stands alone": {
			prop: map[string]any{"type": "string", "enum": []any{1, 2}},
			want: "One of `1`, `2`.",
		},
		// The cell would otherwise be blank next to a Type of "any", with nothing saying why.
		"an unconstrained field explains itself": {
			prop: map[string]any{},
			want: "Accepts any value; cargoship does not constrain its shape here.",
		},
		// A typed property with no doc comment is a missing comment, not an unconstrained field,
		// so it gets no explanation.
		"a typed field with no comment stays blank": {
			prop: map[string]any{"type": "string"},
			want: "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, propertyDescription(tc.prop))
		})
	}
}

func TestWritePropertiesTable(t *testing.T) {
	out := renderTable(t, map[string]any{
		schema.PropertiesKey: map[string]any{
			"zulu":  map[string]any{"type": "string", "description": "Last by name."},
			"alpha": map[string]any{"type": "integer", "default": 3, "description": "First."},
		},
		"required": []any{"alpha"},
	})

	// The div is what docs/css/root.css sizes every schema table by, and the blank lines around
	// it are what keep the table itself parsed as Markdown.
	require.Contains(t, out, "<div class=\"schema-table\">\n\n|")
	require.Contains(t, out, "|\n\n</div>")

	// Rows are in name order rather than in whatever order the map iterated.
	require.Less(t, strings.Index(out, "`alpha`"), strings.Index(out, "`zulu`"))

	alpha := rowFor(t, out, "`alpha`")
	require.Contains(t, alpha, "`integer`")
	require.Contains(t, alpha, "| yes ")
	require.Contains(t, alpha, "`3`")

	zulu := rowFor(t, out, "`zulu`")
	require.Contains(t, zulu, "| no ")
}

// TestWritePropertiesTableOnAMapType renders nothing rather than an empty table: a map's
// "properties" are suggested keys, and its own description explains them.
func TestWritePropertiesTableOnAMapType(t *testing.T) {
	// Trimmed, because the markdown builder terminates even an empty document with a newline.
	require.Empty(t, strings.TrimSpace(renderTable(t, map[string]any{"type": "object"})))
	require.Empty(t, strings.TrimSpace(renderTable(t, map[string]any{schema.PropertiesKey: map[string]any{}})))
}

func renderTable(t *testing.T, obj map[string]any) string {
	t.Helper()
	var buf bytes.Buffer
	md := markdown.NewMarkdown(&buf)
	writePropertiesTable(md, obj)
	require.NoError(t, md.Build())
	return buf.String()
}

func rowFor(t *testing.T, table string, cell string) string {
	t.Helper()
	for _, line := range strings.Split(table, "\n") {
		if strings.Contains(line, cell) {
			return line
		}
	}
	require.FailNowf(t, "missing row", "no row holds %s in:\n%s", cell, table)
	return ""
}
