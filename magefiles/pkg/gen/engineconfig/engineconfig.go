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
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/colonel-byte/cargoship/pkg/engineconfig/extract"
	"github.com/colonel-byte/cargoship/pkg/engineconfig/gen"
)

const (
	// EngineConfigOut is where the generated config structs are written.
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
			switch distro {
			case "rke2":
				written, genErr = generateRKE2ConfigVersion(version)
			case "upstream":
				written, genErr = generateUpstreamConfigVersion(version)
			default:
				written, genErr = generateEngineConfigVersion(distro, version)
			}
			if genErr != nil {
				return fmt.Errorf("%s %s: %w", distro, version, genErr)
			}

			// Only register a distro/version once every target it declares was
			// generated -- Registry entries are looked up by consumers (types/distrocfg)
			// that assume that. Upstream only ever produces one artifact (no separate
			// agent target -- see docs/agent/design-config-codegen.md), so it only needs
			// "server".
			ready := written["server"] && (distro == "upstream" || written["agent"])
			if ready {
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
