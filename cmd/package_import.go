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
	"os"

	"github.com/colonel-byte/cargoship/cmd/flags"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/pkg/distro"
	"github.com/spf13/cobra"
	zconfig "github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type packageImportOptions struct {
	outputDirectory string
	format          string
	packageVerifyFlags
}

func newPackageImportCommand() *cobra.Command {
	o := packageImportOptions{}
	cmd := &cobra.Command{
		Use:     "import [ARCHIVE]",
		Args:    cobra.ExactArgs(1),
		Short:   lang.CmdPackageImportShort,
		Long:    lang.CmdPackageImportLong,
		Example: lang.CmdPackageImportExample,
		GroupID: lang.RootGroupPackageID,
		PreRunE: o.preRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return o.run(ctx, cmd, args)
		},
	}

	output, err := zconfig.GetAbsHomePath(v.GetString(distroOutputKey))
	if err != nil {
		logger.From(cmd.Context()).Debug("error when trying to get user path", "error", err)
		output = v.GetString(distroOutputKey)
	}

	cmd.Flags().StringVarP(&o.outputDirectory, "output", "o", output, lang.CmdPackageCreateFlagOutput)
	cmd.Flags().StringVar(&o.format, "format", "auto", lang.CmdPackageImportFlagFormat)
	if err := cmd.RegisterFlagCompletionFunc("format", flags.RegisterImportFormat); err != nil {
		logger.From(cmd.Context()).Debug("error registering format flag completion", "error", err)
	}
	addVerifyFlags(cmd, v, &o.packageVerifyFlags)
	addBuildFlags(cmd)

	return cmd
}

func (o *packageImportOptions) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	srcArchive := args[0]
	outputDir := o.outputDirectory
	if outputDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		outputDir = wd
	}

	pkgPath, err := distro.Import(ctx, srcArchive, distro.ImportOptions{
		Format:               distro.ImportFormat(o.format),
		Architecture:         config.CLIArch,
		Output:               outputDir,
		VerificationStrategy: o.verify.toStrategy(),
		VerifyBlobOptions:    o.buildVerifyBlobOptions(cmd, v),
	})
	if err != nil {
		return err
	}

	logger.From(ctx).Info("distro package imported", "path", pkgPath)
	return nil
}
