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

// Package extract statically parses k3s/RKE2's pkg/cli/cmds source (via go/ast) to recover the
// urfave/cli flag list they declare, without ever compiling or importing that package. Their
// go.mod replace directives make importing them as a real dependency unsafe -- see
// docs/agent/design-config-codegen.md for the full reasoning.
package extract

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
)

// Flag is one urfave/cli flag recovered from a []cli.Flag{...} literal.
type Flag struct {
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Slice       bool   `json:"slice,omitempty"`
	Hidden      bool   `json:"hidden,omitempty"`
	Destination string `json:"destination,omitempty"`
	Value       string `json:"value,omitempty"`
	// Unresolved is set when the []cli.Flag{...} literal referenced a bare identifier (e.g.
	// ConfigFlag) that isn't declared as a "Name = &cli.XFlag{...}" var anywhere in the parsed
	// file set. k3s spreads shared flags across files this package is often not given; losing
	// those is an acceptable, visible gap rather than a reason to fail extraction.
	Unresolved bool `json:"unresolved,omitempty"`
	// UnknownType is set when the flag's cli.XFlag type isn't in flagTypes.
	UnknownType bool `json:"unknownType,omitempty"`
	// AliasOf holds the canonical flag Name when this flag's Destination matches one already
	// seen earlier in the same list -- k3s's pattern for a deprecated flag name that populates
	// the same struct field as its replacement.
	AliasOf string `json:"aliasOf,omitempty"`
}

// Manifest is the extracted flag set for one (distro, version, target) triple.
type Manifest struct {
	Distro  string `json:"distro"`
	Version string `json:"version"`
	Target  string `json:"target"`
	Flags   []Flag `json:"flags"`
}

var flagTypes = map[string]struct {
	goType string
	slice  bool
}{
	"StringFlag":      {"string", false},
	"BoolFlag":        {"bool", false},
	"IntFlag":         {"int", false},
	"Int64Flag":       {"int64", false},
	"DurationFlag":    {"string", false},
	"StringSliceFlag": {"string", true},
	"IntSliceFlag":    {"int", true},
}

// BuildVarIndex indexes every top-level "Name = &cli.XFlag{...}" declaration across files, so
// ExtractFlags can resolve the bare identifiers a []cli.Flag{...} literal references -- k3s
// declares many shared flags once and lists them by name in both the server and agent flag
// slices.
func BuildVarIndex(files ...*ast.File) map[string]*ast.CompositeLit {
	idx := map[string]*ast.CompositeLit{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != len(vs.Values) {
					continue
				}
				for i, name := range vs.Names {
					if lit := flagLiteral(vs.Values[i]); lit != nil {
						idx[name.Name] = lit
					}
				}
			}
		}
	}
	return idx
}

// ExtractFlags finds the []cli.Flag{...} literal in file and resolves each element: inline
// &cli.XFlag{...} literals directly, bare identifiers via varIndex.
func ExtractFlags(varIndex map[string]*ast.CompositeLit, file *ast.File) ([]Flag, error) {
	list := findFlagsList(file)
	if list == nil {
		return nil, fmt.Errorf("no []cli.Flag{...} literal found in %s", file.Name.Name)
	}

	flags := make([]Flag, 0, len(list.Elts))
	seenDestination := map[string]string{}

	for _, el := range list.Elts {
		var lit *ast.CompositeLit
		var unresolvedName string

		switch v := el.(type) {
		case *ast.UnaryExpr:
			lit = flagLiteral(v)
		case *ast.Ident:
			if l, ok := varIndex[v.Name]; ok {
				lit = l
			} else {
				unresolvedName = v.Name
			}
		}

		if lit == nil {
			flags = append(flags, Flag{Name: unresolvedName, Unresolved: true})
			continue
		}

		flags = append(flags, buildFlag(lit, seenDestination))
	}
	return flags, nil
}

// findFlagsList locates the single []cli.Flag{...} composite literal in file, whether it's a
// top-level var's value (k3s's server.go: "var ServerFlags = []cli.Flag{...}") or nested inside
// a returned struct literal (k3s's agent.go: "&cli.Command{... Flags: []cli.Flag{...}}").
func findFlagsList(file *ast.File) *ast.CompositeLit {
	var found *ast.CompositeLit
	ast.Inspect(file, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		at, ok := cl.Type.(*ast.ArrayType)
		if !ok {
			return true
		}
		sel, ok := at.Elt.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "cli" && sel.Sel.Name == "Flag" {
			found = cl
			return false
		}
		return true
	})
	return found
}

// flagLiteral returns cl if e is "&cli.XFlag{...}", else nil.
func flagLiteral(e ast.Expr) *ast.CompositeLit {
	u, ok := e.(*ast.UnaryExpr)
	if !ok || u.Op != token.AND {
		return nil
	}
	cl, ok := u.X.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	if _, ok := flagKind(cl); !ok {
		return nil
	}
	return cl
}

// flagKind returns the cli.XFlag type name (e.g. "StringFlag") for a "cli.XFlag{...}" literal.
func flagKind(cl *ast.CompositeLit) (string, bool) {
	sel, ok := cl.Type.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "cli" {
		return "", false
	}
	return sel.Sel.Name, true
}

func buildFlag(lit *ast.CompositeLit, seenDestination map[string]string) Flag {
	f := Flag{}

	kind, _ := flagKind(lit)
	if t, ok := flagTypes[kind]; ok {
		f.Type = t.goType
		f.Slice = t.slice
	} else {
		f.UnknownType = true
	}

	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Name":
			if s, ok := stringLit(kv.Value); ok {
				f.Name = s
			} else {
				// Non-literal Name (e.g. an imported package's string const, or a
				// concatenation expression) -- surface it rather than silently emitting an
				// empty flag name.
				f.Name = types.ExprString(kv.Value)
				f.Unresolved = true
			}
		case "Hidden":
			f.Hidden = boolLit(kv.Value)
		case "Destination":
			f.Destination = types.ExprString(kv.Value)
		case "Value":
			f.Value = valueText(kv.Value)
		}
	}

	if f.Destination != "" {
		if canonical, ok := seenDestination[f.Destination]; ok {
			f.AliasOf = canonical
		} else {
			seenDestination[f.Destination] = f.Name
		}
	}

	return f
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

func boolLit(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "true"
}

// valueText renders a flag's Value field as source text for the manifest. Literal strings are
// unquoted for readability; anything else (consts, arithmetic like "10 * time.Minute") is kept
// as its Go source form -- informational only, never evaluated. The generated struct does not
// use this to populate Go zero values.
func valueText(e ast.Expr) string {
	if s, ok := stringLit(e); ok {
		return s
	}
	return types.ExprString(e)
}
