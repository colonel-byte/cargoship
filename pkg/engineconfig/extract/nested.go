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

package extract

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

// FieldNode describes one field of a nested config document, recovered from a real Go struct's
// json tags. A nil Children means the field is a leaf: a scalar, a slice, or a map, whose
// contents pass through unvalidated -- recursion stops at the first field whose type isn't
// itself a named struct declared in the same file, rather than guessing the shape of a
// slice-of-struct or a type from another package.
type FieldNode struct {
	Children map[string]FieldNode
}

// structIndex indexes every top-level "type X struct {...}" declaration in a file, by name.
func structIndex(file *ast.File) map[string]*ast.StructType {
	idx := map[string]*ast.StructType{}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if st, ok := ts.Type.(*ast.StructType); ok {
				idx[ts.Name.Name] = st
			}
		}
	}
	return idx
}

// ExtractNestedKeys walks rootType's fields in file, resolving each field's json tag name and,
// for a field whose type is itself a named struct declared in file, recursing into it. An
// embedded field naming another local struct (e.g. "APIServer" embedding "ControlPlaneComponent"
// with json:",inline") has its children merged directly into the embedding type's own node,
// matching how encoding/json flattens an inlined struct. An embedded field naming a type from
// another package (e.g. metav1.TypeMeta) carries no fields this walker can see, so it is
// skipped rather than guessed at.
func ExtractNestedKeys(file *ast.File, rootType string) (FieldNode, error) {
	idx := structIndex(file)
	st, ok := idx[rootType]
	if !ok {
		return FieldNode{}, fmt.Errorf("no struct type %q found in %s", rootType, file.Name.Name)
	}
	return walkStruct(idx, st, map[string]bool{rootType: true}), nil
}

// walkStruct builds one FieldNode's Children from st's fields. seen guards against a struct that
// (directly or through embedding) refers back to itself, which real kubeadm types never do, but
// an infinite loop is worse than a defensively dropped cycle.
func walkStruct(idx map[string]*ast.StructType, st *ast.StructType, seen map[string]bool) FieldNode {
	children := map[string]FieldNode{}

	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			mergeEmbedded(idx, f, seen, children)
			continue
		}

		name, ok := jsonFieldName(f)
		if !ok {
			continue
		}

		if branch, ok := resolveBranch(idx, f.Type, seen); ok {
			children[name] = branch
		} else {
			children[name] = FieldNode{}
		}
	}

	return FieldNode{Children: children}
}

// mergeEmbedded handles an anonymous field. A local struct type is inlined: its own children are
// merged into the caller's map so the embedding type exposes them as if declared directly.
// Anything else (a foreign-package type) is skipped -- there is no struct body to recurse into.
func mergeEmbedded(idx map[string]*ast.StructType, f *ast.Field, seen map[string]bool, children map[string]FieldNode) {
	ident, ok := f.Type.(*ast.Ident)
	if !ok {
		return
	}
	if seen[ident.Name] {
		return
	}
	embedded, ok := idx[ident.Name]
	if !ok {
		return
	}
	nextSeen := extendSeen(seen, ident.Name)
	for k, v := range walkStruct(idx, embedded, nextSeen).Children {
		children[k] = v
	}
}

// resolveBranch reports whether typ refers to a named struct declared in idx (directly or
// through one level of pointer indirection), and if so, its recursively-walked FieldNode.
func resolveBranch(idx map[string]*ast.StructType, typ ast.Expr, seen map[string]bool) (FieldNode, bool) {
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	ident, ok := typ.(*ast.Ident)
	if !ok {
		return FieldNode{}, false
	}
	if seen[ident.Name] {
		return FieldNode{}, false
	}
	st, ok := idx[ident.Name]
	if !ok {
		return FieldNode{}, false
	}
	return walkStruct(idx, st, extendSeen(seen, ident.Name)), true
}

func extendSeen(seen map[string]bool, name string) map[string]bool {
	next := make(map[string]bool, len(seen)+1)
	for k := range seen {
		next[k] = true
	}
	next[name] = true
	return next
}

// jsonFieldName returns f's json tag name, and false when the field has no usable name: no json
// tag at all, or an explicit json:"-".
func jsonFieldName(f *ast.Field) (string, bool) {
	if f.Tag == nil {
		return "", false
	}
	tag := jsonTagValue(f.Tag.Value)
	name, _, _ := strings.Cut(tag, ",")
	if name == "" || name == "-" {
		return "", false
	}
	return name, true
}

// jsonTagValue extracts the json struct tag's value from a raw Go tag literal (e.g.
// "`json:\"podSubnet,omitempty\"`").
func jsonTagValue(raw string) string {
	raw = strings.Trim(raw, "`")
	const prefix = `json:"`
	i := strings.Index(raw, prefix)
	if i == -1 {
		return ""
	}
	raw = raw[i+len(prefix):]
	end := strings.IndexByte(raw, '"')
	if end == -1 {
		return ""
	}
	return raw[:end]
}
