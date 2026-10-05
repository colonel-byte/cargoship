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
	"go/ast"
	"go/token"
	"strings"
)

// Components is the packaged-component vocabulary recovered for one distro/version -- the
// values a flag accepts, as opposed to the flag names ExtractFlags recovers. k3s and RKE2 both
// declare these as plain package-level lists outside the files that hold the []cli.Flag{...}
// literal, so they need their own pull and their own extraction pass.
type Components struct {
	// Disable is the set of packaged components `disable:` accepts.
	Disable []string
	// CNI is the set of values `cni:` accepts. Empty for distros that don't declare one.
	CNI []string
	// Ingress is the set of values `ingress-controller:` accepts. Empty for distros that
	// don't declare one.
	Ingress []string
}

// StringListDecl finds the top-level const or var named name across files and returns the string
// items it declares. Both shapes k3s and RKE2 use are handled:
//
//	const DisableItems = "coredns, servicelb, traefik"  // k3s, pkg/cli/cmds/stage.go
//	var DisableItems = []string{"rke2-coredns", ...}    // rke2, pkg/cli/types.go
//
// Elements that aren't string literals are skipped rather than guessed at, the same way
// buildFlag surfaces an unresolvable flag name instead of inventing one.
func StringListDecl(name string, files ...*ast.File) ([]string, bool) {
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != len(vs.Values) {
					continue
				}
				for i, ident := range vs.Names {
					if ident.Name == name {
						return stringListValue(vs.Values[i])
					}
				}
			}
		}
	}
	return nil, false
}

// stringListValue renders one declaration's value as a list of items.
func stringListValue(e ast.Expr) ([]string, bool) {
	if s, ok := stringLit(e); ok {
		var items []string
		for part := range strings.SplitSeq(s, ",") {
			if part = strings.TrimSpace(part); part != "" {
				items = append(items, part)
			}
		}
		return items, true
	}

	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	at, ok := cl.Type.(*ast.ArrayType)
	if !ok {
		return nil, false
	}
	if elt, ok := at.Elt.(*ast.Ident); !ok || elt.Name != "string" {
		return nil, false
	}

	var items []string
	for _, el := range cl.Elts {
		if s, ok := stringLit(el); ok {
			items = append(items, s)
		}
	}
	return items, true
}

// DisableSetCalls collects the value of every `<ctx>.Set("disable", "<literal>")` call in file.
// RKE2's validateCNI disables the multus charts that way rather than listing them in
// pkg/cli/types.go, so scanning for the call recovers them without hardcoding chart names here.
func DisableSetCalls(file *ast.File) []string {
	var values []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Set" {
			return true
		}
		if key, ok := stringLit(call.Args[0]); !ok || key != "disable" {
			return true
		}
		if v, ok := stringLit(call.Args[1]); ok {
			values = append(values, v)
		}
		return true
	})
	return values
}
