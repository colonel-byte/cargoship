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

package clustercfg

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// parseYAML parses src into a document whose anchors and aliases have been resolved away: an
// anchored value is replaced by the value itself, and an alias by the node its anchor holds.
//
// Everything in this package finds values with go-yaml's path filter, and that filter cannot walk
// into either -- "auth: &auth" is an anchor node where it expects a mapping, and "auth: *auth" an
// alias -- so a configuration that shares one credential block between two registries used to read
// as a configuration with nothing in it to encrypt. Resolving them first is what makes those
// documents work rather than silently do nothing.
//
// The nodes that come back are the ones the parser built, so their tokens still point at the bytes
// they were parsed from, which is what the splices in this package rewrite. An aliased value
// therefore resolves to the anchored value's own bytes: encrypting a registry whose auth block is
// "*auth" rewrites the block the anchor introduced, and both registries read the ciphertext, which
// is what sharing a value means.
func parseYAML(src []byte) (*ast.File, error) {
	file, err := parser.ParseBytes(src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	// One map for the whole file rather than one per document: an anchor is scoped to its document,
	// but a name reused across documents is resolved to the nearest one before it either way, since
	// documents are walked in order.
	anchors := map[string]ast.Node{}
	for _, doc := range file.Docs {
		if doc == nil || doc.Body == nil {
			continue
		}
		body, err := resolveAnchors(doc.Body, anchors)
		if err != nil {
			return nil, err
		}
		doc.Body = body
	}
	return file, nil
}

// resolveAnchors returns node with anchors and aliases resolved away, recording each anchor it
// passes so that a later alias can be replaced by the value it names. YAML requires an anchor to
// appear before the aliases that use it, so one pass in document order is enough.
//
// Nodes are rewritten in place, which is safe because every caller parses the document for itself
// and throws the tree away after the value it wanted has been spliced.
func resolveAnchors(node ast.Node, anchors map[string]ast.Node) (ast.Node, error) {
	switch n := node.(type) {
	case *ast.AnchorNode:
		value, err := resolveAnchors(n.Value, anchors)
		if err != nil {
			return nil, err
		}
		if n.Name != nil {
			anchors[n.Name.GetToken().Value] = value
		}
		return value, nil

	case *ast.AliasNode:
		if n.Value == nil {
			return nil, fmt.Errorf("an alias at line %d names no anchor", n.GetToken().Position.Line)
		}
		name := n.Value.GetToken().Value
		value, ok := anchors[name]
		if !ok {
			// The parser accepts this; a document that uses an alias before its anchor is one whose
			// values cannot all be found, and saying so beats reporting them as absent.
			return nil, fmt.Errorf("the alias *%s at line %d has no anchor before it", name, n.GetToken().Position.Line)
		}
		return value, nil

	case *ast.MappingNode:
		// A merge key stands for the keys of the mappings it names, which the path filter cannot
		// walk either: a registry that reaches its credentials through "<<: *auth" holds no auth
		// key of its own for the filter to find. Adding them to the mapping is what makes the
		// document walkable, and the order they are added in is their precedence, because the
		// filter answers with the first key it matches.
		//
		// YAML gives a mapping's own keys precedence over merged ones, and an earlier merge source
		// precedence over a later one. So the mapping's own keys come first, then each merged key
		// no earlier key already set. A merged key that is already set is dropped rather than added
		// a second time: it is not the value this mapping resolves to, and keeping it would leave
		// the mapping holding the same key twice.
		var own, merged, merges []*ast.MappingValueNode
		for _, value := range n.Values {
			resolved, err := resolveAnchors(value, anchors)
			if err != nil {
				return nil, err
			}
			mv, ok := resolved.(*ast.MappingValueNode)
			if !ok {
				// Every value of a mapping is a key and its value, and resolving one gives it back,
				// so this is unreachable. It is an error rather than a skip because dropping the
				// value here is what reporting a credential as absent looks like.
				return nil, fmt.Errorf("the mapping entry at line %d resolved to a %s rather than to a key and its value",
					value.GetToken().Position.Line, strings.ToLower(resolved.Type().String()))
			}
			if mv.Key == nil || !mv.Key.IsMergeKey() {
				own = append(own, mv)
				continue
			}
			sources, err := mergeSources(mv)
			if err != nil {
				return nil, err
			}
			merged = append(merged, sources...)
			// The merge key itself is kept so that nothing the parser built is dropped from the
			// tree. Nothing reads it: the splices in this package rewrite the bytes the tokens
			// point at rather than write the tree back out.
			merges = append(merges, mv)
		}

		named := make(map[string]bool, len(own))
		for _, mv := range own {
			named[mappingKeyName(mv)] = true
		}
		values := make([]*ast.MappingValueNode, 0, len(own)+len(merged)+len(merges))
		values = append(values, own...)
		for _, mv := range merged {
			name := mappingKeyName(mv)
			if name != "" && named[name] {
				continue
			}
			named[name] = true
			values = append(values, mv)
		}
		n.Values = append(values, merges...)
		return n, nil

	case *ast.MappingValueNode:
		value, err := resolveAnchors(n.Value, anchors)
		if err != nil {
			return nil, err
		}
		n.Value = value
		return n, nil

	case *ast.SequenceNode:
		for i, value := range n.Values {
			resolved, err := resolveAnchors(value, anchors)
			if err != nil {
				return nil, err
			}
			n.Values[i] = resolved
			// The parser keeps a second view of the same items, which nothing here reads but which
			// would be left pointing at the unresolved nodes if it were not kept in step.
			if i < len(n.Entries) && n.Entries[i] != nil {
				n.Entries[i].Value = resolved
			}
		}
		return n, nil

	default:
		return node, nil
	}
}

// mappingKeyName returns the key a mapping entry is written under. An entry this package cannot
// name comes back as the empty string, which is never treated as a key another entry already set.
func mappingKeyName(mv *ast.MappingValueNode) string {
	if mv.Key == nil {
		return ""
	}
	return mv.Key.GetToken().Value
}

// mergeSources returns the entries a merge key contributes, in the order it names them. YAML lets a
// merge key take one mapping or a list of them, and either may be written as an alias, which
// resolveAnchors has already replaced by the mapping the anchor holds.
//
// Anything else is an error rather than a merge that quietly adds nothing. A merge this package
// walks past is a set of keys it cannot reach, and the whole-file commands would then report the
// document as having had nothing to do -- which reads as success over credentials still sitting
// there in the clear.
func mergeSources(mv *ast.MappingValueNode) ([]*ast.MappingValueNode, error) {
	line := mv.GetToken().Position.Line

	switch target := mv.Value.(type) {
	case *ast.MappingNode:
		return target.Values, nil

	case *ast.MappingValueNode:
		// A mapping holding a single key, which the parser hands back as the entry itself.
		return []*ast.MappingValueNode{target}, nil

	case *ast.SequenceNode:
		var sources []*ast.MappingValueNode
		for _, value := range target.Values {
			switch source := value.(type) {
			case *ast.MappingNode:
				sources = append(sources, source.Values...)
			case *ast.MappingValueNode:
				sources = append(sources, source)
			default:
				return nil, fmt.Errorf("the merge key at line %d names a %s in its list; a merge key takes a mapping or a list of mappings",
					line, strings.ToLower(value.Type().String()))
			}
		}
		return sources, nil

	case nil:
		return nil, fmt.Errorf("the merge key at line %d names nothing; a merge key takes a mapping or a list of mappings", line)

	default:
		return nil, fmt.Errorf("the merge key at line %d names a %s; a merge key takes a mapping or a list of mappings",
			line, strings.ToLower(target.Type().String()))
	}
}
