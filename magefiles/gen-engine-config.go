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

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/src/pkg/engineconfig/extract"
	"github.com/colonel-byte/cargoship/src/pkg/engineconfig/gen"
)

const (
	thirdpartySrcDir = "thirdparty-src"
	engineConfigOut  = "src/pkg/engineconfig/gen"

	// rke2CommonFlagsFile holds RKE2's commonFlag []cli.Flag{...} literal, shared by both
	// server.go and agent.go (appended on top of the wrapped k3s command and RKE2's own
	// per-target additions).
	rke2CommonFlagsFile = "root.go"
	// rke2FlagOptsFile declares the copyFlag/dropFlag/hideFlag/ignoreFlag vars that a RKE2
	// K3SFlagSet{...} literal's entries reference by bare identifier.
	rke2FlagOptsFile = "k3sopts.go"
)

// engineConfigTarget maps the source file name for one urfave/cli command to the struct it
// produces. k3s and RKE2 both split "server" and "agent" the same way.
type engineConfigTarget struct {
	sourceFile string
	target     string
	structName string
}

var engineConfigTargets = []engineConfigTarget{
	{"server.go", "server", "ServerConfig"},
	{"agent.go", "agent", "AgentConfig"},
}

var invalidPackageChars = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// EngineConfig generates config.yaml structs from thirdparty-src/
//
// Statically parses the k3s/RKE2 source committed under thirdparty-src/ (see
// Generate.PullEngineSource) into a Go struct per distro/version/target. Offline: it
// never touches the network or imports k3s/RKE2 as a module -- see
// docs/agent/design-config-codegen.md.
func (Generate) EngineConfig() error {
	distroDirs, err := os.ReadDir(thirdpartySrcDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("no", thirdpartySrcDir, "directory, nothing to generate")
			return nil
		}
		return err
	}

	for _, distroDir := range distroDirs {
		if !distroDir.IsDir() {
			continue
		}
		distro := distroDir.Name()

		versionDirs, err := os.ReadDir(filepath.Join(thirdpartySrcDir, distro))
		if err != nil {
			return err
		}
		for _, versionDir := range versionDirs {
			if !versionDir.IsDir() {
				continue
			}
			version := versionDir.Name()

			var genErr error
			if distro == "rke2" {
				genErr = generateRKE2ConfigVersion(version)
			} else {
				genErr = generateEngineConfigVersion(distro, version)
			}
			if genErr != nil {
				return fmt.Errorf("%s %s: %w", distro, version, genErr)
			}
		}
	}
	return nil
}

// parseGoFiles parses whichever of names is present in srcDir, keyed by file name. Missing files
// are skipped, not an error -- callers decide what's required.
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

func generateEngineConfigVersion(distro, version string) error {
	srcDir := filepath.Join(thirdpartySrcDir, distro, version)
	pkgName := invalidPackageChars.ReplaceAllString(version, "_")
	outDir := filepath.Join(engineConfigOut, distro, pkgName)

	fset := token.NewFileSet()
	files, err := parseGoFiles(fset, srcDir, targetSourceFiles())
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}

	varIndex := extract.BuildVarIndex(astFileValues(files)...)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	for _, t := range engineConfigTargets {
		f, ok := files[t.sourceFile]
		if !ok {
			continue
		}

		flags, err := extract.ExtractFlags(varIndex, f)
		if err != nil {
			return fmt.Errorf("extracting %s: %w", t.sourceFile, err)
		}

		manifest := extract.Manifest{
			Distro:  distro,
			Version: version,
			Target:  t.target,
			Flags:   flags,
		}

		if err := writeEngineConfig(outDir, pkgName, t, manifest); err != nil {
			return err
		}
	}
	return nil
}

// generateRKE2ConfigVersion composes RKE2's real flag set instead of just extracting whatever
// []cli.Flag{...} literal happens to be in server.go/agent.go. RKE2 doesn't declare its config
// flags outright: it imports k3s's command and wraps it at runtime via
// mustCmdFromK3S(cmd, K3SFlagSet{...}) (rke2FlagOptsFile/rke2CommonFlagsFile), which drops or
// hides k3s flags by name, then appends a small RKE2-only literal (serverFlag/deprecatedFlags)
// and the shared commonFlag literal (rke2CommonFlagsFile). See docs/dev/thirdparty-src.md.
func generateRKE2ConfigVersion(version string) error {
	rke2Dir := filepath.Join(thirdpartySrcDir, "rke2", version)
	k3sDir := filepath.Join(thirdpartySrcDir, "k3s", version)

	fset := token.NewFileSet()

	rke2Files, err := parseGoFiles(fset, rke2Dir, append(targetSourceFiles(), rke2CommonFlagsFile, rke2FlagOptsFile))
	if err != nil {
		return err
	}
	if len(rke2Files) == 0 {
		return nil
	}

	k3sFiles, err := parseGoFiles(fset, k3sDir, targetSourceFiles())
	if err != nil {
		return err
	}
	if len(k3sFiles) == 0 {
		return fmt.Errorf("no k3s source found at %s to compose rke2 %s against", k3sDir, version)
	}

	optIndex := extract.BuildK3SFlagOptionIndex(astFileValues(rke2Files)...)
	rke2VarIndex := extract.BuildVarIndex(astFileValues(rke2Files)...)
	k3sVarIndex := extract.BuildVarIndex(astFileValues(k3sFiles)...)

	var commonFlags []extract.Flag
	if rootFile, ok := rke2Files[rke2CommonFlagsFile]; ok {
		commonFlags, err = extract.ExtractFlags(rke2VarIndex, rootFile)
		if err != nil {
			return fmt.Errorf("extracting %s: %w", rke2CommonFlagsFile, err)
		}
	}

	pkgName := invalidPackageChars.ReplaceAllString(version, "_")
	outDir := filepath.Join(engineConfigOut, "rke2", pkgName)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	for _, t := range engineConfigTargets {
		rke2File, ok := rke2Files[t.sourceFile]
		if !ok {
			continue
		}
		k3sFile, ok := k3sFiles[t.sourceFile]
		if !ok {
			return fmt.Errorf("rke2 %s has no matching k3s %s to compose against", t.sourceFile, t.sourceFile)
		}

		k3sFlags, err := extract.ExtractFlags(k3sVarIndex, k3sFile)
		if err != nil {
			return fmt.Errorf("extracting k3s %s for rke2 composition: %w", t.sourceFile, err)
		}

		composed := k3sFlags
		if flagSetLit := extract.FindK3SFlagSet(rke2File); flagSetLit != nil {
			flagSet, err := extract.ParseK3SFlagSet(optIndex, flagSetLit)
			if err != nil {
				return fmt.Errorf("parsing K3SFlagSet in rke2 %s: %w", t.sourceFile, err)
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
			return fmt.Errorf("extracting rke2 %s's own flags: %w", t.sourceFile, err)
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
			return err
		}
	}
	return nil
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

	outPath := filepath.Join(outDir, t.target+"_config.go")
	if err := os.WriteFile(outPath, src, 0o644); err != nil {
		return err
	}

	unresolved := unresolvedNames(manifest.Flags)
	fmt.Printf("Successfully generated %s (%d flags, %d unresolved)\n", outPath, len(manifest.Flags)-len(unresolved), len(unresolved))
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
