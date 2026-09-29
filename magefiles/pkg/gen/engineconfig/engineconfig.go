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

package engineconfig

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/pkg/engineconfig/extract"
	"github.com/colonel-byte/cargoship/pkg/engineconfig/gen"
)

const (
	EngineConfigOut = "pkg/engineconfig/gen"

	rke2CommonFlagsFile = "zz_root.go"
	rke2FlagOptsFile    = "zz_k3sopts.go"

	k3sComponentsFile  = "zz_stage.go"
	rke2ComponentsFile = "zz_types.go"

	disableItemsDecl = "DisableItems"
	cniItemsDecl     = "CNIItems"
	ingressItemsDecl = "IngressItems"
)

type engineConfigTarget struct {
	sourceFile string
	target     string
	structName string
}

var engineConfigTargets = []engineConfigTarget{
	{
		sourceFile: "zz_server.go",
		target:     "server",
		structName: "ServerConfig",
	},
	{
		sourceFile: "zz_agent.go",
		target:     "agent",
		structName: "AgentConfig",
	},
}

var invalidPackageChars = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// GenerateEngineConfig generates config.yaml structs from thirdparty-src/.
func GenerateEngineConfig() error {
	distroDirs, err := os.ReadDir(ThirdpartySrcDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("no", ThirdpartySrcDir, "directory, nothing to generate")
			return nil
		}
		return err
	}

	var regEntries []registryEntry
	for _, distroDir := range distroDirs {
		if !distroDir.IsDir() {
			continue
		}
		distro := distroDir.Name()

		versionDirs, err := os.ReadDir(filepath.Join(ThirdpartySrcDir, distro))
		if err != nil {
			return err
		}
		for _, versionDir := range versionDirs {
			if !versionDir.IsDir() {
				continue
			}
			version := versionDir.Name()

			var written map[string]bool
			var genErr error
			if distro == "rke2" {
				written, genErr = generateRKE2ConfigVersion(version)
			} else {
				written, genErr = generateEngineConfigVersion(distro, version)
			}
			if genErr != nil {
				return fmt.Errorf("%s %s: %w", distro, version, genErr)
			}

			if written["server"] && written["agent"] {
				regEntries = append(regEntries, registryEntry{
					Distro: distro,
					Pkg:    invalidPackageChars.ReplaceAllString(version, "_"),
				})
			}
		}
	}
	return writeRegistry(regEntries)
}

func parseGoFiles(fset *token.FileSet, srcDir string, names []string) (map[string]*ast.File, error) {
	files := map[string]*ast.File{}
	for _, name := range names {
		path := filepath.Join(srcDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		files[name] = f
	}
	return files, nil
}

func astFileValues(files map[string]*ast.File) []*ast.File {
	out := make([]*ast.File, 0, len(files))
	for _, f := range files {
		out = append(out, f)
	}
	return out
}

func targetSourceFiles() []string {
	names := make([]string, len(engineConfigTargets))
	for i, t := range engineConfigTargets {
		names[i] = t.sourceFile
	}
	return names
}

func generateEngineConfigVersion(distro, version string) (map[string]bool, error) {
	srcDir := filepath.Join(ThirdpartySrcDir, distro, version)
	pkgName := invalidPackageChars.ReplaceAllString(version, "_")
	outDir := filepath.Join(EngineConfigOut, distro, pkgName)

	fset := token.NewFileSet()
	files, err := parseGoFiles(fset, srcDir, append(targetSourceFiles(), k3sComponentsFile))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	varIndex := extract.BuildVarIndex(astFileValues(files)...)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	written := map[string]bool{}
	for _, t := range engineConfigTargets {
		f, ok := files[t.sourceFile]
		if !ok {
			continue
		}

		flags, err := extract.ExtractFlags(varIndex, f)
		if err != nil {
			return written, fmt.Errorf("extracting %s: %w", t.sourceFile, err)
		}

		manifest := extract.Manifest{
			Distro:  distro,
			Version: version,
			Target:  t.target,
			Flags:   flags,
		}

		if err := writeEngineConfig(outDir, pkgName, t, manifest); err != nil {
			return written, err
		}
		written[t.target] = true
	}

	var components extract.Components
	if f, ok := files[k3sComponentsFile]; ok {
		components.Disable, _ = extract.StringListDecl(disableItemsDecl, f)
	}
	if err := writeEngineComponents(outDir, pkgName, distro, version, components); err != nil {
		return written, err
	}
	return written, nil
}

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

func writeEngineConfig(outDir, pkgName string, t engineConfigTarget, manifest extract.Manifest) error {
	src, err := gen.Generate(gen.Options{
		PackageName: pkgName,
		StructName:  t.structName,
		Manifest:    manifest,
	})
	if err != nil {
		return fmt.Errorf("generating %s: %w", t.target, err)
	}

	outPath := filepath.Join(outDir, "zz_"+t.target+"_config.go")
	if err := os.WriteFile(outPath, src, 0o644); err != nil {
		return err
	}

	unresolved := unresolvedNames(manifest.Flags)
	fmt.Printf("Successfully generated %s (%d flags, %d unresolved)\n", outPath, len(manifest.Flags)-len(unresolved), len(unresolved))
	return nil
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

func writeEngineComponents(outDir, pkgName, distro, version string, components extract.Components) error {
	src, err := gen.GenerateComponents(gen.ComponentsOptions{
		PackageName: pkgName,
		Distro:      distro,
		Version:     version,
		Components:  components,
	})
	if err != nil {
		return fmt.Errorf("generating components: %w", err)
	}

	outPath := filepath.Join(outDir, "zz_addons.go")
	if err := os.WriteFile(outPath, src, 0o644); err != nil {
		return err
	}

	fmt.Printf("Successfully generated %s (%d addons, %d cni, %d ingress)\n",
		outPath, len(components.Disable), len(components.CNI), len(components.Ingress))
	return nil
}

func unresolvedNames(flags []extract.Flag) []string {
	var names []string
	for _, f := range flags {
		if f.Unresolved {
			names = append(names, f.Name)
		}
	}
	sort.Strings(names)
	return names
}

type registryEntry struct {
	Distro string
	Pkg    string
}

const registryHeader = `// Copyright 2026 colonel-byte
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

// Code generated by mage generate:engineConfig. DO NOT EDIT.

package gen

`

func writeRegistry(entries []registryEntry) error {
	if len(entries) == 0 {
		return nil
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Distro != entries[j].Distro {
			return entries[i].Distro < entries[j].Distro
		}
		return entries[i].Pkg < entries[j].Pkg
	})

	byDistro := map[string][]registryEntry{}
	var distros []string
	for _, e := range entries {
		if _, ok := byDistro[e.Distro]; !ok {
			distros = append(distros, e.Distro)
		}
		byDistro[e.Distro] = append(byDistro[e.Distro], e)
	}
	sort.Strings(distros)

	var buf strings.Builder
	buf.WriteString(registryHeader)

	buf.WriteString("import (\n")
	for _, e := range entries {
		fmt.Fprintf(&buf, "\t%s %q\n", registryImportAlias(e), fmt.Sprintf("github.com/colonel-byte/cargoship/pkg/engineconfig/gen/%s/%s", e.Distro, e.Pkg))
	}
	buf.WriteString(")\n\n")

	buf.WriteString("// Registry maps distro id (e.g. \"k3s\", \"rke2\") -> sanitized minor version package\n")
	buf.WriteString("// name (e.g. \"v1_35\") -> Entry. See Lookup in lookup.go for the consumer-facing API.\n")
	buf.WriteString("var Registry = map[string]map[string]Entry{\n")
	for _, distro := range distros {
		fmt.Fprintf(&buf, "\t%q: {\n", distro)
		for _, e := range byDistro[distro] {
			alias := registryImportAlias(e)
			fmt.Fprintf(&buf, "\t\t%q: {Server: %s.ServerConfig{}, Agent: %s.AgentConfig{}, Addons: %s.Addons, CNIs: %s.CNIs, IngressControllers: %s.IngressControllers},\n",
				e.Pkg, alias, alias, alias, alias, alias)
		}
		buf.WriteString("\t},\n")
	}
	buf.WriteString("}\n")

	src, err := format.Source([]byte(buf.String()))
	if err != nil {
		return fmt.Errorf("formatting registry.go: %w", err)
	}

	outPath := filepath.Join(EngineConfigOut, "zz_registry.go")
	if err := os.WriteFile(outPath, src, 0o644); err != nil {
		return err
	}
	fmt.Printf("Successfully generated %s (%d distro/version entries)\n", outPath, len(entries))
	return nil
}

func registryImportAlias(e registryEntry) string {
	return e.Distro + "_" + e.Pkg
}
