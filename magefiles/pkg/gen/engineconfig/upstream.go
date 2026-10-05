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

// This file holds the upstream path: upstream's config surface is kubeadm's real Go struct
// fields, so its keys are extracted as a nested tree from an API type rather than from a
// []cli.Flag{...} literal the way k3s and rke2 declare theirs.

package engineconfig

import (
	"fmt"
	"go/token"
	"os"
	"path/filepath"

	"github.com/colonel-byte/cargoship/pkg/engineconfig/extract"
	"github.com/colonel-byte/cargoship/pkg/engineconfig/gen"
)

const (
	// upstreamTypesFile holds kubeadm's v1beta4 API type declarations.
	upstreamTypesFile = "zz_types.go"
	// upstreamRootType is the struct upstream's `.spec.config.engine.config` is shaped like --
	// see the doc comment on it in types/distrocfg/upstream_kubeadm.go.
	upstreamRootType = "ClusterConfiguration"
	// upstreamKeysVar is the generated var name holding the extracted nested key tree.
	upstreamKeysVar = "ClusterConfigurationKeys"
)

// generateUpstreamConfigVersion extracts a nested key tree from kubeadm's ClusterConfiguration
// (upstreamRootType) instead of a []cli.Flag{...} literal -- upstream's config surface is real Go
// struct fields, not a flag set. Written as "server" so the caller's registration gate can treat
// upstream as done after one artifact; upstream has no separate agent target.
func generateUpstreamConfigVersion(version string) (map[string]bool, error) {
	srcDir := filepath.Join(ThirdpartySrcDir, "upstream", version)
	pkgName := invalidPackageChars.ReplaceAllString(version, "_")
	outDir := filepath.Join(EngineConfigOut, "upstream", pkgName)

	fset := token.NewFileSet()
	files, err := parseGoFiles(fset, srcDir, []string{upstreamTypesFile})
	if err != nil {
		return nil, err
	}
	typesFile, ok := files[upstreamTypesFile]
	if !ok {
		return nil, nil
	}

	node, err := extract.ExtractNestedKeys(typesFile, upstreamRootType)
	if err != nil {
		return nil, fmt.Errorf("extracting %s: %w", upstreamRootType, err)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	src, err := gen.GenerateNestedKeys(gen.NestedKeysOptions{
		PackageName: pkgName,
		VarName:     upstreamKeysVar,
		Distro:      "upstream",
		Version:     version,
		RootType:    upstreamRootType,
		Node:        node,
	})
	if err != nil {
		return nil, fmt.Errorf("generating %s: %w", upstreamRootType, err)
	}

	outPath := filepath.Join(outDir, "zz_upstream_config.go")
	if err := os.WriteFile(outPath, src, 0o644); err != nil {
		return nil, err
	}
	fmt.Printf("Successfully generated %s\n", outPath)

	written := map[string]bool{"server": true}
	if err := writeEngineComponents(outDir, pkgName, "upstream", version, extract.Components{}); err != nil {
		return written, err
	}
	return written, nil
}
