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

package schema

import (
	"fmt"

	"github.com/colonel-byte/cargoship/pkg/helmvalues"
)

// valuesPath is where a package's own values schema is grafted into the inventory
// schema. spec and spec.config are both $ref nodes in the generated document, so the
// $defs entry is the only place the subtree can be edited.
var valuesPath = []string{"$defs", "ZarfClusterConfig", "properties", "values"}

// ComposeInventory returns the inventory schema with a package's values schema grafted
// onto spec.config.values, so an editor can complete and check the one block of an
// inventory whose vocabulary belongs to the package rather than to cargoship.
//
// note is appended to the grafted node's description - the package name and version, so
// a generated file left lying around says which package it came from.
//
// The result describes the override subtree on its own, while cargoship validates the
// package's values merged with the overrides. It therefore catches the mistakes that are
// wrong in either reading - an unknown key, a wrong type, a name outside an enum or
// pattern - and does not attempt to reproduce install-time validation.
func ComposeInventory(values helmvalues.Schema, note string) (map[string]any, error) {
	doc, err := Load(KindInventory)
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return doc, nil
	}

	node, err := valuesNode(doc)
	if err != nil {
		return nil, err
	}

	subtree := prepareSubtree(map[string]any(values))

	// The generated description explains what the block is for; the package's own root
	// description, if it has one, explains what this package accepts. Both are useful
	// hover text, so they are joined rather than one replacing the other.
	parts := make([]string, 0, 3)
	if d, ok := node["description"].(string); ok && d != "" {
		parts = append(parts, d)
	}
	if d, ok := subtree["description"].(string); ok && d != "" {
		parts = append(parts, d)
	}
	if note != "" {
		parts = append(parts, note)
	}

	for k, v := range subtree {
		node[k] = v
	}
	if len(parts) > 0 {
		node["description"] = joinDescriptions(parts)
	}
	return doc, nil
}

// valuesNode walks to the node the values schema is grafted onto. It fails loudly rather
// than creating the path: an absent node means the generated schema's shape changed, and
// silently writing a new one would emit a document that validates nothing.
func valuesNode(doc map[string]any) (map[string]any, error) {
	node := doc
	for i, key := range valuesPath {
		next, ok := node[key].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("inventory schema has no %s node, cannot graft package values", pathString(valuesPath[:i+1]))
		}
		node = next
	}
	return node, nil
}

// prepareSubtree copies a values schema into a form that is correct nested inside another
// document.
//
// $schema and $id are dropped: they describe a standalone document, and a nested $schema
// is read as a dialect switch by some validators. required is dropped at every depth,
// because an inventory supplies overrides on top of the values the package already ships
// - a key the package's own values.yaml provides is not required of the operator, and
// flagging it would be a false positive on a correct file.
//
// Everything else is kept as written. additionalProperties, type, enum, pattern, examples
// and description all mean the same thing against the override subtree as they do against
// the merged result.
func prepareSubtree(values map[string]any) map[string]any {
	out, ok := stripRequired(values).(map[string]any)
	if !ok {
		// Unreachable: stripRequired returns a map for a map. Handled rather than asserted
		// so a schema that is somehow not an object cannot panic the command.
		return map[string]any{}
	}
	delete(out, "$schema")
	delete(out, "$id")
	return out
}

// schemaMapKeywords are the keywords whose value is a map of *names* to schemas. The keys
// under them come from the package's values, so a package with a value called "required"
// must not have it mistaken for the keyword.
var schemaMapKeywords = map[string]bool{
	"properties":        true,
	"patternProperties": true,
	"$defs":             true,
	"definitions":       true,
	"dependentSchemas":  true,
}

// dataKeywords are the keywords whose value is arbitrary instance data rather than a
// schema. Nothing inside them is a keyword, so they are copied untouched.
var dataKeywords = map[string]bool{
	"enum":     true,
	"examples": true,
	"const":    true,
	"default":  true,
}

// stripRequired deep-copies a schema object without any required keyword. The copy is what
// makes this safe to call on a schema the caller still holds.
func stripRequired(node any) any {
	switch n := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(n))
		for k, v := range n {
			switch {
			case k == "required":
				continue
			case dataKeywords[k]:
				out[k] = v
			case schemaMapKeywords[k]:
				out[k] = stripRequiredInNamedSchemas(v)
			default:
				out[k] = stripRequired(v)
			}
		}
		return out
	case []any:
		out := make([]any, len(n))
		for i, v := range n {
			out[i] = stripRequired(v)
		}
		return out
	default:
		return node
	}
}

// stripRequiredInNamedSchemas walks a map of names to schemas, descending into the values
// without ever reading the names as keywords.
func stripRequiredInNamedSchemas(node any) any {
	n, ok := node.(map[string]any)
	if !ok {
		return node
	}
	out := make(map[string]any, len(n))
	for name, sub := range n {
		out[name] = stripRequired(sub)
	}
	return out
}

func joinDescriptions(parts []string) string {
	out := parts[0]
	for _, p := range parts[1:] {
		out += "\n\n" + p
	}
	return out
}

func pathString(path []string) string {
	out := path[0]
	for _, p := range path[1:] {
		out += "." + p
	}
	return out
}
