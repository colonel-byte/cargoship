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

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/internal/cfg"
	"github.com/colonel-byte/cargoship/src/pkg/distro"
	"github.com/colonel-byte/cargoship/src/pkg/helmvalues"
	"github.com/colonel-byte/cargoship/src/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/src/pkg/utils"
	"github.com/spf13/cobra"
)

// packageSchemaSource is the --package flag and everything reaching a package needs, shared by the
// commands that compose a package's values schema into the inventory schema: `schema inventory`
// emits the composed document for an editor, and `validate` checks an inventory against it.
type packageSchemaSource struct {
	packageVerifyFlags
	pkg string
}

// packageValuesSchema reads the values schema out of whatever --package names, and returns a note
// identifying the package for the grafted node's description.
//
// A package with no values schema is not an error. It is a package that accepts no values, or one
// that accepts them without describing them; either way the plain inventory schema is the right
// answer, and the warning says why spec.config.values did not gain any completions.
func (o *packageSchemaSource) packageValuesSchema(ctx context.Context, cmd *cobra.Command) (helmvalues.Schema, string, error) {
	dir, rel, note, cleanup, err := o.resolvePackage(ctx, cmd)
	if err != nil {
		return nil, "", err
	}
	defer cleanup()

	if rel == "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s declares no values schema, so spec.config.values is left untyped\n", o.pkg)
		return nil, "", nil
	}

	schema, err := helmvalues.LoadSchema(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return nil, "", err
	}
	return schema, note, nil
}

// resolvePackage locates the package's directory and the schema path relative to it.
//
// Both shapes are worth supporting. A built package is what an operator has in an air gap, and is
// the only shape that carries a resolved values/values.schema.json. A source directory is what a
// package author has open, and reading it directly means the schema can be composed without a
// build first.
func (o *packageSchemaSource) resolvePackage(ctx context.Context, cmd *cobra.Command) (dir string, rel string, note string, cleanup func(), err error) {
	noop := func() {}

	if _, srcErr := utils.IdentifySource(o.pkg); srcErr != nil {
		// Not a fetchable source, so it is a path to a definition on disk.
		disPath, err := layout.ResolveDistroPath(o.pkg)
		if err != nil {
			return "", "", "", noop, err
		}
		b, err := os.ReadFile(disPath.ManifestFile)
		if err != nil {
			return "", "", "", noop, fmt.Errorf("unable to read %s: %w", disPath.ManifestFile, err)
		}
		// Parsed rather than validated: emitting a schema should work on a definition that is
		// still being written, and the values schema is readable long before the package builds.
		dis, err := cfg.Parse(ctx, b)
		if err != nil {
			return "", "", "", noop, err
		}
		return disPath.BaseDir, dis.Spec.Values.Schema, packageNote(dis.Metadata.Name, dis.Metadata.Version), noop, nil
	}

	cachePath, err := getCachePath(ctx)
	if err != nil {
		return "", "", "", noop, err
	}

	layoutOut, err := distro.Load(ctx, o.pkg, distro.LoadOptions{
		CachePath:            cachePath,
		Architecture:         config.CLIArch,
		VerificationStrategy: o.verify.toStrategy(),
		VerifyBlobOptions:    o.buildVerifyBlobOptions(cmd, v),
	})
	if err != nil {
		return "", "", "", noop, err
	}
	cleanup = func() {
		if err := layoutOut.Cleanup(); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "failed to remove the extracted package: %v\n", err)
		}
	}

	return layoutOut.DirPath(),
		layoutOut.Distro.Spec.Values.Schema,
		packageNote(layoutOut.Distro.Metadata.Name, layoutOut.Distro.Metadata.Version),
		cleanup,
		nil
}

// packageNote names the package the values came from, so that a generated file found later says
// what it was composed against.
func packageNote(name, version string) string {
	switch {
	case name == "" && version == "":
		return ""
	case version == "":
		return fmt.Sprintf("The values below were composed in from the %s package.", name)
	default:
		return fmt.Sprintf("The values below were composed in from the %s package, version %s.", name, version)
	}
}
