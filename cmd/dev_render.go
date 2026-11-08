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
	"fmt"
	"os"

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/pkg/distrogen"
	"github.com/colonel-byte/cargoship/pkg/helmvalues"
	"github.com/spf13/cobra"
)

const (
	// DevRenderTemplate flag
	DevRenderTemplate = "template"
	// DevRenderValues flag
	DevRenderValues = "values"
	// DevRenderOut flag
	DevRenderOut = "out"
	// DevRenderNoValidate flag
	DevRenderNoValidate = "no-validate"
)

type renderOptions struct {
	templates  []string
	values     []string
	out        string
	noValidate bool
}

// newDevRenderCommand renders package definition templates.
//
// A fleet's definitions differ in a handful of values -- a version, an architecture, a CNI --
// and are otherwise the same file, which is why cargoship's own examples under example/ are
// generated rather than written. This exposes that generator: the same templates, the same
// function map, against values an operator supplies instead of values derived from engine pins.
//
// The flags are all one-off instructions about a single invocation rather than settings a fleet
// has one answer to, so none of them has a config file key.
func newDevRenderCommand() *cobra.Command {
	o := renderOptions{}

	cmd := &cobra.Command{
		Use:     "render",
		Args:    cobra.NoArgs,
		Short:   lang.CmdDevRenderShort,
		Long:    lang.CmdDevRenderLong,
		Example: lang.CmdDevRenderExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return o.run(cmd)
		},
	}

	cmd.Flags().StringArrayVarP(&o.templates, DevRenderTemplate, "t", nil, lang.CmdDevRenderFlagTemplate)
	cmd.Flags().StringArrayVar(&o.values, DevRenderValues, nil, lang.CmdDevRenderFlagValues)
	cmd.Flags().StringVarP(&o.out, DevRenderOut, "o", ".", lang.CmdDevRenderFlagOut)
	cmd.Flags().BoolVar(&o.noValidate, DevRenderNoValidate, false, lang.CmdDevRenderFlagNoValidate)

	if err := cmd.MarkFlagRequired(DevRenderTemplate); err != nil {
		fmt.Fprintf(os.Stderr, "failed to mark %s as required: %v\n", DevRenderTemplate, err)
	}

	return cmd
}

func (o *renderOptions) run(cmd *cobra.Command) error {
	values := map[string]any{}
	for _, path := range o.values {
		parsed, err := helmvalues.ParseFile(path)
		if err != nil {
			return err
		}
		values = helmvalues.MergeValues(values, parsed)
	}

	files := make([]distrogen.File, 0, len(o.templates))
	for _, path := range o.templates {
		files = append(files, distrogen.File{Template: path})
	}

	// Before anything is read: --out defaults to the working directory, so a template named
	// for what it renders to would otherwise be overwritten by its own output.
	if err := distrogen.CheckOutputs(o.out, files); err != nil {
		return err
	}

	rendered, err := distrogen.Render(files, values)
	if err != nil {
		return err
	}

	// Checked before anything is written, so a definition that renders but cannot be built does
	// not land on disk looking finished.
	if !o.noValidate {
		checked := 0
		for _, r := range rendered {
			if r.Name != config.DistroYAML {
				continue
			}
			if err := distrogen.ValidateDistro(r.Name, r.Content); err != nil {
				return err
			}
			checked++
		}
		// Only a file named distro.yaml is a package definition there is a schema for, so a
		// run that rendered none validated nothing. Said out loud, because the alternative is
		// an operator believing a template named anything else was checked.
		if checked == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "No %s rendered, so nothing was checked against the package schema\n", config.DistroYAML)
		}
	}

	paths, err := distrogen.Write(o.out, rendered)
	if err != nil {
		return err
	}
	for _, path := range paths {
		fmt.Fprintln(cmd.OutOrStdout(), "Rendered "+path)
	}
	return nil
}
