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
)

// K3SFlagOption is the Drop/Hide subset of RKE2's own K3SFlagOption struct (pkg/cli/cmds/k3sopts.go)
// that matters for config.yaml key generation. RKE2 wraps every k3s flag it imports through a
// K3SFlagSet{...} map keyed by flag name, using this to decide whether the flag survives into
// RKE2's own command at all, and whether it's hidden.
type K3SFlagOption struct {
	Drop bool
	Hide bool
}

// BuildK3SFlagOptionIndex indexes every top-level "Name = &K3SFlagOption{...}" var declaration
// across files -- RKE2's k3sopts.go declares the four convenience vars (copyFlag, dropFlag,
// hideFlag, ignoreFlag) this way, and a K3SFlagSet{...} map literal's entries reference them by
// bare identifier.
func BuildK3SFlagOptionIndex(files ...*ast.File) map[string]K3SFlagOption {
	idx := map[string]K3SFlagOption{}
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
					if lit := k3sFlagOptionLiteral(vs.Values[i]); lit != nil {
						idx[name.Name] = k3sFlagOptionFromLit(lit)
					}
				}
			}
		}
	}
	return idx
}

// k3sFlagOptionLiteral returns cl if e is "&K3SFlagOption{...}", else nil.
func k3sFlagOptionLiteral(e ast.Expr) *ast.CompositeLit {
	u, ok := e.(*ast.UnaryExpr)
	if !ok || u.Op != token.AND {
		return nil
	}
	cl, ok := u.X.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	sel, ok := cl.Type.(*ast.Ident)
	if !ok || sel.Name != "K3SFlagOption" {
		return nil
	}
	return cl
}

// k3sFlagOptionFromLit reads the Drop/Hide fields out of a *K3SFlagOption composite literal.
func k3sFlagOptionFromLit(cl *ast.CompositeLit) K3SFlagOption {
	var opt K3SFlagOption
	for _, elt := range cl.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Drop":
			opt.Drop = boolLit(kv.Value)
		case "Hide":
			opt.Hide = boolLit(kv.Value)
		}
	}
	return opt
}

// FindK3SFlagSet locates the single "K3SFlagSet{...}" map composite literal in file -- RKE2's
// server.go/agent.go each build one inline as an argument to mustCmdFromK3S(...).
func FindK3SFlagSet(file *ast.File) *ast.CompositeLit {
	var found *ast.CompositeLit
	ast.Inspect(file, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		id, ok := cl.Type.(*ast.Ident)
		if !ok || id.Name != "K3SFlagSet" {
			return true
		}
		found = cl
		return false
	})
	return found
}

// ParseK3SFlagSet resolves a K3SFlagSet{...} map literal's entries into flag name -> K3SFlagOption,
// using optIndex (see BuildK3SFlagOptionIndex) to resolve bare-identifier values (copyFlag,
// dropFlag, ...) and parsing inline/elided "*K3SFlagOption{...}" literal values directly.
func ParseK3SFlagSet(optIndex map[string]K3SFlagOption, mapLit *ast.CompositeLit) (map[string]K3SFlagOption, error) {
	result := map[string]K3SFlagOption{}
	for _, elt := range mapLit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		name, ok := stringLit(kv.Key)
		if !ok {
			continue
		}

		switch v := kv.Value.(type) {
		case *ast.Ident:
			opt, ok := optIndex[v.Name]
			if !ok {
				return nil, fmt.Errorf("K3SFlagSet[%q]: unresolved identifier %q", name, v.Name)
			}
			result[name] = opt
		case *ast.UnaryExpr:
			if lit := k3sFlagOptionLiteral(v); lit != nil {
				result[name] = k3sFlagOptionFromLit(lit)
				continue
			}
			return nil, fmt.Errorf("K3SFlagSet[%q]: unresolved value expression", name)
		case *ast.CompositeLit:
			// Elided "*K3SFlagOption{...}" -- Go allows omitting the type when it matches the
			// map's value type, so v.Type is nil here rather than "K3SFlagOption".
			result[name] = k3sFlagOptionFromLit(v)
		default:
			return nil, fmt.Errorf("K3SFlagSet[%q]: unresolved value expression", name)
		}
	}
	return result, nil
}

// ApplyK3SFlagSet transforms k3s's own extracted flags the way RKE2's commandFromK3S does: flags
// with Drop:true are excluded, flags with Hide:true are kept but marked Hidden, and everything
// else passes through unchanged. A k3s flag with no matching flagSet entry is kept unchanged --
// upstream treats that as a hard error (every k3s flag must be explicitly accounted for), but here
// it's more useful to keep the flag and surface the gap to the caller than to silently drop real
// config.yaml keys because our own K3SFlagSet parsing missed an entry.
func ApplyK3SFlagSet(k3sFlags []Flag, flagSet map[string]K3SFlagOption) (flags []Flag, unmapped []string) {
	for _, f := range k3sFlags {
		if f.Unresolved {
			flags = append(flags, f)
			continue
		}
		opt, ok := flagSet[f.Name]
		if !ok {
			unmapped = append(unmapped, f.Name)
			flags = append(flags, f)
			continue
		}
		if opt.Drop {
			continue
		}
		if opt.Hide {
			f.Hidden = true
		}
		flags = append(flags, f)
	}
	return flags, unmapped
}
