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

// This file holds the rke2 path: rke2 wraps k3s and re-declares its flags through a
// K3SFlagSet, so its config is composed from both trees rather than extracted from one.

package engineconfig

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/colonel-byte/cargoship/pkg/engineconfig/extract"
)

func generateRKE2ConfigVersion(version string) (map[string]bool, error) {
	rke2Dir := filepath.Join(ThirdpartySrcDir, "rke2", version)
	k3sDir := filepath.Join(ThirdpartySrcDir, "k3s", version)

	fset := token.NewFileSet()

	rke2Files, err := parseGoFiles(fset, rke2Dir, append(targetSourceFiles(), rke2CommonFlagsFile, rke2FlagOptsFile, rke2ComponentsFile))
	if err != nil {
		return nil, err
	}
	if len(rke2Files) == 0 {
		return nil, nil
	}

	k3sFiles, err := parseGoFiles(fset, k3sDir, targetSourceFiles())
	if err != nil {
		return nil, err
	}
	if len(k3sFiles) == 0 {
		return nil, fmt.Errorf("no k3s source found at %s to compose rke2 %s against", k3sDir, version)
	}

	optIndex := extract.BuildK3SFlagOptionIndex(astFileValues(rke2Files)...)
	rke2VarIndex := extract.BuildVarIndex(astFileValues(rke2Files)...)
	k3sVarIndex := extract.BuildVarIndex(astFileValues(k3sFiles)...)

	var commonFlags []extract.Flag
	if rootFile, ok := rke2Files[rke2CommonFlagsFile]; ok {
		commonFlags, err = extract.ExtractFlags(rke2VarIndex, rootFile)
		if err != nil {
			return nil, fmt.Errorf("extracting %s: %w", rke2CommonFlagsFile, err)
		}
	}

	pkgName := invalidPackageChars.ReplaceAllString(version, "_")
	outDir := filepath.Join(EngineConfigOut, "rke2", pkgName)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	written := map[string]bool{}
	for _, t := range engineConfigTargets {
		rke2File, ok := rke2Files[t.sourceFile]
		if !ok {
			continue
		}
		k3sFile, ok := k3sFiles[t.sourceFile]
		if !ok {
			return written, fmt.Errorf("rke2 %s has no matching k3s %s to compose against", t.sourceFile, t.sourceFile)
		}

		k3sFlags, err := extract.ExtractFlags(k3sVarIndex, k3sFile)
		if err != nil {
			return written, fmt.Errorf("extracting k3s %s for rke2 composition: %w", t.sourceFile, err)
		}

		composed := k3sFlags
		if flagSetLit := extract.FindK3SFlagSet(rke2File); flagSetLit != nil {
			flagSet, err := extract.ParseK3SFlagSet(optIndex, flagSetLit)
			if err != nil {
				return written, fmt.Errorf("parsing K3SFlagSet in rke2 %s: %w", t.sourceFile, err)
			}
			var unmapped []string
			composed, unmapped = extract.ApplyK3SFlagSet(k3sFlags, flagSet)
			if len(unmapped) > 0 {
				fmt.Printf("warning: rke2 %s %s: k3s flag(s) with no K3SFlagSet entry, kept as-is: %s\n",
					version, t.sourceFile, strings.Join(unmapped, ", "))
			}
		}

		ownFlags, err := extract.ExtractFlags(rke2VarIndex, rke2File)
		if err != nil {
			return written, fmt.Errorf("extracting rke2 %s's own flags: %w", t.sourceFile, err)
		}

		flags := make([]extract.Flag, 0, len(composed)+len(ownFlags)+len(commonFlags))
		flags = append(flags, composed...)
		flags = append(flags, ownFlags...)
		flags = append(flags, commonFlags...)

		manifest := extract.Manifest{
			Distro:  "rke2",
			Version: version,
			Target:  t.target,
			Flags:   flags,
		}

		if err := writeEngineConfig(outDir, pkgName, t, manifest); err != nil {
			return written, err
		}
		written[t.target] = true
	}

	if err := writeEngineComponents(outDir, pkgName, "rke2", version, rke2Components(rke2Files)); err != nil {
		return written, err
	}
	return written, nil
}

func rke2Components(files map[string]*ast.File) extract.Components {
	componentsFile, ok := files[rke2ComponentsFile]
	if !ok {
		return extract.Components{}
	}

	disable, _ := extract.StringListDecl(disableItemsDecl, componentsFile)
	cni, _ := extract.StringListDecl(cniItemsDecl, componentsFile)
	ingress, _ := extract.StringListDecl(ingressItemsDecl, componentsFile)

	disable = append(disable, rke2ChartNames(cni)...)
	disable = append(disable, rke2ChartNames(ingress)...)
	if serverFile, ok := files["zz_server.go"]; ok {
		disable = append(disable, extract.DisableSetCalls(serverFile)...)
	}

	slices.Sort(disable)
	return extract.Components{
		Disable: slices.Compact(disable),
		CNI:     cni,
		Ingress: ingress,
	}
}

func rke2ChartNames(items []string) []string {
	names := make([]string, 0, len(items)*2)
	for _, item := range items {
		names = append(names, "rke2-"+item, "rke2-"+item+"-crd")
	}
	return names
}
