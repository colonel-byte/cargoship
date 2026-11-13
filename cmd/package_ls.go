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
	"strings"

	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/pkg/distro"
	goyaml "github.com/goccy/go-yaml"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/message"
)

type packageLsOptions struct {
	name         string
	outputFormat outputFormat
	registryTargetFlags
}

func newPackageLsCommand() *cobra.Command {
	o := packageLsOptions{}
	cmd := &cobra.Command{
		Use:     "ls [REPOSITORY]",
		Aliases: []string{"list"},
		Args:    cobra.MaximumNArgs(1),
		Short:   lang.CmdPackageLsShort,
		Long:    lang.CmdPackageLsLong,
		Example: lang.CmdPackageLsExample,
		GroupID: lang.RootGroupPackageID,
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.run(cmd.Context(), args)
		},
	}

	cmd.Flags().StringVar(&o.name, "name", "", lang.CmdPackageLsFlagName)
	addRegistryTargetFlags(cmd, &o.registryTargetFlags)
	if err := registerFlagOutputFormat(cmd, &o.outputFormat); err != nil {
		logger.From(cmd.Context()).Debug("error when trying add shell completion", "error", err)
	}
	addRegistryFlags(cmd)

	return cmd
}

func (o *packageLsOptions) run(ctx context.Context, args []string) error {
	explicit, err := explicitRegistry(args, 0)
	if err != nil {
		return err
	}
	// The same three places mirror looks, in the same order, so that listing a registry and
	// populating it are spelled identically.
	target, err := o.resolveTarget(ctx, explicit)
	if err != nil {
		return err
	}
	defer target.release()

	records, err := distro.ListMirrored(ctx, target.registry, distro.ListMirroredOptions{
		RemoteOptions: defaultRemoteOptions(),
		Username:      target.username,
		Password:      target.password,
		DialContext:   target.dial,
		CABundle:      target.caBundle,
		Name:          o.name,
	})
	if err != nil {
		return err
	}

	return o.print(ctx, target.registry, records)
}

// print writes the listing: a machine-readable document to stdout when one was asked for, and
// otherwise a table to OutputWriter, which is where every other human-facing table goes.
func (o *packageLsOptions) print(ctx context.Context, target string, records []distro.MirrorRecord) error {
	switch o.outputFormat {
	case "yaml":
		b, err := goyaml.Marshal(records)
		if err != nil {
			return fmt.Errorf("could not marshal yaml output: %w", err)
		}
		fmt.Println(string(b))
		return nil
	case "json":
		// Marshalled from a non-nil slice so that an empty registry is "[]" rather than
		// "null", which a caller has to special-case.
		if records == nil {
			records = []distro.MirrorRecord{}
		}
		b, err := json.MarshalIndent(records, "", "  ")
		if err != nil {
			return fmt.Errorf("could not marshal json output: %w", err)
		}
		fmt.Println(string(b))
		return nil
	case "":
	default:
		return fmt.Errorf("unsupported output format: %s", o.outputFormat)
	}

	if len(records) == 0 {
		// Not an error. A registry a mirror has never run against is a perfectly ordinary
		// answer, and an exit code saying otherwise would break a script that checks.
		logger.From(ctx).Info("no cargoship package is recorded as mirrored here", "registry", target)
		return nil
	}

	rows := make([][]string, 0, len(records))
	for _, r := range records {
		rows = append(rows, []string{
			r.Name,
			r.Version,
			strings.Join(r.Arches, ","),
			r.Engine,
			r.MirroredAt.Local().Format("2006-01-02 15:04"),
			r.CargoshipVersion,
		})
	}
	logger.From(ctx).Info("packages mirrored into this registry", "registry", target, "count", len(records))
	message.Table(OutputWriter, []string{"Package", "Version", "Arch", "Engine", "Mirrored", "By"}, rows)
	return nil
}
