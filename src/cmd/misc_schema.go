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
	"encoding/json"
	"fmt"
	"os"

	"github.com/colonel-byte/cargoship/src/config/lang"
	pkgschema "github.com/colonel-byte/cargoship/src/pkg/schema"
	"github.com/spf13/cobra"
)

const (
	// MiscSchemaOutput flag
	MiscSchemaOutput = "output"
	// MiscSchemaPackage flag
	MiscSchemaPackage = "package"
)

type schemaOptions struct {
	packageSchemaSource
	output string
}

// newSchemaCommand serves the schemas cargoship generates for its own file formats out of the
// binary, so that editor support does not depend on reaching raw.githubusercontent.com.
//
// Two things are wrong with the hosted copy every generated distro.yaml and the inventory guide
// point at. An air-gapped operator cannot fetch it at all, and yaml-language-server degrades
// silently when it cannot, so nothing says the file stopped being checked. An operator who can
// fetch it gets the default branch rather than the release they are running. Both are fixed by
// the binary carrying the schema it validates against.
//
// --package goes further than a hosted schema ever could: spec.config.values is untyped in the
// generated inventory schema, because its shape belongs to whichever package is being installed.
func newSchemaCommand() *cobra.Command {
	o := schemaOptions{}

	cmd := &cobra.Command{
		Use:       "schema KIND",
		Args:      cobra.ExactArgs(1),
		ValidArgs: pkgschema.Kinds(),
		Short:     lang.CmdSchemaShort,
		Long:      lang.CmdSchemaLong,
		Example:   lang.CmdSchemaExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.run(cmd.Context(), cmd, args)
		},
	}

	cmd.Flags().StringVarP(&o.output, MiscSchemaOutput, "o", "", lang.CmdSchemaFlagOutput)
	cmd.Flags().StringVar(&o.pkg, MiscSchemaPackage, "", lang.CmdSchemaFlagPackage)

	// Only --package needs any of these, and only when it names a built package rather than a
	// source directory. They are registered unconditionally because cobra has no way to add a
	// flag once an argument has been parsed, and a package that has to be pulled and verified
	// needs the same transport and trust settings every other command takes.
	addVerifyFlags(cmd, v, &o.packageVerifyFlags)
	addBuildFlags(cmd)
	addRegistryFlags(cmd)

	return cmd
}

func (o *schemaOptions) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	kind := pkgschema.Kind(args[0])
	if _, err := pkgschema.FileName(kind); err != nil {
		return err
	}

	if o.pkg != "" && kind != pkgschema.KindInventory {
		return fmt.Errorf("--%s only applies to the %s schema: a package's values are overridden in an inventory, and %q describes a different file",
			MiscSchemaPackage, pkgschema.KindInventory, kind)
	}

	if o.pkg == "" {
		// Served verbatim, so that a file written here and the same file fetched from the
		// repository are byte-identical.
		raw, err := pkgschema.Raw(kind)
		if err != nil {
			return err
		}
		return o.emit(cmd, raw)
	}

	values, note, err := o.packageValuesSchema(ctx, cmd)
	if err != nil {
		return err
	}

	doc, err := pkgschema.ComposeInventory(values, note)
	if err != nil {
		return err
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("unable to serialize the composed schema: %w", err)
	}
	return o.emit(cmd, append(out, '\n'))
}

// emit writes the schema where the operator asked for it. Anything that is not the schema goes to
// stderr, so that stdout can be redirected into a file and piped into jq unchanged.
func (o *schemaOptions) emit(cmd *cobra.Command, b []byte) error {
	if o.output == "" {
		_, err := cmd.OutOrStdout().Write(b)
		return err
	}
	if err := os.WriteFile(o.output, b, 0644); err != nil {
		return fmt.Errorf("unable to write %s: %w", o.output, err)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Wrote %s\n", o.output)
	return nil
}
