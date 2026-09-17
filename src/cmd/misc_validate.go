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
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/colonel-byte/cargoship/src/config/lang"
	pkgschema "github.com/colonel-byte/cargoship/src/pkg/schema"
	"github.com/spf13/cobra"
)

const (
	// MiscValidateKind flag
	MiscValidateKind = "kind"
	// MiscValidatePackage flag
	MiscValidatePackage = "package"
)

type validateOptions struct {
	packageSchemaSource
	kind string
}

// newValidateCommand checks cargoship's own files against the schemas the binary carries.
//
// Parsing is not validation here. Inventories and config files are unmarshalled without strict
// key checking, so a misspelled key is dropped rather than reported: an inventory that says
// `loadbalancr:` parses cleanly and installs with no load balancer address. The generated schemas
// already describe every one of those keys, and `schema` already serves them, so the only thing
// missing was a way to run the check without an editor -- in CI, over a directory of inventories,
// or on a jump box with no language server.
//
// The kind comes from the document's own `kind:` field, so a file says what it is. A config file
// has no kind field and has to be named with --kind.
func newValidateCommand() *cobra.Command {
	o := validateOptions{}

	cmd := &cobra.Command{
		Use:     "validate FILE...",
		Args:    cobra.MinimumNArgs(1),
		Short:   lang.CmdValidateShort,
		Long:    lang.CmdValidateLong,
		Example: lang.CmdValidateExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.run(cmd.Context(), cmd, args)
		},
	}

	cmd.Flags().StringVar(&o.kind, MiscValidateKind, "", lang.CmdValidateFlagKind)
	cmd.Flags().StringVar(&o.pkg, MiscValidatePackage, "", lang.CmdValidateFlagPackage)

	if err := cmd.RegisterFlagCompletionFunc(MiscValidateKind, func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return pkgschema.Kinds(), cobra.ShellCompDirectiveNoFileComp
	}); err != nil {
		fmt.Printf("failed to register %s flag completion: %v", MiscValidateKind, err)
	}

	// As on `schema`, these matter only when --package names a package that has to be pulled and
	// verified rather than read off disk.
	addVerifyFlags(cmd, v, &o.packageVerifyFlags)
	addBuildFlags(cmd)
	addRegistryFlags(cmd)

	return cmd
}

func (o *validateOptions) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	if o.kind != "" {
		if _, err := pkgschema.FileName(pkgschema.Kind(o.kind)); err != nil {
			return err
		}
	}
	if o.pkg != "" && o.kind != "" && pkgschema.Kind(o.kind) != pkgschema.KindInventory {
		return fmt.Errorf("--%s only applies to the %s schema: a package's values are overridden in an inventory, and %q describes a different file",
			MiscValidatePackage, pkgschema.KindInventory, o.kind)
	}

	// Loaded once, before the first file: --package may name an OCI reference that has to be
	// pulled, and pulling it per file would be paid for nothing.
	inventory, err := o.inventoryValidator(ctx, cmd)
	if err != nil {
		return err
	}
	validators := map[pkgschema.Kind]*pkgschema.Validator{}
	if inventory != nil {
		validators[pkgschema.KindInventory] = inventory
	}

	var failures []error
	for _, path := range args {
		if err := o.check(cmd, validators, path); err != nil {
			var invalid *pkgschema.ValidationError
			if errors.As(err, &invalid) {
				failures = append(failures, err)
				continue
			}
			// A file that could not be read or whose kind could not be determined is a problem
			// with the run rather than with the document, and stops it.
			return err
		}
	}

	switch len(failures) {
	case 0:
		return nil
	case 1:
		return failures[0]
	default:
		// Reported in full rather than counted: a run over a directory should need one pass to
		// fix, and the problems are what the operator came for.
		return &multiValidationError{files: len(args), failures: failures}
	}
}

// multiValidationError reports every file that failed, keeping each ValidationError intact so a
// caller can still reach one with errors.As.
type multiValidationError struct {
	files    int
	failures []error
}

func (e *multiValidationError) Error() string {
	parts := make([]string, 0, len(e.failures))
	for _, f := range e.failures {
		parts = append(parts, f.Error())
	}
	return fmt.Sprintf("%d of %d files do not match their schema:\n\n%s", len(e.failures), e.files, strings.Join(parts, "\n\n"))
}

func (e *multiValidationError) Unwrap() []error { return e.failures }

// check validates one file, reusing a compiled validator per kind so that a directory of
// inventories compiles the schema once.
func (o *validateOptions) check(cmd *cobra.Command, validators map[pkgschema.Kind]*pkgschema.Validator, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("unable to read %s: %w", path, err)
	}
	doc, err := pkgschema.Decode(b)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	kind, err := o.kindOf(doc)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if o.pkg != "" && kind != pkgschema.KindInventory {
		return fmt.Errorf("%s is a %s document, and --%s only applies to the %s schema: a package's values are overridden in an inventory",
			path, kind, MiscValidatePackage, pkgschema.KindInventory)
	}

	validator, ok := validators[kind]
	if !ok {
		validator, err = pkgschema.NewValidatorFor(kind)
		if err != nil {
			return err
		}
		validators[kind] = validator
	}

	if err := validator.Validate(path, doc); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s: ok (%s)\n", path, kind)
	return nil
}

// kindOf resolves which schema a document is checked against. --kind wins, so that a file with a
// kind field cargoship does not recognize can still be checked deliberately.
func (o *validateOptions) kindOf(doc any) (pkgschema.Kind, error) {
	if o.kind != "" {
		return pkgschema.Kind(o.kind), nil
	}
	kind, err := pkgschema.DetectKind(doc)
	if err != nil {
		return "", fmt.Errorf("%w (--%s %s)", err, MiscValidateKind, strings.Join(pkgschema.Kinds(), "|"))
	}
	return kind, nil
}

// inventoryValidator builds the inventory validator when --package asks for the package's values
// to be checked too, and returns nil when it does not, leaving the generated schema to be compiled
// on demand.
//
// This is the part no editor-independent check can do otherwise: spec.config.values is untyped in
// the generated inventory schema because its shape belongs to the package, so without --package a
// misspelled value key passes and is only caught when the install reaches the merge.
func (o *validateOptions) inventoryValidator(ctx context.Context, cmd *cobra.Command) (*pkgschema.Validator, error) {
	if o.pkg == "" {
		return nil, nil
	}
	values, note, err := o.packageValuesSchema(ctx, cmd)
	if err != nil {
		return nil, err
	}
	doc, err := pkgschema.ComposeInventory(values, note)
	if err != nil {
		return nil, err
	}
	return pkgschema.NewValidator(pkgschema.KindInventory, doc)
}
